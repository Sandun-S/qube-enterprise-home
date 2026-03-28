package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// globalDashHub is set during router initialization.
var globalDashHub *DashboardHub

// dashWSHandler handles WebSocket upgrade requests from dashboard clients.
// Auth: JWT token in query param (WebSocket upgrade can't use headers easily).
// Usage: /ws/dashboard?token=<jwt>
func dashWSHandler(pool *pgxpool.Pool, dashHub *DashboardHub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			writeError(w, http.StatusUnauthorized, "token query param required")
			return
		}

		// Validate JWT and extract claims
		claims, err := parseJWT(tokenStr, jwtSecret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		orgID, _ := claims["org_id"].(string)
		userID, _ := claims["user_id"].(string)
		if orgID == "" || userID == "" {
			writeError(w, http.StatusUnauthorized, "invalid token claims")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[dash-ws] upgrade failed: %v", err)
			return
		}

		clientID := generateConnID()
		client := NewDashboardClient(clientID, orgID)
		dashHub.Register(client)

		log.Printf("[dash-ws] connected: %s (user=%s, org=%s)", clientID, userID, orgID)

		go dashWritePump(conn, client)
		go dashReadPump(conn, client, pool, dashHub)
	}
}

// dashReadPump reads subscription control messages from the dashboard client.
// Message format:
//
//	{"action": "subscribe",   "qube_id": "Q-1001"}
//	{"action": "unsubscribe", "qube_id": "Q-1001"}
func dashReadPump(conn *websocket.Conn, client *DashboardClient, pool *pgxpool.Pool, dashHub *DashboardHub) {
	defer func() {
		dashHub.Unregister(client.ID)
		conn.Close()
		log.Printf("[dash-ws] disconnected: %s", client.ID)
	}()

	conn.SetReadLimit(4096)
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
				log.Printf("[dash-ws] read error from %s: %v", client.ID, err)
			}
			return
		}

		var msg struct {
			Action string `json:"action"`
			QubeID string `json:"qube_id"`
		}
		if err := json.Unmarshal(data, &msg); err != nil || msg.QubeID == "" {
			continue
		}

		// Verify the Qube belongs to the client's org
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`,
			msg.QubeID, client.OrgID).Scan(&count)

		if count == 0 {
			errMsg := NewWSMessage("error", msg.QubeID, map[string]any{
				"message": "qube not found or not in your organisation",
			})
			select {
			case client.send <- errMsg:
			default:
			}
			continue
		}

		switch msg.Action {
		case "subscribe":
			dashHub.Subscribe(client.ID, msg.QubeID)
			ack := NewWSMessage("subscribed", msg.QubeID, map[string]any{
				"qube_id": msg.QubeID,
			})
			select {
			case client.send <- ack:
			default:
			}
			log.Printf("[dash-ws] %s subscribed to %s", client.ID, msg.QubeID)

		case "unsubscribe":
			dashHub.Unsubscribe(client.ID, msg.QubeID)
			ack := NewWSMessage("unsubscribed", msg.QubeID, map[string]any{
				"qube_id": msg.QubeID,
			})
			select {
			case client.send <- ack:
			default:
			}
			log.Printf("[dash-ws] %s unsubscribed from %s", client.ID, msg.QubeID)
		}
	}
}

// dashWritePump writes messages to the dashboard WebSocket connection.
func dashWritePump(conn *websocket.Conn, client *DashboardClient) {
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
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := MarshalMessage(msg)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
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

// parseJWT validates a JWT string and returns the claims map.
func parseJWT(tokenStr, secret string) (jwt.MapClaims, error) {
	tok, err := jwt.Parse(tokenStr,
		func(t *jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

// generateConnID creates a short random hex ID for dashboard connections.
func generateConnID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "dash-" + hex.EncodeToString(b)
}
