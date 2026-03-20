# 10 - Deep Code Walkthrough: Adding a New Gateway

This is an extremely detailed, code-level explanation of exactly what happens when a user adds a new Gateway (e.g., an SNMP gateway) from the Cloud UI, and precisely how that change physically instantiates a new Docker container on a remote Qube device.

We will follow the exact execution path through the Go source code.

---

## Part 1: The Cloud API Receives the Request

When the user clicks "Add Gateway" in the UI, an HTTP POST request is sent to the Cloud API running on port 8080.

**File:** `cloud/internal/api/gateways.go`
**Function:** `createGatewayHandler`

The router directs the request here. First, it decodes the JSON payload into a local struct:
```go
var req struct {
    Name       string `json:"name"`
    Protocol   string `json:"protocol"` // e.g., "snmp"
    Host       string `json:"host"`
    Port       int    `json:"port"`
    ConfigJSON any    `json:"config_json"`
}
if err := json.NewDecoder(r.Body).Decode(&req); err != nil { ... }
```

### 1.1 Database Insertion (PostgreSQL)
The cloud opens a Postgres transaction `tx, err := pool.Begin(ctx)` to ensure the gateway and its associated service are created together safely.

First, it inserts the Gateway record:
```go
// Returns the new Gateway ID (gwID)
tx.QueryRow(ctx, `
    INSERT INTO gateways 
      (qube_id, name, protocol, host, port, config_json, service_image)
    VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
    qubeID, req.Name, req.Protocol, req.Host, req.Port, cfgBytes, defaultImage,
).Scan(&gwID)
```

Immediately after, it derives a safe Docker service name (e.g., `"Panel A Modbus"` → `"panel-a-modbus"`) using `sanitizeServiceName()` and inserts a Service record. This is crucial because a single Qube can have multiple SNMP gateways, so each needs a unique Docker container name:
```go
serviceName := sanitizeServiceName(req.Name)
tx.QueryRow(ctx, `
    INSERT INTO services 
      (qube_id, gateway_id, name, image, port, env_json)
    VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
    qubeID, gwID, serviceName, defaultImage, req.Port, envBytes,
).Scan(&svcID)

tx.Commit(ctx) // Lock the data into the database!
```

### 1.2 The Most Important Function Call
Before returning the HTTP 201 Created response to the user, the API calls the master configuration hash function:
```go
hash, _ := recomputeConfigHash(ctx, pool, qubeID)
```

---

## Part 2: Recalculating the Device Hash

**File:** `cloud/internal/api/hash.go`
**Function:** `recomputeConfigHash`

This function is the bridge between human UI interactions and machine state. The system needs a way to instantly inform a remote device: *"Your configuration has changed."* It does this via SHA-256 hashing.

### 2.1 Building the State Snapshot
The function runs a giant SQL query pulling *every single active gateway and sensor* attached to this Qube:
```go
rows, err := pool.Query(ctx, `
    SELECT g.id, g.name, g.protocol, g.host, g.port, g.config_json, g.service_image
    FROM gateways g WHERE g.qube_id=$1 AND g.status='active' ORDER BY g.created_at ASC`, qubeID)
// ... It loops through rows, and for each gateway, it queries the sensors table ...
```
It builds a massive deeply-nested Go struct containing all this data.

### 2.2 Serializing and Hashing
It converts that massive struct into a flat JSON byte array, then hashes it:
```go
canonical, err := json.Marshal(map[string]any{
    "qube_id":  qubeID,
    "gateways": gateways, // Contains all gateways + their nested sensors
})

sum := sha256.Sum256(canonical)
hash := hex.EncodeToString(sum[:]) // e.g. "a1b2c3d4..."
```

### 2.3 Saving the Hash
Finally, it updates the `config_state` table:
```go
pool.Exec(ctx, `
    UPDATE config_state 
    SET hash=$1, generated_at=NOW(), config_snapshot=$2 
    WHERE qube_id=$3`, hash, canonical, qubeID)
```

---

## Part 3: The Edge Device Notices the Change

Meanwhile, thousands of miles away, the physical Qube device is running the `conf-agent` Go binary.

**File:** `conf-agent/main.go`
**Function:** `runCycle()`

Every 30 seconds, a `time.Ticker` fires and executes `runCycle()`. The agent hits the Cloud TP-API (Port 8081).

### 3.1 Polling the Hash
```go
state, err := getState(client) // HTTP GET /v1/sync/state
```
This is a lightning-fast HTTP request. The Cloud merely runs `SELECT hash FROM config_state` and returns it.

### 3.2 Detecting the Mismatch
The agent compares the Cloud's hash to the hash saved in a hidden file (`.config_hash`) on its own hard drive.
```go
if state.Hash == *localHash && state.Hash != "" {
    log.Println("[sync] hashes match — no action needed")
    return // Goes back to sleep
}

log.Println("[sync] hash mismatch — downloading config")
sc, err := getConfig(client) // HTTP GET /v1/sync/config
```
Because the UI action in Part 1 updated the hash in Part 2, the `if` statement bypasses the `return`. The agent initiates a full configuration download!

---

## Part 4: Building the Docker Infrastructure on the Fly

When the agent calls `GET /v1/sync/config`, the Cloud must instantly generate all the configuration files required for Docker.

**File:** `cloud/internal/tpapi/sync.go`
**Function:** `syncConfigHandler`

This is where the magic happens. The cloud does NOT store `docker-compose.yml` text files in the database. It generates them dynamically based on the active gateways.

### 4.1 Generating Gateway CSV Data
It loops through all active services. For our new SNMP Gateway, it grabs the protocol (`"snmp"`) and passes the sensors to `renderGatewayFiles()`:
```go
mainFile, extraFiles := renderGatewayFiles(svc.Protocol, svc.Name, entries)
csvFiles[fmt.Sprintf("configs/%s/config.csv", svc.Name)] = mainFile
```
Inside `renderGatewayFiles()`, a switch statement outputs the exact schema needed for SNMP:
```go
case "snmp":
    b.WriteString("#Table,Device,SNMP_csv,Community,Version,Output,Tags\n")
    // Loops through sensors and builds the CSV string builder...
```

### 4.2 Building `docker-compose.yml`
It calls `buildFullComposeYML()`. This function takes a `bytes.Buffer` and uses `fmt.Fprintf` to manually write standard Docker Compose syntax:

```go
// Inject the core framework
fmt.Fprintf(&b, `version: "3.8"
networks:
  qube-net:
    external: true
services:
  enterprise-conf-agent: ...
  enterprise-influx-to-sql: ...
`)

// Loop through user gateways and inject their container blocks
for _, svc := range services {
    fmt.Fprintf(&b, "  %s:\n", svc.Name) // e.g. "snmp-gateway-1:"
    fmt.Fprintf(&b, "    image: %s\n", svc.Image)
    fmt.Fprintf(&b, "    volumes:\n")
    // This part is critical! It mounts the generated CSV file directly into the container
    fmt.Fprintf(&b, "      - /opt/qube/configs/%s/config.csv:/app/config.csv:ro\n", svc.Name)
    // ...
}
```

### 4.3 Returning the JSON Payload
The API bundles all these text files into a JSON response:
```go
writeJSON(w, http.StatusOK, map[string]any{
    "hash":               hash,
    "docker_compose_yml": composeYML,     // The big generated YAML string
    "csv_files":          csvFiles,       // Map of file paths to CSV strings
    "sensor_map":         sensorMap,      // JSON mapping string for the sql-bridge
})
```

---

## Part 5: The Qube Applies the Configuration

Back on the physical device, the `conf-agent` receives the massive JSON payload.

**File:** `conf-agent/main.go`
**Function:** `applyConfig()`

### 5.1 Writing to Disk
The agent unpacks the JSON and physically writes the files to the Raspberry Pi's SSD:
```go
// Write the compose file
os.WriteFile(filepath.Join(cfg.WorkDir, "docker-compose.yml"), []byte(sc.DockerComposeYML), 0644)

// Loop through CSVs and write them to the subfolders
for path, content := range sc.CSVFiles {
    full := filepath.Join(cfg.WorkDir, path)
    os.MkdirAll(filepath.Dir(full), 0755)
    os.WriteFile(full, []byte(content), 0644)
}
```
At this point, `/opt/qube/configs/` contains a brand new folder for the new Gateway, fully populated with CSV files.

### 5.2 Restarting Docker
Finally, the agent issues a shell command to the OS native Docker Daemon:
```go
func deployDocker(workDir string) {
    // Determine if we are in Swarm or standard Compose mode
    swarmOut, _ := exec.Command("docker", "info", ...).Output()
    isSwarm := strings.TrimSpace(string(swarmOut)) == "active"

    if isSwarm {
        cmd := exec.Command("docker", "stack", "deploy", "-c", "docker-compose.yml", "qube")
        cmd.Dir = workDir
        cmd.CombinedOutput()
    } else {
        cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "up", "-d")
        cmd.Dir = workDir
        cmd.CombinedOutput()
    }
}
```

### 5.3 Docker Engine Takes Over
When `docker compose up -d` runs, the Docker Engine itself does a "diff" calculation:
1. It sees the new `snmp-gateway-1` declared in the `docker-compose.yml`.
2. It detects it is NOT currently running.
3. It downloads the `snmp-gateway` image from GitLab registry if needed.
4. It spins up the container, attaching the newly written `config.csv` volume mount.

**Conclusion:** The SNMP Gateway container boots up, reads `config.csv` from `/app/config.csv`, and immediately begins interrogating the physical SNMP devices on the network. The user's UI action has successfully rippled all the way to physical hardware protocol execution!
