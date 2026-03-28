// conf-agent v2 — Qube Enterprise Edge Agent
//
// Primary: WebSocket connection to cloud (real-time config push + commands)
// Fallback: HTTP polling to TP-API (when WS disconnected)
//
// Startup flow:
//   1. Read /boot/mit.txt → get device_id + register_key
//   2. If QUBE_TOKEN not set → call /v1/device/register (HTTP polling)
//   3. Connect WebSocket to cloud (ws://<cloud>:8080/ws?qube_id=X&token=Y)
//   4. On WS: receive config_push + commands in real-time
//   5. Fallback: poll TP-API every 30s if WebSocket is disconnected
//   6. Config stored in SQLite at SQLITE_PATH (shared with reader containers)
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	TPAPIURL     string        // HTTP polling fallback (port 8081)
	CloudWSURL   string        // WebSocket primary (port 8080)
	QubeID       string
	QubeToken    string
	RegisterKey  string        // from /boot/mit.txt — used for self-registration
	WorkDir      string
	SQLitePath   string        // /opt/qube/data/qube.db
	PollInterval time.Duration
	MitTxtPath   string
}

// MitTxt holds the device identity written at flash time by image-install.sh
type MitTxt struct {
	DeviceID    string
	DeviceName  string
	DeviceType  string
	RegisterKey string
	MaintainKey string
}

func loadConfig() Config {
	interval, _ := strconv.Atoi(getenv("POLL_INTERVAL", "30"))
	return Config{
		TPAPIURL:     getenv("TPAPI_URL", "http://localhost:8081"),
		CloudWSURL:   getenv("CLOUD_WS_URL", "ws://localhost:8080/ws"),
		QubeID:       getenv("QUBE_ID", ""),
		QubeToken:    getenv("QUBE_TOKEN", ""),
		RegisterKey:  getenv("REGISTER_KEY", ""),
		WorkDir:      getenv("WORK_DIR", "/opt/qube"),
		SQLitePath:   getenv("SQLITE_PATH", "/opt/qube/data/qube.db"),
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
		case "deviceid":
			m.DeviceID = val
		case "devicename":
			m.DeviceName = val
		case "devicetype":
			m.DeviceType = val
		case "register":
			m.RegisterKey = val
		case "maintain":
			m.MaintainKey = val
		}
	}
	if m.DeviceID == "" {
		return nil, fmt.Errorf("deviceid not found in %s", path)
	}
	return m, nil
}

// ─── TP-API HTTP Client (fallback) ───────────────────────────────────────────

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

// ─── Data Types ──────────────────────────────────────────────────────────────

type SyncState struct {
	Hash          string `json:"hash"`
	ConfigVersion int    `json:"config_version"`
	UpdatedAt     string `json:"updated_at"`
}

type SyncConfig struct {
	Hash               string              `json:"hash"`
	ConfigVersion      int                 `json:"config_version"`
	DockerComposeYML   string              `json:"docker_compose_yml"`
	Readers            []ReaderConfig      `json:"readers"`
	Containers         []ContainerConfig   `json:"containers"`
	CoreSwitchSettings map[string]string   `json:"coreswitch_settings"`
	TelemetrySettings  []map[string]any    `json:"telemetry_settings"`
}

type ReaderConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Protocol   string         `json:"protocol"`
	ConfigJSON map[string]any `json:"config_json"`
	Status     string         `json:"status"`
	Sensors    []SensorConfig `json:"sensors"`
}

type SensorConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ConfigJSON map[string]any `json:"config_json"`
	TagsJSON   map[string]any `json:"tags_json"`
	Output     string         `json:"output"`
	TableName  string         `json:"table_name"`
}

type ContainerConfig struct {
	ID         string         `json:"id"`
	ReaderID   string         `json:"reader_id"`
	Image      string         `json:"image"`
	ServiceName string        `json:"service_name"`
	EnvJSON    map[string]any `json:"env_json"`
	Protocol   string         `json:"protocol"`
}

