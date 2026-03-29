// Package agent implements the conf-agent core logic:
// WebSocket connection, HTTP polling fallback, config sync, and command execution.
package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/configs"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/docker"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/sqlite"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/tpapi"
)

// Agent is the main conf-agent runtime state.
type Agent struct {
	cfg       configs.Config
	client    *tpapi.Client
	db        *sql.DB
	localHash string
	hashFile  string

	// WebSocket
	wsConn  *websocket.Conn
	wsMu    sync.Mutex
	wsAlive bool

	// Shutdown
	done chan struct{}
}

// New creates a new Agent and restores the last known config hash from disk.
func New(cfg configs.Config, db *sql.DB) *Agent {
	a := &Agent{
		cfg:      cfg,
		client:   tpapi.NewClient(cfg.TPAPIURL, cfg.QubeID, cfg.QubeToken),
		db:       db,
		hashFile: filepath.Join(cfg.WorkDir, ".config_hash"),
		done:     make(chan struct{}),
	}
	if b, err := os.ReadFile(a.hashFile); err == nil {
		a.localHash = strings.TrimSpace(string(b))
		log.Printf("[agent] Restored local hash: %s", safeHash(a.localHash))
	}
	return a
}

// UpdateConfig updates the agent's config and rebuilds the TP-API client.
func (a *Agent) UpdateConfig(cfg configs.Config) {
	a.cfg = cfg
	a.client = tpapi.NewClient(cfg.TPAPIURL, cfg.QubeID, cfg.QubeToken)
}

// Start launches the WebSocket loop and runs the polling loop (blocking).
func (a *Agent) Start() {
	go a.wsLoop()
	a.pollLoop()
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

		if !a.wsAlive {
			log.Println("[ws] connection failed — retrying in 15s")
			time.Sleep(15 * time.Second)
			continue
		}

		a.readWS()

		a.wsMu.Lock()
		a.wsAlive = false
		a.wsMu.Unlock()
		log.Println("[ws] disconnected — reconnecting in 5s")
		time.Sleep(5 * time.Second)
	}
}

func (a *Agent) connectWS() {
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
			a.wsSend(tpapi.WSMessage{
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

		var msg tpapi.WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[ws] invalid message: %v", err)
			continue
		}

		a.handleWSMessage(msg)
	}
}

