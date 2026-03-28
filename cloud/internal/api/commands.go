package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var validCommands = map[string]bool{
	"ping":             true,
	"restart_qube":     true,
	"restart_reader":   true,
	"stop_container":   true,
	"reload_config":    true,
	"get_logs":         true,
	"list_containers":  true,
	"update_sqlite":    true,
}

func sendCommandHandler(pool *pgxpool.Pool) http.HandlerFunc {
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
		// Verify qube belongs to this org
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

		writeJSON(w, http.StatusAccepted, map[string]any{
			"command_id": cmdID,
			"status":     "pending",
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