type WSMessage struct {
	Type      string `json:"type"`
	QubeID    string `json:"qube_id"`
	ID        string `json:"id"`
	Payload   any    `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

type Command struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Payload map[string]any `json:"payload"`
}

type PollResponse struct {
	Commands []Command `json:"commands"`
}

// ─── Agent State ─────────────────────────────────────────────────────────────

type Agent struct {
	cfg       Config
	client    *Client
	db        *sql.DB
	localHash string
	hashFile  string

	// WebSocket
	wsConn   *websocket.Conn
	wsMu     sync.Mutex
	wsAlive  bool

	// Shutdown
	done chan struct{}
}

func newAgent(cfg Config) *Agent {
	return &Agent{
		cfg:      cfg,
		client:   newClient(cfg),
		hashFile: filepath.Join(cfg.WorkDir, ".config_hash"),
		done:     make(chan struct{}),
	}
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()

	// ── Step 1: Read /boot/mit.txt ────────────────────────────────────────
	mit, err := readMitTxt(cfg.MitTxtPath)
	if err != nil {
		log.Printf("[agent] Could not read %s: %v", cfg.MitTxtPath, err)
		log.Printf("[agent] Falling back to env vars (QUBE_ID, REGISTER_KEY)")
	} else {
		log.Printf("[agent] Device identity from mit.txt: id=%s reg=%s type=%s",
			mit.DeviceID, mit.RegisterKey, mit.DeviceType)
		if cfg.QubeID == "" {
			cfg.QubeID = mit.DeviceID
		}
		if cfg.RegisterKey == "" {
			cfg.RegisterKey = mit.RegisterKey
		}
	}

	if cfg.QubeID == "" {
		log.Fatal("[agent] Cannot determine device ID. Set QUBE_ID or ensure /boot/mit.txt exists.")
	}

	agent := newAgent(cfg)

	// ── Step 2: Wait for TP-API ──────────────────────────────────────────
	log.Printf("[agent] Starting v2 — QubeID=%s TPAPI=%s WS=%s Interval=%s",
		cfg.QubeID, cfg.TPAPIURL, cfg.CloudWSURL, cfg.PollInterval)

	for {
		_, status, err := agent.client.do("GET", "/health", nil)
		if err == nil && status == 200 {
			log.Println("[agent] TP-API reachable")
			break
		}
		log.Printf("[agent] TP-API not reachable (err=%v status=%d), retrying in 10s...", err, status)
		time.Sleep(10 * time.Second)
	}

	// ── Step 3: Self-register if no token ────────────────────────────────
	if cfg.QubeToken == "" {
		log.Println("[agent] No QUBE_TOKEN — attempting self-registration...")
		cfg.QubeToken = selfRegister(agent.client, cfg)
		agent.cfg = cfg
		agent.client = newClient(cfg)
	}
	if cfg.QubeToken == "" {
		log.Fatal("[agent] Could not obtain QUBE_TOKEN. Device may not be claimed yet.")
	}

	// ── Step 4: Initialize SQLite ────────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0755); err != nil {
		log.Fatalf("[agent] Cannot create SQLite directory: %v", err)
	}
	agent.db, err = sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		log.Fatalf("[agent] Cannot open SQLite: %v", err)
	}
	defer agent.db.Close()
	initSQLite(agent.db)
	log.Printf("[agent] SQLite initialized at %s", cfg.SQLitePath)

	// ── Step 5: Load last known hash ─────────────────────────────────────
	if b, err := os.ReadFile(agent.hashFile); err == nil {
		agent.localHash = strings.TrimSpace(string(b))
		log.Printf("[agent] Restored local hash: %s", safeHash(agent.localHash))
	}

	// ── Step 6: Start WebSocket + polling loop ───────────────────────────
	go agent.wsLoop()
	agent.pollLoop()
}

// ─── SQLite Schema ───────────────────────────────────────────────────────────

func initSQLite(db *sql.DB) {
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	db.Exec(`CREATE TABLE IF NOT EXISTS readers (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		protocol    TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		status      TEXT NOT NULL DEFAULT 'active'
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS sensors (
		id          TEXT PRIMARY KEY,
		reader_id   TEXT NOT NULL REFERENCES readers(id),
		name        TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		tags_json   TEXT NOT NULL DEFAULT '{}',
		output      TEXT NOT NULL DEFAULT 'influxdb',
		table_name  TEXT NOT NULL DEFAULT ''
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS coreswitch_settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS telemetry_settings (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		device     TEXT NOT NULL,
		reading    TEXT NOT NULL,
		agg_time_min INTEGER NOT NULL DEFAULT 1,
		agg_func   TEXT NOT NULL DEFAULT 'last',
		sensor_id  TEXT NOT NULL,
		tag_names  TEXT NOT NULL DEFAULT '[]'
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
}

// ─── WebSocket Loop ──────────────────────────────────────────────────────────

func (a *Agent) wsLoop() {
	for {
		select {
		case <-a.done:
			return
		default:
		}

		a.connectWS()

		// If connection failed, wait before retrying
		if !a.wsAlive {
			log.Println("[ws] connection failed — retrying in 15s")
			time.Sleep(15 * time.Second)
			continue
		}

		// Read messages until disconnect
		a.readWS()

		// Disconnected — mark and retry
		a.wsMu.Lock()
		a.wsAlive = false
		a.wsMu.Unlock()
		log.Println("[ws] disconnected — reconnecting in 5s")
		time.Sleep(5 * time.Second)
	}
}

func (a *Agent) connectWS() {
	// Compute HMAC token for WS auth (same as TP-API)
	token := a.cfg.QubeToken

	u, err := url.Parse(a.cfg.CloudWSURL)
	if err != nil {
		log.Printf("[ws] invalid WS URL: %v", err)
		return
	}
	q := u.Query()
	q.Set("qube_id", a.cfg.QubeID)
	q.Set("token", token)
	u.RawQuery = q.Encode()

	log.Printf("[ws] connecting to %s", u.Host)

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("[ws] dial failed: %v", err)
		return
	}

	a.wsMu.Lock()
	a.wsConn = conn
	a.wsAlive = true
	a.wsMu.Unlock()

	log.Printf("[ws] connected to cloud")

	// Start heartbeat sender
	go a.wsHeartbeatLoop()
}

func (a *Agent) wsHeartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.wsMu.Lock()
			alive := a.wsAlive
			a.wsMu.Unlock()
			if !alive {
				return
			}
			a.wsSend(WSMessage{
				Type:      "heartbeat",
				QubeID:    a.cfg.QubeID,
				Timestamp: time.Now().UnixMilli(),
			})
		case <-a.done:
			return
		}
	}
}

