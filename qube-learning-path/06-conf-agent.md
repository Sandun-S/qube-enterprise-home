# 06 — Conf-Agent: The Edge Brain

The conf-agent is the most important piece of Enterprise. It runs on every Qube and manages everything — registration, config sync, container deployment, command execution.

File: `conf-agent/main.go` — 625 lines, zero external dependencies, just Go stdlib.

---

## Startup sequence

```
main()
  │
  ├─ loadConfig()          reads env vars (TPAPI_URL, WORK_DIR, etc.)
  │
  ├─ readMitTxt("/boot/mit.txt")
  │    reads: deviceid, register_key, maintain_key
  │    sets:  cfg.QubeID, cfg.RegisterKey
  │
  ├─ wait for TP-API to be reachable
  │    curl http://cloud:8081/health every 10s until OK
  │
  ├─ if QUBE_TOKEN empty → selfRegister()
  │    polls POST /v1/device/register every 60s
  │    202 pending → wait (customer hasn't claimed yet)
  │    200 claimed → save token to /opt/qube/.env
  │    401 invalid → fatal (wrong register_key)
  │
  ├─ restore localHash from /opt/qube/.config_hash
  │
  └─ ticker loop every 30s:
       sendHeartbeat()    → POST /v1/heartbeat
       executeCommands()  → POST /v1/commands/poll → exec each
       getState()         → GET /v1/sync/state → compare hash
       if hash changed:
         getConfig()      → GET /v1/sync/config
         applyConfig()    → write files + docker stack deploy
         save new hash
```

---

## Reading /boot/mit.txt

```go
func readMitTxt(path string) (*MitTxt, error) {
    f, err := os.Open(path)
    if err != nil { return nil, err }
    defer f.Close()

    m := &MitTxt{}
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        parts := strings.SplitN(line, ":", 2)
        if len(parts) != 2 { continue }
        
        key := strings.TrimSpace(parts[0])
        val := strings.TrimSpace(parts[1])
        switch key {
        case "deviceid":   m.DeviceID = val     // "Qube-1302"
        case "register":   m.RegisterKey = val   // "4D4L-R4KY-ZTQ5"
        case "maintain":   m.MaintainKey = val
        case "devicetype": m.DeviceType = val
        }
    }
    if m.DeviceID == "" {
        return nil, fmt.Errorf("deviceid not found in %s", path)
    }
    return m, nil
}
```

`SplitN(line, ":", 2)` — the `2` means "split into at most 2 parts." Without this, `opc.tcp://192.168.1.1:4840` would split at every colon. With `2`, it only splits at the first colon, giving `["opc.tcp", "//192.168.1.1:4840"]`.

---

## Self-registration

This is how the device gets its QUBE_TOKEN without anyone manually copying it:

```go
func selfRegister(client *Client, cfg Config) string {
    for {
        data, status, err := client.doPublic("POST", "/v1/device/register",
            map[string]any{
                "device_id":    cfg.QubeID,
                "register_key": cfg.RegisterKey,
            })

        switch status {
        case 200:
            // Device has been claimed — we have our token
            var resp map[string]any
            json.Unmarshal(data, &resp)
            token := resp["qube_token"].(string)
            saveTokenToEnv(cfg, token)  // write to /opt/qube/.env
            return token

        case 202:
            // Not yet claimed — customer hasn't registered the device yet
            // Log a helpful message and wait
            log.Printf("[register] Device not yet claimed. Customer must enter key '%s' in portal. Retrying in 60s...",
                cfg.RegisterKey)
            time.Sleep(60 * time.Second)

        case 401:
            // Wrong register_key — fatal
            log.Fatalf("[register] Invalid register_key: %s", data)
        }
    }
}
```

`doPublic()` vs `do()`: `do()` sets `X-Qube-ID` and `Authorization` headers (authenticated). `doPublic()` sends no auth headers — needed for `/v1/device/register` which is public.

---

## The hash comparison

```go
func runCycle(client *Client, cfg Config, localHash *string, hashFile string) {
    sendHeartbeat(client)
    executeCommands(client, cfg)

    // Get current hash from cloud
    state, err := getState(client)
    if err != nil {
        log.Printf("[sync] failed to get state: %v", err)
        return
    }
    
    log.Printf("[sync] remote=%s local=%s", safeHash(state.Hash), safeHash(*localHash))

    if state.Hash == *localHash && state.Hash != "" {
        log.Println("[sync] hashes match — no action needed")
        return  // ← Most cycles end here. No work done.
    }

    // Hash changed — download and apply full config
    log.Println("[sync] hash mismatch — downloading config")
    sc, err := getConfig(client)
    // ... write files, deploy
    *localHash = state.Hash
    os.WriteFile(hashFile, []byte(state.Hash), 0644)
}
```

