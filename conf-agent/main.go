// conf-agent — Qube Enterprise Edge Agent
// Polls TP-API, syncs config, executes commands, sends heartbeats.
//
// Startup flow:
//   1. Read /boot/mit.txt → get device_id + register_key
//   2. If QUBE_TOKEN not set → call /v1/device/register to get token automatically
//   3. Poll every 30s: heartbeat → commands → hash check → sync if changed
//
// Uses stdlib only (no external dependencies).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	TPAPIURL     string
	QubeID       string
	QubeToken    string
	RegisterKey  string // from /boot/mit.txt — used for self-registration
	WorkDir      string
	PollInterval time.Duration
	MitTxtPath   string // /boot/mit.txt on real Qube, overridable for testing
}

// MitTxt holds the device identity written at flash time by image-install.sh
type MitTxt struct {
	DeviceID    string // deviceid field
	DeviceName  string // devicename field
	DeviceType  string // devicetype field
	RegisterKey string // register field — customer enters this to claim
	MaintainKey string // maintain field — IoT team use
}

func loadConfig() Config {
	interval, _ := strconv.Atoi(getenv("POLL_INTERVAL", "30"))
	return Config{
		TPAPIURL:     getenv("TPAPI_URL", "http://localhost:8081"),
		QubeID:       getenv("QUBE_ID", ""),
		QubeToken:    getenv("QUBE_TOKEN", ""),
		RegisterKey:  getenv("REGISTER_KEY", ""),
		WorkDir:      getenv("WORK_DIR", "/opt/qube"),
		MitTxtPath:   getenv("MIT_TXT_PATH", "/boot/mit.txt"),
		PollInterval: time.Duration(interval) * time.Second,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// readMitTxt reads /boot/mit.txt written by image-install.sh at flash time.
// Format:
//   deviceid: Qube-1302
//   devicename: Qube-1302
//   devicetype: rasp4
//   register: 4D4L-R4KY-ZTQ5
//   maintain: KC3L-T7XT-7T7E
func readMitTxt(path string) (*MitTxt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &MitTxt{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "deviceid":   m.DeviceID = val
		case "devicename": m.DeviceName = val
		case "devicetype": m.DeviceType = val
		case "register":   m.RegisterKey = val
		case "maintain":   m.MaintainKey = val
		}
	}
	if m.DeviceID == "" {
		return nil, fmt.Errorf("deviceid not found in %s", path)
	}
	return m, nil
}

// ─── TP-API Client ────────────────────────────────────────────────────────────

type Client struct {
	cfg  Config
	http *http.Client
}

func newClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) do(method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.cfg.TPAPIURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Qube-ID", c.cfg.QubeID)
	req.Header.Set("Authorization", "Bearer "+c.cfg.QubeToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// doPublic sends a request without auth headers (for self-registration)
func (c *Client) doPublic(method, path string, body any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, c.cfg.TPAPIURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// ─── State ────────────────────────────────────────────────────────────────────

type SyncState struct {
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SyncConfig struct {
	Hash             string            `json:"hash"`
	DockerComposeYML string            `json:"docker_compose_yml"`
	CSVFiles         map[string]string `json:"csv_files"`
	EnvFiles         map[string]string `json:"env_files"`
	SensorMap        map[string]string `json:"sensor_map"`
}

type Command struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Payload map[string]any `json:"payload"`
}

type PollResponse struct {
	Commands []Command `json:"commands"`
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()

	// ── Step 1: Try to read /boot/mit.txt for device identity ──────────────
	mit, err := readMitTxt(cfg.MitTxtPath)
	if err != nil {
		log.Printf("[agent] Could not read %s: %v", cfg.MitTxtPath, err)
		log.Printf("[agent] Falling back to env vars (QUBE_ID, REGISTER_KEY)")
	} else {
		log.Printf("[agent] Device identity from mit.txt: id=%s reg=%s type=%s",
			mit.DeviceID, mit.RegisterKey, mit.DeviceType)
		// mit.txt takes precedence over env vars for device identity
		if cfg.QubeID == "" {
			cfg.QubeID = mit.DeviceID
		}
		if cfg.RegisterKey == "" {
			cfg.RegisterKey = mit.RegisterKey
		}
	}

	if cfg.QubeID == "" {
		log.Fatal("[agent] Cannot determine device ID. Set QUBE_ID env var or ensure /boot/mit.txt exists.")
	}

	client := newClient(cfg)

	// ── Step 2: Wait for TP-API to be reachable ──────────────────────────
	log.Printf("[agent] Starting — QubeID=%s TPAPI=%s Interval=%s",
		cfg.QubeID, cfg.TPAPIURL, cfg.PollInterval)

	for {
		_, status, err := client.do("GET", "/health", nil)
		if err == nil && status == 200 {
			log.Println("[agent] TP-API reachable")
			break
		}
		log.Printf("[agent] TP-API not reachable (err=%v status=%d), retrying in 10s...", err, status)
		time.Sleep(10 * time.Second)
	}

	// ── Step 3: Self-register if no token ────────────────────────────────
	// If QUBE_TOKEN is not set, use register_key to get it from TP-API.
	// This is the production flow — no manual token copying needed.
	if cfg.QubeToken == "" {
		log.Println("[agent] No QUBE_TOKEN set — attempting self-registration...")
		cfg.QubeToken = selfRegister(client, cfg)
		client = newClient(cfg) // recreate client with new token
	}

	if cfg.QubeToken == "" {
		log.Fatal("[agent] Could not obtain QUBE_TOKEN. Device may not be claimed yet.")
	}

	// ── Step 4: Load last known hash from disk ────────────────────────────
	localHash := ""
	hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
	if b, err := os.ReadFile(hashFile); err == nil {
		localHash = strings.TrimSpace(string(b))
		log.Printf("[agent] Restored local hash: %s", safeHash(localHash))
	}

	// ── Step 5: Main poll loop ────────────────────────────────────────────
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	runCycle(client, cfg, &localHash, hashFile)
	for range ticker.C {
		runCycle(client, cfg, &localHash, hashFile)
	}
}

// selfRegister calls /v1/device/register with device_id + register_key.
// Polls until the customer claims the device (status = "claimed").
// Saves the token to /opt/qube/.env for persistence across restarts.
func selfRegister(client *Client, cfg Config) string {
	if cfg.RegisterKey == "" {
		log.Println("[register] No register_key available — cannot self-register")
		return ""
	}

	log.Printf("[register] Polling for claim status (device_id=%s)...", cfg.QubeID)

	for {
		data, status, err := client.doPublic("POST", "/v1/device/register", map[string]any{
			"device_id":    cfg.QubeID,
			"register_key": cfg.RegisterKey,
		})

		if err != nil {
			log.Printf("[register] request failed: %v — retrying in 30s", err)
			time.Sleep(30 * time.Second)
			continue
		}

		var resp map[string]any
		json.Unmarshal(data, &resp)

		switch status {
		case 200:
			// Claimed — we have our token
			token, _ := resp["qube_token"].(string)
			if token == "" {
				log.Printf("[register] got 200 but no qube_token in response")
				time.Sleep(30 * time.Second)
				continue
			}
			log.Printf("[register] Device claimed! Token received.")
			saveTokenToEnv(cfg, token)
			return token

		case 202:
			// Not yet claimed — customer hasn't registered the device yet
			retrySecs := 60
			if r, ok := resp["retry_secs"].(float64); ok {
				retrySecs = int(r)
			}
			log.Printf("[register] Device not yet claimed. Customer must register device key '%s' in the portal. Retrying in %ds...",
				cfg.RegisterKey, retrySecs)
			time.Sleep(time.Duration(retrySecs) * time.Second)

		case 401:
			// Wrong device_id or register_key — fatal
			log.Fatalf("[register] Invalid device_id or register_key: %s", data)

		default:
			log.Printf("[register] unexpected status %d: %s — retrying in 30s", status, data)
			time.Sleep(30 * time.Second)
		}
	}
}

// saveTokenToEnv writes QUBE_TOKEN to /opt/qube/.env so it persists across restarts.
// Next boot, the agent reads QUBE_TOKEN from env and skips self-registration.
func saveTokenToEnv(cfg Config, token string) {
	envPath := filepath.Join(cfg.WorkDir, ".env")

	// Read existing .env if it exists
	existing := ""
	if b, err := os.ReadFile(envPath); err == nil {
		existing = string(b)
	}

	// Update or add QUBE_TOKEN line
	lines := strings.Split(existing, "\n")
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
		lines = append(lines, "QUBE_ID="+cfg.QubeID)
	}

	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(envPath, []byte(newContent), 0600); err != nil {
		log.Printf("[register] WARNING: could not save token to %s: %v", envPath, err)
	} else {
		log.Printf("[register] Token saved to %s", envPath)
	}
}

// ─── Poll cycle ───────────────────────────────────────────────────────────────

func runCycle(client *Client, cfg Config, localHash *string, hashFile string) {
	log.Println("[cycle] ─────────────────────────────")
	sendHeartbeat(client)
	executeCommands(client, cfg)

	state, err := getState(client)
	if err != nil {
		log.Printf("[sync] failed to get state: %v", err)
		return
	}
	log.Printf("[sync] remote=%s local=%s", safeHash(state.Hash), safeHash(*localHash))

	if state.Hash == *localHash && state.Hash != "" {
		log.Println("[sync] hashes match — no action needed")
		return
	}

	log.Println("[sync] hash mismatch — downloading config")
	sc, err := getConfig(client)
	if err != nil {
		log.Printf("[sync] failed to get config: %v", err)
		return
	}

	if err := applyConfig(cfg, sc); err != nil {
		log.Printf("[sync] failed to apply config: %v", err)
		return
	}

	*localHash = state.Hash
	os.WriteFile(hashFile, []byte(state.Hash), 0644)
	log.Printf("[sync] config applied — new hash: %s", safeHash(state.Hash))
}

// ─── TP-API calls ─────────────────────────────────────────────────────────────

func sendHeartbeat(client *Client) {
	_, status, err := client.do("POST", "/v1/heartbeat", map[string]any{})
	if err != nil || status != 200 {
		log.Printf("[heartbeat] failed (status=%d err=%v)", status, err)
		return
	}
	log.Println("[heartbeat] sent ok")
}

func getState(client *Client) (*SyncState, error) {
	data, status, err := client.do("GET", "/v1/sync/state", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("sync/state returned %d: %s", status, data)
	}
	var s SyncState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func getConfig(client *Client) (*SyncConfig, error) {
	data, status, err := client.do("GET", "/v1/sync/config", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("sync/config returned %d: %s", status, data)
	}
	var c SyncConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func executeCommands(client *Client, cfg Config) {
	data, status, err := client.do("POST", "/v1/commands/poll", map[string]any{})
	if err != nil || status != 200 {
		log.Printf("[commands] poll failed: status=%d err=%v", status, err)
		return
	}
	var resp PollResponse
	if err := json.Unmarshal(data, &resp); err != nil || len(resp.Commands) == 0 {
		return
	}

	log.Printf("[commands] %d commands to execute", len(resp.Commands))
	for _, cmd := range resp.Commands {
		log.Printf("[cmd] executing: %s (id=%s)", cmd.Command, cmd.ID[:8])
		result, execErr := execCommand(cmd, cfg)
		status := "executed"
		if execErr != nil {
			status = "failed"
			result = map[string]any{"error": execErr.Error()}
			log.Printf("[cmd] FAILED: %s — %v", cmd.Command, execErr)
		} else {
			log.Printf("[cmd] OK: %s", cmd.Command)
		}
		client.do("POST", "/v1/commands/"+cmd.ID+"/ack", map[string]any{
			"status": status, "result": result,
		})
	}
}

// ─── Config Application ───────────────────────────────────────────────────────

func applyConfig(cfg Config, sc *SyncConfig) error {
	composePath := filepath.Join(cfg.WorkDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(sc.DockerComposeYML), 0644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	log.Printf("[apply] wrote %s", composePath)

	for path, content := range sc.CSVFiles {
		full := filepath.Join(cfg.WorkDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		log.Printf("[apply] wrote %s", path)
	}

	for path, content := range sc.EnvFiles {
		full := filepath.Join(cfg.WorkDir, path)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0600)
	}

	if len(sc.SensorMap) > 0 {
		smData, _ := json.MarshalIndent(sc.SensorMap, "", "  ")
		os.WriteFile(filepath.Join(cfg.WorkDir, "sensor_map.json"), smData, 0644)
		log.Printf("[apply] wrote sensor_map.json (%d entries)", len(sc.SensorMap))
	}

	deployDocker(cfg.WorkDir)
	return nil
}

func deployDocker(workDir string) {
	if _, err := exec.LookPath("docker"); err != nil {
		log.Println("[docker] docker not in PATH — skipping deploy (test mode)")
		return
	}

	composePath := filepath.Join(workDir, "docker-compose.yml")
	swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
	isSwarm := strings.TrimSpace(string(swarmOut)) == "active"

	if isSwarm {
		log.Println("[docker] swarm mode — running: docker stack deploy")
		cmd := exec.Command("docker", "stack", "deploy",
			"-c", composePath, "--with-registry-auth", "qube")
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[docker] stack deploy FAILED: %v\n%s", err, out)
			return
		}
		log.Printf("[docker] stack deploy OK:\n%s", out)
	} else {
		log.Println("[docker] compose mode — running: docker compose up -d")
		cmd := exec.Command("docker", "compose", "-f", composePath,
			"up", "-d", "--remove-orphans")
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[docker] compose deploy FAILED: %v\n%s", err, out)
			return
		}
		log.Printf("[docker] compose deploy OK:\n%s", out)
	}
}

// ─── Command Executor ─────────────────────────────────────────────────────────

func execCommand(cmd Command, cfg Config) (map[string]any, error) {
	switch cmd.Command {
	case "ping":
		target, _ := cmd.Payload["target"].(string)
		if target == "" { target = "8.8.8.8" }
		out, err := run("ping", "-c", "4", "-W", "2", target)
		if err != nil { return nil, fmt.Errorf("ping failed: %s", out) }
		return map[string]any{"output": out, "latency_ms": parsePingLatency(out), "target": target}, nil

	case "restart_service":
		service, _ := cmd.Payload["service"].(string)
		if service == "" { return nil, fmt.Errorf("service name required") }
		swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
		isSwarm := strings.TrimSpace(string(swarmOut)) == "active"
		var out string
		var err error
		if isSwarm {
			out, err = run("docker", "service", "update", "--force", "qube_"+service)
		} else {
			out, err = run("docker", "compose", "-f",
				filepath.Join(cfg.WorkDir, "docker-compose.yml"), "restart", service)
		}
		if err != nil { return nil, fmt.Errorf("restart failed: %s", out) }
		return map[string]any{"restarted": service, "output": out}, nil

	case "restart_qube":
		log.Println("[cmd] REBOOT requested — rebooting in 3s")
		go func() {
			time.Sleep(3 * time.Second)
			exec.Command("sudo", "reboot").Run()
		}()
		return map[string]any{"rebooting": true}, nil

	case "reload_config":
		hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
		os.WriteFile(hashFile, []byte(""), 0644)
		return map[string]any{"message": "local hash cleared — will resync on next cycle"}, nil

	case "get_logs":
		service, _ := cmd.Payload["service"].(string)
		lines := "100"
		if l, ok := cmd.Payload["lines"].(float64); ok { lines = strconv.Itoa(int(l)) }
		swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
		isSwarm := strings.TrimSpace(string(swarmOut)) == "active"
		var out string
		var err error
		if isSwarm && service != "" {
			out, err = run("docker", "service", "logs", "--tail="+lines, "--no-task-ids", "qube_"+service)
		} else {
			args := []string{"compose", "-f", filepath.Join(cfg.WorkDir, "docker-compose.yml"), "logs", "--tail=" + lines}
			if service != "" { args = append(args, service) }
			out, err = run("docker", args...)
		}
		if err != nil { return nil, fmt.Errorf("logs failed: %v", err) }
		return map[string]any{"logs": out, "service": service}, nil

	case "list_containers":
		out, err := run("docker", "ps", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
		if err != nil { return nil, fmt.Errorf("docker ps failed: %v", err) }
		return map[string]any{"containers": out}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parsePingLatency(output string) float64 {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "rtt") || strings.Contains(line, "round-trip") {
			parts := strings.Split(line, "=")
			if len(parts) < 2 { continue }
			vals := strings.Split(strings.TrimSpace(parts[1]), "/")
			if len(vals) >= 2 {
				v, _ := strconv.ParseFloat(strings.TrimSpace(vals[1]), 64)
				return v
			}
		}
	}
	return -1
}

func safeHash(h string) string {
	if len(h) == 0 { return "(none)" }
	if len(h) > 8 { return h[:8] + "..." }
	return h
}