func (a *Agent) readWS() {
	for {
		_, data, err := a.wsConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] read error: %v", err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[ws] invalid message: %v", err)
			continue
		}

		a.handleWSMessage(msg)
	}
}

func (a *Agent) handleWSMessage(msg WSMessage) {
	switch msg.Type {

	case "config_push":
		// Cloud telling us config has changed — trigger sync
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		hash, _ := payload["hash"].(string)
		log.Printf("[ws] config_push received (hash=%s)", safeHash(hash))

		if hash != "" && hash != a.localHash {
			log.Println("[ws] hash mismatch — syncing config via HTTP")
			a.syncConfig()

			// Send config_ack
			a.wsSend(WSMessage{
				Type:   "config_ack",
				QubeID: a.cfg.QubeID,
				Payload: map[string]any{
					"hash":   a.localHash,
					"status": "applied",
				},
				Timestamp: time.Now().UnixMilli(),
			})
		}

	case "command":
		// Real-time command from cloud via WebSocket
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		cmdID, _ := payload["command_id"].(string)
		command, _ := payload["command"].(string)
		cmdPayload, _ := payload["payload"].(map[string]any)

		log.Printf("[ws] command received: %s (id=%s)", command, safeHash(cmdID))

		cmd := Command{ID: cmdID, Command: command, Payload: cmdPayload}
		result, execErr := execCommand(cmd, a.cfg)
		status := "executed"
		if execErr != nil {
			status = "failed"
			result = map[string]any{"error": execErr.Error()}
			log.Printf("[ws] command FAILED: %s — %v", command, execErr)
		} else {
			log.Printf("[ws] command OK: %s", command)
		}

		// Send command_ack via WebSocket
		a.wsSend(WSMessage{
			Type:   "command_ack",
			QubeID: a.cfg.QubeID,
			Payload: map[string]any{
				"command_id": cmdID,
				"status":     status,
				"result":     result,
			},
			Timestamp: time.Now().UnixMilli(),
		})

	case "heartbeat_ack":
		// Server acknowledged our heartbeat — nothing to do
		log.Println("[ws] heartbeat ack received")

	default:
		log.Printf("[ws] unknown message type: %s", msg.Type)
	}
}