`localHash *string` — the `*` means pointer. Passing `&localHash` lets the function modify the caller's variable. Without the pointer, the function would get a copy and the caller wouldn't see changes.

---

## Applying config: writing files

```go
func applyConfig(cfg Config, sc *SyncConfig) error {
    // Write docker-compose.yml
    composePath := filepath.Join(cfg.WorkDir, "docker-compose.yml")
    os.WriteFile(composePath, []byte(sc.DockerComposeYML), 0644)

    // Write all CSV and config files
    // sc.CSVFiles is map[string]string — path → content
    for path, content := range sc.CSVFiles {
        full := filepath.Join(cfg.WorkDir, path)
        os.MkdirAll(filepath.Dir(full), 0755)  // create parent dirs
        os.WriteFile(full, []byte(content), 0644)
        // e.g. writes /opt/qube/configs/panel-a/config.csv
        //            /opt/qube/configs/panel-a/configs.yml
    }

    // Write sensor_map.json
    smData, _ := json.MarshalIndent(sc.SensorMap, "", "  ")
    os.WriteFile(filepath.Join(cfg.WorkDir, "sensor_map.json"), smData, 0644)

    // Deploy the stack
    deployDocker(cfg.WorkDir)
    return nil
}
```

`filepath.Join` correctly handles path separators on all OSes. Always use it instead of string concatenation for paths.

---

## Docker detection: swarm vs compose

```go
func deployDocker(workDir string) {
    composePath := filepath.Join(workDir, "docker-compose.yml")

    // Ask Docker if swarm is active
    out, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
    isSwarm := strings.TrimSpace(string(out)) == "active"

    if isSwarm {
        // Real Qube: docker stack deploy -c compose.yml qube
        cmd := exec.Command("docker", "stack", "deploy",
            "-c", composePath,
            "--with-registry-auth",
            "qube")  // stack name = "qube" — services become "qube_panel-a" etc.
        out, err := cmd.CombinedOutput()
        // ...
    } else {
        // Dev mode: docker compose up -d
        cmd := exec.Command("docker", "compose", "-f", composePath,
            "up", "-d", "--remove-orphans")
        // ...
    }
}
```

`exec.Command().Output()` returns the stdout. `CombinedOutput()` returns stdout + stderr combined — useful for error messages.

---

## Command execution

Commands are sent from the Cloud API and queued in Postgres. The conf-agent polls for them:

```go
case "restart_service":
    service := cmd.Payload["service"].(string)  // e.g. "panel-a"
    
    swarmOut, _ := exec.Command("docker", "info", "--format",
        "{{.Swarm.LocalNodeState}}").Output()
    isSwarm := strings.TrimSpace(string(swarmOut)) == "active"
    
    if isSwarm {
        // In swarm, service is named qube_panel-a
        out, err = run("docker", "service", "update", "--force", "qube_"+service)
    } else {
        out, err = run("docker", "compose", "-f",
            filepath.Join(cfg.WorkDir, "docker-compose.yml"),
            "restart", service)
    }
```

`--force` on `docker service update` causes Docker Swarm to restart the container even if nothing changed. This is the swarm equivalent of `docker restart`.

---

## Saving the token

```go
func saveTokenToEnv(cfg Config, token string) {
    envPath := filepath.Join(cfg.WorkDir, ".env")
    
    // Read existing .env content
    existing, _ := os.ReadFile(envPath)
    lines := strings.Split(string(existing), "\n")
    
    // Update QUBE_TOKEN line if it exists, or add it
    found := false
    for i, line := range lines {
        if strings.HasPrefix(line, "QUBE_TOKEN=") {
            lines[i] = "QUBE_TOKEN=" + token
            found = true
            break
        }
    }
    if !found {
        lines = append(lines, "QUBE_TOKEN="+token)
    }
    
    os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600)
    // 0600 = only owner can read/write (token is a secret)
}
```

On the next systemd service restart, `EnvironmentFile=/opt/qube/.env` loads `QUBE_TOKEN` from the file, so the agent skips self-registration and goes straight to syncing.

---

## Why stdlib only?

The conf-agent has `go 1.22` in its `go.mod` and no external dependencies. This is a deliberate choice:
- No `go mod download` during build — faster
- Smaller binary
- No dependency vulnerabilities to track
- Works offline — important for edge devices in factories
- The standard library's `net/http`, `bufio`, `os/exec`, `encoding/json` do everything needed
