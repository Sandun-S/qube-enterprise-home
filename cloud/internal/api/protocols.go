package api

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/protocols — list all active protocols
func listProtocolsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(context.Background(),
			`SELECT id, label, description, reader_standard
			 FROM protocols WHERE is_active = TRUE
			 ORDER BY label`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type protocol struct {
			ID             string `json:"id"`
			Label          string `json:"label"`
			Description    string `json:"description"`
			ReaderStandard string `json:"reader_standard"` // "endpoint" or "multi_target"
		}
		protocols := []protocol{}
		for rows.Next() {
			var p protocol
			if err := rows.Scan(&p.ID, &p.Label, &p.Description, &p.ReaderStandard); err != nil {
				continue
			}
			protocols = append(protocols, p)
		}
		writeJSON(w, http.StatusOK, protocols)
	}
}