func (a *Agent) wsSend(msg WSMessage) {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()

	if !a.wsAlive || a.wsConn == nil {
		return
	}

	a.wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return
	}
	if err := a.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("[ws] write error: %v", err)
	}
}

// ─── Polling Loop (fallback when WS disconnected) ────────────────────────────

func (a *Agent) pollLoop() {
	// Run first cycle immediately
	a.runPollCycle()

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.runPollCycle()
		case <-a.done:
			return
		}
	}
}

func (a *Agent) runPollCycle() {
	log.Println("[cycle] ─────────────────────────────")

	a.wsMu.Lock()
	wsUp := a.wsAlive
	a.wsMu.Unlock()

	if wsUp {
		// WebSocket is handling real-time — only do state check
		log.Println("[cycle] WebSocket connected — checking state only")
		a.checkAndSync()
		return
	}

	// Full polling cycle when WebSocket is down
	log.Println("[cycle] WebSocket disconnected — full poll cycle")
	sendHeartbeat(a.client)
	executeCommandsHTTP(a.client, a.cfg)
	a.checkAndSync()
}

func (a *Agent) checkAndSync() {
	state, err := getState(a.client)
	if err != nil {
		log.Printf("[sync] failed to get state: %v", err)
		return
	}
	log.Printf("[sync] remote=%s local=%s version=%d",
		safeHash(state.Hash), safeHash(a.localHash), state.ConfigVersion)

	if state.Hash == a.localHash && state.Hash != "" {
		log.Println("[sync] hashes match — no action needed")
		return
	}

	a.syncConfig()
}

func (a *Agent) syncConfig() {
	log.Println("[sync] downloading config...")
	sc, err := getConfig(a.client)
	if err != nil {
		log.Printf("[sync] failed to get config: %v", err)
		return
	}

	if err := a.applyConfig(sc); err != nil {
		log.Printf("[sync] failed to apply config: %v", err)
		return
	}

	a.localHash = sc.Hash
	os.WriteFile(a.hashFile, []byte(sc.Hash), 0644)
	log.Printf("[sync] config applied — hash=%s version=%d", safeHash(sc.Hash), sc.ConfigVersion)
}

// ─── TP-API HTTP Calls ───────────────────────────────────────────────────────

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

func executeCommandsHTTP(client *Client, cfg Config) {
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
		log.Printf("[cmd] executing: %s (id=%s)", cmd.Command, safeHash(cmd.ID))
		result, execErr := execCommand(cmd, cfg)
		ackStatus := "executed"
		if execErr != nil {
			ackStatus = "failed"
			result = map[string]any{"error": execErr.Error()}
			log.Printf("[cmd] FAILED: %s — %v", cmd.Command, execErr)
		} else {
			log.Printf("[cmd] OK: %s", cmd.Command)
		}
		client.do("POST", "/v1/commands/"+cmd.ID+"/ack", map[string]any{
			"status": ackStatus, "result": result,
		})
	}
}

// ─── Config Application ─────────────────────────────────────────────────────

