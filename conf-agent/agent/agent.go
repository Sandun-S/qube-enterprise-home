// Package agent implements the conf-agent v2 core: WebSocket cloud sync,
// HTTP polling fallback, config application, and command execution.
//
// Command dispatch supports all v1 script-based operations (set_wifi, set_eth,
// set_firewall, etc.) plus enterprise-specific commands (restart_reader, get_logs…).
// Commands arrive via WebSocket "command" messages or TP-API /v1/commands/poll.
//
// Scripts run from WorkDir (default /opt/qube). In container deployments the
// conf-agent container must have:
//   - /boot/mit.txt mounted read-only (device identity)
//   - /etc/netplan/ bind-mounted for network config commands
//   - Host PID namespace or sudo for reboot/shutdown
//   - Docker socket for container management
package agent

import (
	"database/sql"
	"encoding/base64"
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
	"github.com/sirupsen/logrus"

	"conf-agent/configs"
	"conf-agent/docker"
	"conf-agent/sqlite"
	"conf-agent/tpapi"
)

// ─── Agent ────────────────────────────────────────────────────────────────────

// Agent is the main conf-agent v2 runtime.
type Agent struct {
	cfg       *configs.Config
	client    *tpapi.Client
	db        *sql.DB
	logger    *logrus.Logger
	localHash string
	hashFile  string

	// WebSocket
	wsConn  *websocket.Conn
	wsMu    sync.Mutex
	wsAlive bool

	done chan struct{}
}

// New creates a new Agent and restores the last known config hash from disk.
func New(cfg *configs.Config, db *sql.DB, logger *logrus.Logger) *Agent {
	a := &Agent{
		cfg:      cfg,
		client:   tpapi.NewClient(cfg.TPAPIURL, cfg.QubeID, cfg.QubeToken),
		db:       db,
		logger:   logger,
		hashFile: filepath.Join(cfg.WorkDir, ".config_hash"),
		done:     make(chan struct{}),
	}
	if b, err := os.ReadFile(a.hashFile); err == nil {
		a.localHash = strings.TrimSpace(string(b))
		log.Printf("[agent] Restored local hash: %s", safeHash(a.localHash))
	}
	return a
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
	u, err := url.Parse(a.cfg.CloudWSURL)
	if err != nil {
		log.Printf("[ws] invalid WS URL: %v", err)
		return
	}
	q := u.Query()
	q.Set("qube_id", a.cfg.QubeID)
	q.Set("token", a.cfg.QubeToken)
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
		log.Printf("[ws] config_push (hash=%s)", safeHash(hash))

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

		log.Printf("[ws] command: %s (id=%s)", command, safeHash(cmdID))
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
		log.Println("[ws] heartbeat ack")

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
		return
	}
	a.wsConn.WriteMessage(websocket.TextMessage, data)
}

// ─── Polling Loop (fallback) ─────────────────────────────────────────────────

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
		log.Println("[cycle] WebSocket up — checking state only")
		a.checkAndSync()
		return
	}

	log.Println("[cycle] WebSocket down — full poll cycle")
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
	log.Printf("[sync] applied — hash=%s version=%d", safeHash(sc.Hash), sc.ConfigVersion)
}