func (a *Agent) handleWSMessage(msg tpapi.WSMessage) {
	switch msg.Type {

	case "config_push":
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		hash, _ := payload["hash"].(string)
		log.Printf("[ws] config_push received (hash=%s)", safeHash(hash))

		if hash != "" && hash != a.localHash {
			log.Println("[ws] hash mismatch — syncing config via HTTP")
			a.syncConfig()

			a.wsSend(tpapi.WSMessage{
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
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		cmdID, _ := payload["command_id"].(string)
		command, _ := payload["command"].(string)
		cmdPayload, _ := payload["payload"].(map[string]any)

		log.Printf("[ws] command received: %s (id=%s)", command, safeHash(cmdID))

		cmd := tpapi.Command{ID: cmdID, Command: command, Payload: cmdPayload}
		result, execErr := ExecCommand(cmd, a.cfg)
		status := "executed"
		if execErr != nil {
			status = "failed"
			result = map[string]any{"error": execErr.Error()}
			log.Printf("[ws] command FAILED: %s — %v", command, execErr)
		} else {
			log.Printf("[ws] command OK: %s", command)
		}

		a.wsSend(tpapi.WSMessage{
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
		log.Println("[ws] heartbeat ack received")

	default:
		log.Printf("[ws] unknown message type: %s", msg.Type)
	}
}

func (a *Agent) wsSend(msg tpapi.WSMessage) {
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
		log.Println("[cycle] WebSocket connected — checking state only")
		a.checkAndSync()
		return
	}

	log.Println("[cycle] WebSocket disconnected — full poll cycle")
	a.sendHeartbeat()
	a.executeCommandsHTTP()
	a.checkAndSync()
}

func (a *Agent) checkAndSync() {
	state, err := a.getState()
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
	sc, err := a.getConfig()
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

func (a *Agent) sendHeartbeat() {
	_, status, err := a.client.Do("POST", "/v1/heartbeat", map[string]any{})
	if err != nil || status != 200 {
		log.Printf("[heartbeat] failed (status=%d err=%v)", status, err)
		return
	}
	log.Println("[heartbeat] sent ok")
}

func (a *Agent) getState() (*tpapi.SyncState, error) {
	data, status, err := a.client.Do("GET", "/v1/sync/state", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("sync/state returned %d: %s", status, data)
	}
	var s tpapi.SyncState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (a *Agent) getConfig() (*tpapi.SyncConfig, error) {
	data, status, err := a.client.Do("GET", "/v1/sync/config", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("sync/config returned %d: %s", status, data)
	}
	var c tpapi.SyncConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (a *Agent) executeCommandsHTTP() {
	data, status, err := a.client.Do("POST", "/v1/commands/poll", map[string]any{})
	if err != nil || status != 200 {
		log.Printf("[commands] poll failed: status=%d err=%v", status, err)
		return
	}
	var resp tpapi.PollResponse
	if err := json.Unmarshal(data, &resp); err != nil || len(resp.Commands) == 0 {
		return
	}

	log.Printf("[commands] %d commands to execute", len(resp.Commands))
	for _, cmd := range resp.Commands {
		log.Printf("[cmd] executing: %s (id=%s)", cmd.Command, safeHash(cmd.ID))
		result, execErr := ExecCommand(cmd, a.cfg)
		ackStatus := "executed"
		if execErr != nil {
			ackStatus = "failed"
			result = map[string]any{"error": execErr.Error()}
			log.Printf("[cmd] FAILED: %s — %v", cmd.Command, execErr)
		} else {
			log.Printf("[cmd] OK: %s", cmd.Command)
		}
		a.client.Do("POST", "/v1/commands/"+cmd.ID+"/ack", map[string]any{
			"status": ackStatus, "result": result,
		})
	}
}

// ─── Config Application ─────────────────────────────────────────────────────

func (a *Agent) applyConfig(sc *tpapi.SyncConfig) error {
	// 1. Write docker-compose.yml
	composePath := filepath.Join(a.cfg.WorkDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(sc.DockerComposeYML), 0644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	log.Printf("[apply] wrote %s", composePath)

	// 2. Write config to SQLite (readers, sensors, settings)
	if err := sqlite.WriteConfig(a.db, sc); err != nil {
		return fmt.Errorf("write sqlite: %w", err)
	}

	// 3. Deploy Docker
	docker.Deploy(a.cfg.WorkDir)
	return nil
}

// ─── Self-Registration ───────────────────────────────────────────────────────

// SelfRegister polls the TP-API until the device is claimed and returns the QUBE_TOKEN.
func SelfRegister(client *tpapi.Client, qubeID, registerKey, workDir string) string {
	if registerKey == "" {
		log.Println("[register] No register_key available — cannot self-register")
		return ""
	}

	log.Printf("[register] Polling for claim status (device_id=%s)...", qubeID)

	for {
		data, status, err := client.DoPublic("POST", "/v1/device/register", map[string]any{
			"device_id":    qubeID,
			"register_key": registerKey,
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
			saveTokenToEnv(workDir, qubeID, token)
			return token

		case 202:
			retrySecs := 60
			if r, ok := resp["retry_secs"].(float64); ok {
				retrySecs = int(r)
			}
			log.Printf("[register] Device not yet claimed. Register key '%s' in portal. Retrying in %ds...",
				registerKey, retrySecs)
			time.Sleep(time.Duration(retrySecs) * time.Second)

		case 401:
			log.Fatalf("[register] Invalid device_id or register_key: %s", data)

		default:
			log.Printf("[register] unexpected status %d: %s — retrying in 30s", status, data)
			time.Sleep(30 * time.Second)
		}
	}
}

func saveTokenToEnv(workDir, qubeID, token string) {
	envPath := filepath.Join(workDir, ".env")
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
			lines[i] = "QUBE_ID=" + qubeID
			foundID = true
		}
	}
	if !foundToken {
		lines = append(lines, "QUBE_TOKEN="+token)
	}
	if !foundID {
		lines = append(lines, "QUBE_ID="+qubeID)
	}

	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		log.Printf("[register] WARNING: could not save token to %s: %v", envPath, err)
	} else {
		log.Printf("[register] Token saved to %s", envPath)
	}
}

// ─── Command Executor ────────────────────────────────────────────────────────

// ExecCommand executes a remote command and returns the result.
func ExecCommand(cmd tpapi.Command, cfg configs.Config) (map[string]any, error) {
	switch cmd.Command {
	case "ping":
		target, _ := cmd.Payload["target"].(string)
		if target == "" {
			target = "8.8.8.8"
		}
		out, err := docker.Run("ping", "-c", "4", "-W", "2", target)
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
			service = readerID
		}
		out, err := docker.RestartService(service, cfg.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("restart failed: %s", out)
		}
		return map[string]any{"restarted": service, "output": out}, nil

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
		out, err := docker.Run("docker", "stop", service)
		if err != nil {
			return nil, fmt.Errorf("stop failed: %s", out)
		}
		return map[string]any{"stopped": service, "output": out}, nil

	case "reload_config":
		hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
		os.WriteFile(hashFile, []byte(""), 0644)
		return map[string]any{"message": "local hash cleared — will resync on next cycle"}, nil

	case "update_sqlite":
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
			out, err = docker.Run("docker", "service", "logs", "--tail="+lines, "--no-task-ids", "qube_"+service)
		} else {
			args := []string{"compose", "-f", filepath.Join(cfg.WorkDir, "docker-compose.yml"), "logs", "--tail=" + lines}
			if service != "" {
				args = append(args, service)
			}
			out, err = docker.Run("docker", args...)
		}
		if err != nil {
			return nil, fmt.Errorf("logs failed: %v", err)
		}
		return map[string]any{"logs": out, "service": service}, nil

	case "list_containers":
		out, err := docker.Run("docker", "ps", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
		if err != nil {
			return nil, fmt.Errorf("docker ps failed: %v", err)
		}
		return map[string]any{"containers": out}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

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