func (a *Agent) applyConfig(sc *SyncConfig) error {
	// 1. Write docker-compose.yml
	composePath := filepath.Join(a.cfg.WorkDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(sc.DockerComposeYML), 0644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	log.Printf("[apply] wrote %s", composePath)

	// 2. Write config to SQLite (readers, sensors, settings)
	if err := a.writeSQLite(sc); err != nil {
		return fmt.Errorf("write sqlite: %w", err)
	}

	// 3. Deploy Docker
	deployDocker(a.cfg.WorkDir)
	return nil
}

func (a *Agent) writeSQLite(sc *SyncConfig) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing data
	tx.Exec("DELETE FROM sensors")
	tx.Exec("DELETE FROM readers")
	tx.Exec("DELETE FROM coreswitch_settings")
	tx.Exec("DELETE FROM telemetry_settings")

	// Write readers + sensors
	for _, rd := range sc.Readers {
		cfgJSON, _ := json.Marshal(rd.ConfigJSON)
		tx.Exec(`INSERT INTO readers (id, name, protocol, config_json, status) VALUES (?, ?, ?, ?, ?)`,
			rd.ID, rd.Name, rd.Protocol, string(cfgJSON), rd.Status)

		for _, s := range rd.Sensors {
			sCfgJSON, _ := json.Marshal(s.ConfigJSON)
			sTagsJSON, _ := json.Marshal(s.TagsJSON)
			tx.Exec(`INSERT INTO sensors (id, reader_id, name, config_json, tags_json, output, table_name)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				s.ID, rd.ID, s.Name, string(sCfgJSON), string(sTagsJSON), s.Output, s.TableName)
		}
	}

	// Write coreswitch settings
	for k, v := range sc.CoreSwitchSettings {
		tx.Exec(`INSERT INTO coreswitch_settings (key, value) VALUES (?, ?)`, k, v)
	}

	// Write telemetry settings
	for _, ts := range sc.TelemetrySettings {
		device, _ := ts["device"].(string)
		reading, _ := ts["reading"].(string)
		aggTimeMin := 1
		if v, ok := ts["agg_time_min"].(float64); ok {
			aggTimeMin = int(v)
		}
		aggFunc, _ := ts["agg_func"].(string)
		if aggFunc == "" {
			aggFunc = "last"
		}
		sensorID, _ := ts["sensor_id"].(string)
		tagNamesJSON := "[]"
		if tn, ok := ts["tag_names"]; ok {
			b, _ := json.Marshal(tn)
			tagNamesJSON = string(b)
		}
		tx.Exec(`INSERT INTO telemetry_settings (device, reading, agg_time_min, agg_func, sensor_id, tag_names)
			VALUES (?, ?, ?, ?, ?, ?)`,
			device, reading, aggTimeMin, aggFunc, sensorID, tagNamesJSON)
	}

	// Write meta
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('config_hash', ?)`, sc.Hash)
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('config_version', ?)`,
		strconv.Itoa(sc.ConfigVersion))
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('updated_at', ?)`,
		time.Now().UTC().Format(time.RFC3339))

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	readerCount := len(sc.Readers)
	sensorCount := 0
	for _, rd := range sc.Readers {
		sensorCount += len(rd.Sensors)
	}
	log.Printf("[sqlite] wrote %d readers, %d sensors, %d coreswitch settings, %d telemetry settings",
		readerCount, sensorCount, len(sc.CoreSwitchSettings), len(sc.TelemetrySettings))
	return nil
}

// ─── Docker Deploy ───────────────────────────────────────────────────────────

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

// ─── Command Executor ────────────────────────────────────────────────────────

func execCommand(cmd Command, cfg Config) (map[string]any, error) {
	switch cmd.Command {
	case "ping":
		target, _ := cmd.Payload["target"].(string)
		if target == "" {
			target = "8.8.8.8"
		}
		out, err := run("ping", "-c", "4", "-W", "2", target)
		if err != nil {
			return nil, fmt.Errorf("ping failed: %s", out)
		}
		return map[string]any{"output": out, "latency_ms": parsePingLatency(out), "target": target}, nil

	case "restart_reader":
		readerID, _ := cmd.Payload["reader_id"].(string)
		service, _ := cmd.Payload["service"].(string)
		if service == "" && readerID == "" {
			return nil, fmt.Errorf("reader_id or service name required")
		}
		if service == "" {
			service = readerID // Fallback — container service_name often matches reader_id
		}
		return restartService(service, cfg)

	case "restart_qube":
		log.Println("[cmd] REBOOT requested — rebooting in 3s")
		go func() {
			time.Sleep(3 * time.Second)
			exec.Command("sudo", "reboot").Run()
		}()
		return map[string]any{"rebooting": true}, nil

	case "stop_container":
		service, _ := cmd.Payload["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("service name required")
		}
		out, err := run("docker", "stop", service)
		if err != nil {
			return nil, fmt.Errorf("stop failed: %s", out)
		}
		return map[string]any{"stopped": service, "output": out}, nil

	case "reload_config":
		hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
		os.WriteFile(hashFile, []byte(""), 0644)
		return map[string]any{"message": "local hash cleared — will resync on next cycle"}, nil

	case "update_sqlite":
		// Force re-download of config and rewrite SQLite
		hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
		os.WriteFile(hashFile, []byte(""), 0644)
		return map[string]any{"message": "SQLite update triggered — will resync on next cycle"}, nil

	case "get_logs":
		service, _ := cmd.Payload["service"].(string)
		lines := "100"
		if l, ok := cmd.Payload["lines"].(float64); ok {
			lines = strconv.Itoa(int(l))
		}
		swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
		isSwarm := strings.TrimSpace(string(swarmOut)) == "active"
		var out string
		var err error
		if isSwarm && service != "" {
			out, err = run("docker", "service", "logs", "--tail="+lines, "--no-task-ids", "qube_"+service)
		} else {
			args := []string{"compose", "-f", filepath.Join(cfg.WorkDir, "docker-compose.yml"), "logs", "--tail=" + lines}
			if service != "" {
				args = append(args, service)
			}
			out, err = run("docker", args...)
		}
		if err != nil {
			return nil, fmt.Errorf("logs failed: %v", err)
		}
		return map[string]any{"logs": out, "service": service}, nil

	case "list_containers":
		out, err := run("docker", "ps", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
		if err != nil {
			return nil, fmt.Errorf("docker ps failed: %v", err)
		}
		return map[string]any{"containers": out}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

func restartService(service string, cfg Config) (map[string]any, error) {
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
	if err != nil {
		return nil, fmt.Errorf("restart failed: %s", out)
	}
	return map[string]any{"restarted": service, "output": out}, nil
}

// ─── Self-Registration ───────────────────────────────────────────────────────

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
			retrySecs := 60
			if r, ok := resp["retry_secs"].(float64); ok {
				retrySecs = int(r)
			}
			log.Printf("[register] Device not yet claimed. Register key '%s' in portal. Retrying in %ds...",
				cfg.RegisterKey, retrySecs)
			time.Sleep(time.Duration(retrySecs) * time.Second)

		case 401:
			log.Fatalf("[register] Invalid device_id or register_key: %s", data)

		default:
			log.Printf("[register] unexpected status %d: %s — retrying in 30s", status, data)
			time.Sleep(30 * time.Second)
		}
	}
}

func saveTokenToEnv(cfg Config, token string) {
	envPath := filepath.Join(cfg.WorkDir, ".env")
	existing := ""
	if b, err := os.ReadFile(envPath); err == nil {
		existing = string(b)
	}

	lines := strings.Split(existing, "\n")
	foundToken, foundID := false, false
	for i, line := range lines {
		if strings.HasPrefix(line, "QUBE_TOKEN=") {
			lines[i] = "QUBE_TOKEN=" + token
			foundToken = true
		}
		if strings.HasPrefix(line, "QUBE_ID=") {
			lines[i] = "QUBE_ID=" + cfg.QubeID
			foundID = true
		}
	}
	if !foundToken {
		lines = append(lines, "QUBE_TOKEN="+token)
	}
	if !foundID {
		lines = append(lines, "QUBE_ID="+cfg.QubeID)
	}

	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		log.Printf("[register] WARNING: could not save token to %s: %v", envPath, err)
	} else {
		log.Printf("[register] Token saved to %s", envPath)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parsePingLatency(output string) float64 {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "rtt") || strings.Contains(line, "round-trip") {
			parts := strings.Split(line, "=")
			if len(parts) < 2 {
				continue
			}
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
	if len(h) == 0 {
		return "(none)"
	}
	if len(h) > 8 {
		return h[:8] + "..."
	}
	return h
}

