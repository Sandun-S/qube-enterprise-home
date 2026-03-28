package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ═══════════════════════════════════════════════════════════════════════════════
// WebSocket Handler — bidirectional cloud↔qube communication
// ═══════════════════════════════════════════════════════════════════════════════
//
// Connection flow:
//   1. Qube conf-agent connects to ws://<cloud>:8080/ws
//   2. Sends auth in query params: ?qube_id=Q-1001&token=<hmac_token>
//   3. Server validates HMAC (same as TP-API auth)
//   4. On success: registers client in hub, sets ws_connected=true
//   5. Bidirectional message exchange begins
//
// Message types (cloud → qube):
//   - config_push:  New config available, download via /v1/sync/config
//   - command:      Execute a command (ping, restart_reader, etc.)
//   - ack:          Acknowledge a message from qube
//
// Message types (qube → cloud):
//   - heartbeat:      Qube is alive
//   - command_ack:    Command execution result
//   - live_telemetry: Real-time sensor data (Output="live")
//   - error:          Error report from qube

const (
	// Time allowed to write a message to the peer.
	wsWriteWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	wsPongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	wsPingPeriod = 45 * time.Second

	// Maximum message size allowed from peer.
	wsMaxMessageSize = 64 * 1024 // 64KB
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Qubes connect from any IP
	},
}

// wsHandler handles WebSocket upgrade requests from Qubes.
func wsHandler(pool *pgxpool.Pool, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth from query params (WebSocket upgrade can't use custom headers easily)
		qubeID := r.URL.Query().Get("qube_id")
		token := r.URL.Query().Get("token")

		if qubeID == "" || token == "" {
			writeError(w, http.StatusUnauthorized, "qube_id and token query params required")
			return
		}

		// Validate HMAC token (same logic as TP-API)
		var orgSecret, orgID string
		err := pool.QueryRow(context.Background(),
			`SELECT o.org_secret, o.id
			 FROM qubes q JOIN organisations o ON o.id = q.org_id
			 WHERE q.id=$1 AND q.org_id IS NOT NULL`, qubeID,
		).Scan(&orgSecret, &orgID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "qube not claimed or not registered")
			return
		}

		expected := wsComputeHMAC(qubeID, orgSecret)
		if !hmac.Equal([]byte(expected), []byte(token)) {
			writeError(w, http.StatusUnauthorized, "invalid qube token")
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[ws] upgrade failed for %s: %v", qubeID, err)
			return
		}

		log.Printf("[ws] connected: %s", qubeID)

		// Create client and register with hub
		client := NewWSClient(qubeID, orgID, hub)
		hub.Register(client)

		// Update qubes table
		pool.Exec(context.Background(),
			`UPDATE qubes SET ws_connected=TRUE, last_seen=NOW(), status='online' WHERE id=$1`,
			qubeID)

		// Start read/write pumps
		go wsWritePump(conn, client)
		go wsReadPump(conn, client, pool)
	}
}

// wsReadPump reads messages from the WebSocket connection.
// Runs in its own goroutine per client.
func wsReadPump(conn *websocket.Conn, client *WSClient, pool *pgxpool.Pool) {
	defer func() {
		client.Hub.Unregister(client.QubeID)
		conn.Close()
		// Mark disconnected
		pool.Exec(context.Background(),
			`UPDATE qubes SET ws_connected=FALSE WHERE id=$1`, client.QubeID)
		log.Printf("[ws] disconnected: %s", client.QubeID)
	}()

	conn.SetReadLimit(wsMaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] read error from %s: %v", client.QubeID, err)
			}
			return
		}

		msg, err := UnmarshalMessage(data)
		if err != nil {
			log.Printf("[ws] invalid message from %s: %v", client.QubeID, err)
			continue
		}
		msg.QubeID = client.QubeID // Enforce sender identity

		handleIncomingMessage(pool, client, msg)
	}
}

// wsWritePump writes messages to the WebSocket connection.
// Runs in its own goroutine per client.
func wsWritePump(conn *websocket.Conn, client *WSClient) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case msg, ok := <-client.send:
			conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				// Hub closed the channel
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := MarshalMessage(msg)
			if err != nil {
				log.Printf("[ws] marshal error for %s: %v", client.QubeID, err)
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("[ws] write error to %s: %v", client.QubeID, err)
				return
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-client.done:
			return
		}
	}
}

// handleIncomingMessage processes messages received from a Qube.
func handleIncomingMessage(pool *pgxpool.Pool, client *WSClient, msg WSMessage) {
	ctx := context.Background()

	switch msg.Type {

	case "heartbeat":
		// Update last_seen
		pool.Exec(ctx,
			`UPDATE qubes SET last_seen=NOW(), status='online' WHERE id=$1`,
			client.QubeID)

		// Respond with server time
		client.send <- NewWSMessage("heartbeat_ack", client.QubeID, map[string]any{
			"server_time": time.Now().UTC(),
		})

	case "command_ack":
		// Qube reporting command execution result
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		cmdID, _ := payload["command_id"].(string)
		status, _ := payload["status"].(string)
		if cmdID == "" || (status != "executed" && status != "failed") {
			return
		}

		resultJSON, _ := json.Marshal(payload["result"])
		pool.Exec(ctx,
			`UPDATE qube_commands SET status=$1, result=$2, executed_at=NOW()
			 WHERE id=$3 AND qube_id=$4 AND status IN ('pending', 'sent')`,
			status, resultJSON, cmdID, client.QubeID)

	case "live_telemetry":
		// Real-time sensor data from qube (Output="live")
		// Relay to subscribed dashboard WebSocket clients
		if globalDashHub != nil {
			relayMsg := NewWSMessage("live_telemetry", client.QubeID, msg.Payload)
			n := globalDashHub.RelayToSubscribers(client.OrgID, client.QubeID, relayMsg)
			if n > 0 {
				log.Printf("[ws] relayed live telemetry from %s to %d dashboard clients", client.QubeID, n)
			}
		}

	case "config_ack":
		// Qube confirming it received and applied config
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			return
		}
		hash, _ := payload["hash"].(string)
		if hash != "" {
			pool.Exec(ctx,
				`UPDATE qubes SET config_version=config_version+1 WHERE id=$1`,
				client.QubeID)
			log.Printf("[ws] config ack from %s, hash=%s", client.QubeID, hash)
		}

	case "error":
		// Error report from qube
		log.Printf("[ws] error from %s: %v", client.QubeID, msg.Payload)

	default:
		log.Printf("[ws] unknown message type from %s: %s", client.QubeID, msg.Type)
	}
}

// ─── Helper ──────────────────────────────────────────────────────────────────

func wsComputeHMAC(qubeID, orgSecret string) string {
	mac := hmac.New(sha256.New, []byte(orgSecret))
	mac.Write([]byte(qubeID + ":" + orgSecret))
	return hex.EncodeToString(mac.Sum(nil))
}
