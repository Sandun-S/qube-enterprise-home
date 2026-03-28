package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/qubes/:id/containers — list all containers for a Qube
func listContainersHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT c.id, c.reader_id, c.name, c.image, c.env_json,
			        c.status, c.version, c.created_at,
			        COALESCE(rd.name, '') AS reader_name,
			        COALESCE(rd.protocol, '') AS protocol
			 FROM containers c
			 LEFT JOIN readers rd ON rd.id = c.reader_id
			 WHERE c.qube_id=$1
			 ORDER BY c.created_at ASC`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, image, status, readerName, protocol string
			var readerID *string
			var envRaw []byte
			var version int
			var createdAt time.Time
			if err := rows.Scan(&id, &readerID, &name, &image, &envRaw,
				&status, &version, &createdAt, &readerName, &protocol); err != nil {
				continue
			}
			var env any
			json.Unmarshal(envRaw, &env)
			result = append(result, map[string]any{
				"id": id, "reader_id": readerID, "name": name,
				"image": image, "env_json": env,
				"status": status, "version": version,
				"reader_name": readerName, "protocol": protocol,
				"created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}
