package tpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pollCommandsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)

		rows, err := pool.Query(context.Background(),
			`SELECT id, command, payload FROM qube_commands
			 WHERE qube_id=$1 AND status IN ('pending', 'sent')
			 ORDER BY created_at ASC LIMIT 10`,
			qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type cmd struct {
			ID      string `json:"id"`
			Command string `json:"command"`
			Payload any    `json:"payload"`
		}
		cmds := make([]cmd, 0)
		var ids []string
		for rows.Next() {
			var c cmd
			var raw []byte
			if err := rows.Scan(&c.ID, &c.Command, &raw); err != nil {
				continue
			}
			json.Unmarshal(raw, &c.Payload)
			cmds = append(cmds, c)
			ids = append(ids, c.ID)
		}

		// Mark polled commands as "sent"
		for _, id := range ids {
			pool.Exec(context.Background(),
				`UPDATE qube_commands SET status='sent', sent_at=NOW()
				 WHERE id=$1 AND status='pending'`, id)
		}

		// Timeout old commands
		pool.Exec(context.Background(),
			`UPDATE qube_commands SET status='timeout'
			 WHERE qube_id=$1 AND status IN ('pending', 'sent')
			 AND created_at < NOW() - INTERVAL '5 minutes'`, qubeID)

		writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
	}
}

func ackCommandHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		cmdID := chi.URLParam(r, "id")

		var req struct {
			Status string `json:"status"`
			Result any    `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.Status == "" {
			req.Status = "executed"
		}
		if req.Status != "executed" && req.Status != "failed" {
			writeError(w, http.StatusBadRequest, "status must be executed or failed")
			return
		}

		result, _ := json.Marshal(req.Result)
		tag, err := pool.Exec(context.Background(),
			`UPDATE qube_commands SET status=$1, result=$2, executed_at=$3
			 WHERE id=$4 AND qube_id=$5 AND status IN ('pending', 'sent')`,
			req.Status, result, time.Now(), cmdID, qubeID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "command not found or already acknowledged")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true, "status": req.Status})
	}
}
