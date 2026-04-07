package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// validCommands is the set of commands the cloud can dispatch to a Qube.
// Grouped into:
//   - Enterprise: cloud config sync and container management
//   - Device management: network, identity, system controls (from v1 conf-agent-master)
//   - Maintenance: operations requiring a maintenance-mode reboot
//   - Service management: v1-style Docker service add/remove/edit
//   - File transfer: push/pull files to/from the device
//
// All commands arrive at the agent via WebSocket "command" message (primary)
// or TP-API /v1/commands/poll (fallback). The agent runs scripts from its
// scripts/ directory for device management commands.
var validCommands = map[string]bool{
	// ── Enterprise: connectivity + containers ─────────────────────────────────
	"ping":            true, // ping a host; payload: {"target":"8.8.8.8"}
	"restart_qube":    true, // reboot the device
	"restart_reader":  true, // restart a reader container; payload: {"reader_id":"..."}
	"stop_container":  true, // stop any container; payload: {"service":"..."}
	"reload_config":   true, // clear local hash → force resync on next cycle
	"update_sqlite":   true, // alias for reload_config
	"get_logs":        true, // get container/service logs; payload: {"service":"...","lines":100}
	"list_containers": true, // list running containers

	// ── Device management: system ─────────────────────────────────────────────
	"reboot":   true, // reboot the device (alias for restart_qube for v1 compat)
	"shutdown": true, // shut down the device
	"get_info": true, // get network info (IPs, MACs, SSID, open ports)

	// ── Device management: network ────────────────────────────────────────────
	"reset_ips":    true, // reset all IPs to DHCP defaults
	"set_eth":      true, // configure ethernet; payload: {"interface":"eth0","mode":"auto"|"static",...}
	"set_wifi":     true, // configure WiFi;     payload: {"interface":"wlan0","mode":"auto"|"static","ssid":"...","password":"...","key_mgmt":"psk"}
	"set_firewall": true, // set iptables rules;  payload: {"rules":"tcp:10.0.0.0/8:1883,tcp:0:8080"}

	// ── Device management: identity ───────────────────────────────────────────
	"set_name":     true, // set device hostname; payload: {"name":"my-qube"}
	"set_timezone": true, // set timezone;        payload: {"timezone":"Asia/Colombo"}

	// ── Data backup / restore ─────────────────────────────────────────────────
	"backup_data":  true, // backup /data to CIFS/NFS; payload: {"type":"cifs","path":"...","user":"...","pass":"..."}
	"restore_data": true, // restore /data from CIFS/NFS (same payload as backup_data)

	// ── Maintenance mode operations (device reboots, performs op, reboots back) ─
	"backup_image":  true, // dd image backup  (enters maintenance mode)
	"restore_image": true, // dd image restore (enters maintenance mode)
	"repair_fs":     true, // e2fsck repair    (enters maintenance mode)

	// ── Service management (v1 compat) ───────────────────────────────────────
	"service_add":  true, // add Docker service from package;   payload: {"name":"...","type":"...","version":"...","ports":"8080"}
	"service_rm":   true, // remove Docker service;             payload: {"name":"..."}
	"service_edit": true, // edit service config/ports;         payload: {"name":"...","ports":"8080,8081"}

	// ── File transfer ─────────────────────────────────────────────────────────
	"put_file": true, // push file to device; payload: {"path":"/relative/path","data":"<base64>"}
	"get_file": true, // pull file from device; payload: {"path":"/relative/path"}
}

// POST /api/v1/qubes/:id/commands
// Tries WebSocket delivery first; falls back to DB queue for polling.
func sendCommandHandler(pool *pgxpool.Pool, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var req struct {
			Command string         `json:"command"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
			writeError(w, http.StatusBadRequest, "command is required")
			return
		}
		if !validCommands[req.Command] {
			writeError(w, http.StatusBadRequest, "unknown command")
			return
		}

		ctx := context.Background()
		var count int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		payload, _ := json.Marshal(req.Payload)
		var cmdID string
		err := pool.QueryRow(ctx,
			`INSERT INTO qube_commands (qube_id, command, payload) VALUES ($1,$2,$3) RETURNING id`,
			qubeID, req.Command, payload,
		).Scan(&cmdID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to queue command")
			return
		}

		// Try WebSocket delivery first
		delivery := "queued"
		if hub.IsConnected(qubeID) {
			msg := NewWSMessage("command", qubeID, map[string]any{
				"command_id": cmdID,
				"command":    req.Command,
				"payload":    req.Payload,
			})
			msg.ID = cmdID
			delivered := hub.SendTo(qubeID, msg)
			logWSDelivery(pool, qubeID, "command", map[string]any{
				"command_id": cmdID, "command": req.Command,
			}, delivered)
			if delivered {
				delivery = "websocket"
				pool.Exec(ctx,
					`UPDATE qube_commands SET status='sent', sent_at=NOW() WHERE id=$1`, cmdID)
			}
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"command_id": cmdID,
			"status":     "pending",
			"delivery":   delivery,
			"poll_url":   "/api/v1/commands/" + cmdID,
		})
	}
}

func getCommandHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		cmdID := chi.URLParam(r, "id")

		var (
			id, qubeID, command, status string
			result                      []byte
			createdAt                   time.Time
			executedAt                  *time.Time
		)
		err := pool.QueryRow(context.Background(),
			`SELECT c.id, c.qube_id, c.command, c.status, c.result, c.created_at, c.executed_at
			 FROM qube_commands c
			 JOIN qubes q ON q.id = c.qube_id
			 WHERE c.id=$1 AND q.org_id=$2`,
			cmdID, orgID,
		).Scan(&id, &qubeID, &command, &status, &result, &createdAt, &executedAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}

		var parsedResult any
		if result != nil {
			json.Unmarshal(result, &parsedResult)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":          id,
			"qube_id":     qubeID,
			"command":     command,
			"status":      status,
			"result":      parsedResult,
			"created_at":  createdAt,
			"executed_at": executedAt,
		})
	}
}
