package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/protocols — list all active protocols
// Public endpoint — no auth required
// Returns protocols from DB so adding a new container = just INSERT into protocols table
func listProtocolsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(context.Background(),
			`SELECT id, label, image_name, default_port, description,
			        connection_params_schema, addr_params_schema
			 FROM protocols WHERE is_active = TRUE
			 ORDER BY label`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type protocol struct {
			ID                   string `json:"id"`
			Label                string `json:"label"`
			ImageName            string `json:"image_name"`
			DefaultPort          int    `json:"default_port"`
			Description          string `json:"description"`
			ConnectionSchema     any    `json:"connection_params_schema"`
			AddrParamsSchema     any    `json:"addr_params_schema"`
		}
		protocols := []protocol{}
		for rows.Next() {
			var p protocol
			var connSchema, addrSchema []byte
			if err := rows.Scan(&p.ID, &p.Label, &p.ImageName, &p.DefaultPort,
				&p.Description, &connSchema, &addrSchema); err != nil {
				continue
			}
			json.Unmarshal(connSchema, &p.ConnectionSchema)
			json.Unmarshal(addrSchema, &p.AddrParamsSchema)
			protocols = append(protocols, p)
		}
		writeJSON(w, http.StatusOK, protocols)
	}
}