// ─── TP-API Calls ─────────────────────────────────────────────────────────────

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
	if status == 401 {
		log.Println("[sync] token rejected (401) — token may have been invalidated by unclaim/reclaim, triggering re-registration")
		a.reRegister()
		return nil, fmt.Errorf("sync/state returned 401: token invalid, re-registration triggered")
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

// ─── Config Application ──────────────────────────────────────────────────────

func (a *Agent) applyConfig(sc *tpapi.SyncConfig) error {
	composePath := filepath.Join(a.cfg.WorkDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(sc.DockerComposeYML), 0644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	log.Printf("[apply] wrote %s", composePath)

	if err := sqlite.WriteConfig(a.db, sc); err != nil {
		return fmt.Errorf("write sqlite: %w", err)
	}

	docker.Deploy(a.cfg.WorkDir)
	return nil
}

// ─── Self-Registration ────────────────────────────────────────────────────────

// SelfRegister polls TP-API until the device is claimed and returns QUBE_TOKEN.
func SelfRegister(client *tpapi.Client, qubeID, registerKey, workDir string) string {
	if registerKey == "" {
		log.Println("[register] No register_key — cannot self-register")
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
				log.Printf("[register] got 200 but no qube_token")
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
			log.Printf("[register] Not yet claimed. Register key '%s' in portal. Retry in %ds...",
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

// reRegister wipes the cached token and re-registers with the cloud.
// Called when the TP-API returns 401 — happens after unclaim/reclaim.
func (a *Agent) reRegister() {
	// Wipe cached token so it won't be reused
	saveTokenToEnv(a.cfg.WorkDir, a.cfg.QubeID, "")
	a.cfg.QubeToken = ""

	// Re-register — blocks until the device is claimed
	bootstrapClient := tpapi.NewClient(a.cfg.TPAPIURL, a.cfg.QubeID, "")
	newToken := SelfRegister(bootstrapClient, a.cfg.QubeID, a.cfg.RegisterKey, a.cfg.WorkDir)
	if newToken == "" {
		log.Println("[register] re-registration failed — will retry on next cycle")
		return
	}

	// Update the running agent with the new token
	a.cfg.QubeToken = newToken
	a.client = tpapi.NewClient(a.cfg.TPAPIURL, a.cfg.QubeID, newToken)
	os.Setenv("QUBE_TOKEN", newToken)
	log.Printf("[register] re-registration successful — new token obtained")
}

// ─── Command Executor ─────────────────────────────────────────────────────────
//
// ExecCommand handles all remote commands dispatched by the enterprise cloud API.
// Commands are grouped into:
//   - Enterprise commands: ping, restart_reader, get_logs, list_containers, etc.
//   - Device management (v1 compat): reboot, shutdown, get_info, set_wifi, etc.
//   - File operations (v1 compat): put_file, get_file
//   - Service management (v1 compat): service_add, service_rm, service_edit
//
// Script-based commands run from cfg.WorkDir using bash.
// All paths are validated to prevent path traversal.

func ExecCommand(cmd tpapi.Command, cfg *configs.Config) (map[string]any, error) {
	switch cmd.Command {

	// ── Enterprise: connectivity ──────────────────────────────────────────────

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

	// ── Enterprise: container management ─────────────────────────────────────

	case "restart_reader":
		readerID, _ := cmd.Payload["reader_id"].(string)
		service, _ := cmd.Payload["service"].(string)
		if service == "" {
			service = readerID
		}
		if service == "" {
			return nil, fmt.Errorf("reader_id or service required")
		}
		out, err := docker.RestartService(service, cfg.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("restart failed: %s", out)
		}
		return map[string]any{"restarted": service, "output": out}, nil

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

	case "list_containers":
		out, err := docker.Run("docker", "ps", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
		if err != nil {
			return nil, fmt.Errorf("docker ps failed: %v", err)
		}
		return map[string]any{"containers": out}, nil

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

	// ── Enterprise: config management ────────────────────────────────────────

	case "reload_config", "update_sqlite":
		hashFile := filepath.Join(cfg.WorkDir, ".config_hash")
		os.WriteFile(hashFile, []byte(""), 0644)
		return map[string]any{"message": "local hash cleared — will resync on next cycle"}, nil

	// ── Device management: reboot / shutdown ──────────────────────────────────

	case "restart_qube", "reboot":
		log.Println("[cmd] REBOOT requested — rebooting in 3s")
		go func() {
			time.Sleep(3 * time.Second)
			// scripts/reboot.sh just logs and exits with 99
			runScript(cfg.WorkDir, "scripts/reboot.sh")
			exec.Command("sudo", "/usr/sbin/reboot").Run()
		}()
		return map[string]any{"rebooting": true}, nil

	case "shutdown":
		log.Println("[cmd] SHUTDOWN requested — shutting down in 3s")
		go func() {
			time.Sleep(3 * time.Second)
			runScript(cfg.WorkDir, "scripts/shutdown.sh")
			exec.Command("sudo", "/usr/sbin/shutdown", "-h", "now").Run()
		}()
		return map[string]any{"shutting_down": true}, nil

	// ── Device management: network ────────────────────────────────────────────

	case "reset_ips":
		out, err := runScript(cfg.WorkDir, "scripts/reset_ips.sh")
		if err != nil {
			return nil, fmt.Errorf("reset_ips failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "set_eth":
		// payload: {"interface":"eth0","mode":"auto"} or
		//          {"interface":"eth0","mode":"static","address":"192.168.1.10/24","gateway":"192.168.1.1","dns":"8.8.8.8"}
		iface, _ := cmd.Payload["interface"].(string)
		mode, _ := cmd.Payload["mode"].(string)
		if iface == "" || mode == "" {
			return nil, fmt.Errorf("interface and mode required")
		}
		args := []string{iface, mode}
		if mode == "static" {
			addr, _ := cmd.Payload["address"].(string)
			gw, _ := cmd.Payload["gateway"].(string)
			dns, _ := cmd.Payload["dns"].(string)
			args = append(args, addr, gw, dns)
		}
		out, err := runScript(cfg.WorkDir, append([]string{"scripts/set_eth.sh"}, args...)...)
		if err != nil {
			return nil, fmt.Errorf("set_eth failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "set_wifi":
		// payload: {"interface":"wlan0","mode":"auto","ssid":"MyWifi","password":"secret","key_mgmt":"psk"} or
		//          {"interface":"wlan0","mode":"static","address":"...","gateway":"...","dns":"...","ssid":"...","password":"...","key_mgmt":"psk"}
		iface, _ := cmd.Payload["interface"].(string)
		mode, _ := cmd.Payload["mode"].(string)
		if iface == "" || mode == "" {
			return nil, fmt.Errorf("interface and mode required")
		}
		ssid, _ := cmd.Payload["ssid"].(string)
		password, _ := cmd.Payload["password"].(string)
		keyMgmt, _ := cmd.Payload["key_mgmt"].(string)
		if keyMgmt == "" {
			keyMgmt = "psk"
		}
		var args []string
		if mode == "auto" {
			// set_wifi.sh: <interface> auto <ssid> <passwd> <key-mgmnt>
			args = []string{iface, "auto", ssid, password, keyMgmt}
		} else {
			// set_wifi.sh: <interface> <ipv4/subnet> <gateway> <dns> <ssid> <passwd> <key-mgmnt>
			addr, _ := cmd.Payload["address"].(string)
			gw, _ := cmd.Payload["gateway"].(string)
			dns, _ := cmd.Payload["dns"].(string)
			args = []string{iface, addr, gw, dns, ssid, password, keyMgmt}
		}
		out, err := runScript(cfg.WorkDir, append([]string{"scripts/set_wifi.sh"}, args...)...)
		if err != nil {
			return nil, fmt.Errorf("set_wifi failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "set_firewall":
		// payload: {"rules":"tcp:10.0.0.0/8:1883,tcp:0:8080"}
		// Rules format: <proto>:<net-or-0>:<port-or-0> comma-separated
		rules, _ := cmd.Payload["rules"].(string)
		if rules == "" {
			return nil, fmt.Errorf("rules required (e.g. tcp:10.0.0.0/8:1883)")
		}
		out, err := runScript(cfg.WorkDir, "scripts/set_firewall.sh", rules)
		if err != nil {
			return nil, fmt.Errorf("set_firewall failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	// ── Device management: identity / system ─────────────────────────────────

	case "set_name":
		name, _ := cmd.Payload["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/set_name.sh", name)
		if err != nil {
			return nil, fmt.Errorf("set_name failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "set_timezone":
		tz, _ := cmd.Payload["timezone"].(string)
		if tz == "" {
			return nil, fmt.Errorf("timezone required (e.g. Asia/Colombo)")
		}
		out, err := runScript(cfg.WorkDir, "scripts/set_timezone.sh", tz)
		if err != nil {
			return nil, fmt.Errorf("set_timezone failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "get_info":
		// Runs scripts/get_info.sh which outputs eth/wlan IPs, MACs, SSID, open ports.
		out, err := runScript(cfg.WorkDir, "scripts/get_info.sh")
		if err != nil {
			return nil, fmt.Errorf("get_info failed: %s — %v", out, err)
		}
		// Parse the key: value output into a map
		info := parseKeyValueOutput(out)
		info["raw"] = out
		return info, nil

	// ── Data backup / restore ─────────────────────────────────────────────────

	case "backup_data":
		// payload: {"type":"cifs","path":"\\\\192.168.1.1\\share","user":"u","pass":"p"}
		//       or {"type":"nfs","path":"192.168.1.1:/nfs-share"}
		mountType, _ := cmd.Payload["type"].(string)
		mountPath, _ := cmd.Payload["path"].(string)
		user, _ := cmd.Payload["user"].(string)
		pass, _ := cmd.Payload["pass"].(string)
		if mountType == "" || mountPath == "" {
			return nil, fmt.Errorf("type and path required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/backup_data.sh", mountType, mountPath, user, pass)
		if err != nil {
			return nil, fmt.Errorf("backup_data failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	case "restore_data":
		mountType, _ := cmd.Payload["type"].(string)
		mountPath, _ := cmd.Payload["path"].(string)
		user, _ := cmd.Payload["user"].(string)
		pass, _ := cmd.Payload["pass"].(string)
		if mountType == "" || mountPath == "" {
			return nil, fmt.Errorf("type and path required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/restore_data.sh", mountType, mountPath, user, pass)
		if err != nil {
			return nil, fmt.Errorf("restore_data failed: %s — %v", out, err)
		}
		return map[string]any{"output": out}, nil

	// ── Maintenance mode operations ───────────────────────────────────────────
	// These reboot the device into maintenance mode, run the target script,
	// then reboot back to normal. Device will be temporarily offline.

	case "backup_image":
		out, err := runScript(cfg.WorkDir, "scripts/maintenance_start.sh", "scripts/backup_image.sh")
		if err != nil && !isMaintenanceReboot(err) {
			return nil, fmt.Errorf("backup_image failed: %s — %v", out, err)
		}
		return map[string]any{"message": "Entering maintenance mode for image backup — device rebooting", "output": out}, nil

	case "restore_image":
		out, err := runScript(cfg.WorkDir, "scripts/maintenance_start.sh", "scripts/restore_image.sh")
		if err != nil && !isMaintenanceReboot(err) {
			return nil, fmt.Errorf("restore_image failed: %s — %v", out, err)
		}
		return map[string]any{"message": "Entering maintenance mode for image restore — device rebooting", "output": out}, nil

	case "repair_fs":
		out, err := runScript(cfg.WorkDir, "scripts/maintenance_start.sh", "scripts/repair_fs.sh")
		if err != nil && !isMaintenanceReboot(err) {
			return nil, fmt.Errorf("repair_fs failed: %s — %v", out, err)
		}
		return map[string]any{"message": "Entering maintenance mode for filesystem repair — device rebooting", "output": out}, nil

	// ── Service management (v1 compat) ────────────────────────────────────────

	case "service_add":
		// payload: {"name":"myservice","type":"modbus","version":"1.2.3","ports":"8080,8081"}
		name, _ := cmd.Payload["name"].(string)
		svcType, _ := cmd.Payload["type"].(string)
		version, _ := cmd.Payload["version"].(string)
		ports, _ := cmd.Payload["ports"].(string)
		if name == "" || svcType == "" || version == "" {
			return nil, fmt.Errorf("name, type, and version required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/service_add.sh", name, svcType, version, ports)
		if err != nil {
			return nil, fmt.Errorf("service_add failed: %s — %v", out, err)
		}
		return map[string]any{"output": out, "service": name}, nil

	case "service_rm":
		name, _ := cmd.Payload["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/service_rm.sh", name)
		if err != nil {
			return nil, fmt.Errorf("service_rm failed: %s — %v", out, err)
		}
		return map[string]any{"output": out, "service": name}, nil

	case "service_edit":
		name, _ := cmd.Payload["name"].(string)
		ports, _ := cmd.Payload["ports"].(string)
		if name == "" {
			return nil, fmt.Errorf("name required")
		}
		out, err := runScript(cfg.WorkDir, "scripts/service_edit.sh", name, ports)
		if err != nil {
			return nil, fmt.Errorf("service_edit failed: %s — %v", out, err)
		}
		return map[string]any{"output": out, "service": name}, nil

	// ── File transfer ─────────────────────────────────────────────────────────

	case "put_file":
		// payload: {"path":"/relative/path/file.txt","data":"<base64>"}
		// File is written to /mit<path> (same convention as v1 conf-agent)
		filePath, _ := cmd.Payload["path"].(string)
		dataB64, _ := cmd.Payload["data"].(string)
		if filePath == "" || dataB64 == "" {
			return nil, fmt.Errorf("path and data required")
		}
		if !pathValidate(filePath) {
			return nil, fmt.Errorf("invalid path")
		}
		data, err := base64.StdEncoding.DecodeString(dataB64)
		if err != nil {
			return nil, fmt.Errorf("invalid base64: %v", err)
		}
		destPath := "/mit" + filePath
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil, fmt.Errorf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return nil, fmt.Errorf("write failed: %v", err)
		}
		return map[string]any{"written": destPath, "bytes": len(data)}, nil

	case "get_file":
		// payload: {"path":"/relative/path/file.txt"}
		filePath, _ := cmd.Payload["path"].(string)
		if filePath == "" {
			return nil, fmt.Errorf("path required")
		}
		if !pathValidate(filePath) {
			return nil, fmt.Errorf("invalid path")
		}
		srcPath := "/mit" + filePath
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("read failed: %v", err)
		}
		return map[string]any{
			"path": srcPath,
			"data": base64.StdEncoding.EncodeToString(data),
			"size": len(data),
		}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Command)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// runScript runs a shell script from workDir with the given arguments.
// The script path is the first element, remaining elements are args.
func runScript(workDir string, scriptAndArgs ...string) (string, error) {
	if len(scriptAndArgs) == 0 {
		return "", fmt.Errorf("no script specified")
	}
	args := append([]string{scriptAndArgs[0]}, scriptAndArgs[1:]...)
	cmd := exec.Command("bash", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// isMaintenanceReboot returns true if the error is from a maintenance exit code (99=reboot).
// maintenance_start.sh always exits 99 to trigger a reboot — this is expected.
func isMaintenanceReboot(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 99
	}
	return false
}

// pathValidate rejects paths with traversal or injection characters.
// Matches the v1 conf-agent pathValidate() function.
func pathValidate(txt string) bool {
	return !strings.Contains(txt, "..") &&
		!strings.Contains(txt, "$") &&
		!strings.Contains(txt, "\\") &&
		!strings.Contains(txt, ";")
}

// parseKeyValueOutput parses "key: value" lines (e.g. get_info.sh output) into a map.
func parseKeyValueOutput(output string) map[string]any {
	result := make(map[string]any)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
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
