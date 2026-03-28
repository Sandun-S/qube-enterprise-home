package api

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// globalHub is set during router initialization. Handlers use this to push
// WebSocket notifications without needing the hub passed through every function.
var globalHub *WSHub

// globalPool is set during router initialization for config push notifications.
var globalPool *pgxpool.Pool

// NotifyConfigChange pushes a config_push message to a Qube via WebSocket
// if it's connected. If not connected, conf-agent will detect the change
// on its next poll via /v1/sync/state hash comparison.
//
// Call this after recomputeConfigHash() whenever config changes.
func NotifyConfigChange(pool *pgxpool.Pool, hub *WSHub, qubeID, newHash string) {
	if hub == nil {
		return
	}
	if !hub.IsConnected(qubeID) {
		return
	}

	// Get config version
	var configVersion int
	pool.QueryRow(context.Background(),
		`SELECT config_version FROM config_state WHERE qube_id=$1`, qubeID,
	).Scan(&configVersion)

	msg := NewWSMessage("config_push", qubeID, map[string]any{
		"hash":           newHash,
		"config_version": configVersion,
		"action":         "sync_required",
	})

	delivered := hub.SendTo(qubeID, msg)
	logWSDelivery(pool, qubeID, "config_push", map[string]any{"hash": newHash}, delivered)

	if delivered {
		log.Printf("[ws] config_push sent to %s (hash=%s)", qubeID, newHash[:12])
	}
}

// logWSDelivery records a WebSocket message delivery attempt in ws_delivery_log.
func logWSDelivery(pool *pgxpool.Pool, qubeID, msgType string, payload any, delivered bool) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}

	if delivered {
		pool.Exec(context.Background(),
			`INSERT INTO ws_delivery_log (qube_id, message_type, payload, delivered, delivered_at)
			 VALUES ($1, $2, $3, TRUE, NOW())`,
			qubeID, msgType, payloadJSON)
	} else {
		pool.Exec(context.Background(),
			`INSERT INTO ws_delivery_log (qube_id, message_type, payload, delivered)
			 VALUES ($1, $2, $3, FALSE)`,
			qubeID, msgType, payloadJSON)
	}
}
