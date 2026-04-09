package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type protocolRow struct {
	ID                      string          `json:"id"`
	Label                   string          `json:"label"`
	Description             string          `json:"description"`
	ReaderStandard          string          `json:"reader_standard"`
	IsActive                bool            `json:"is_active"`
	Icon                    string          `json:"icon"`
	SensorConfigKey         string          `json:"sensor_config_key"`
	MeasurementFieldsSchema json.RawMessage `json:"measurement_fields_schema"`
	DefaultParamsSchema     json.RawMessage `json:"default_params_schema"`
}

const protocolSelectCols = `id, label, description, reader_standard, is_active,
	icon, sensor_config_key, measurement_fields_schema, default_params_schema`

func scanProtocol(rows interface{ Scan(...any) error }, p *protocolRow) error {
	return rows.Scan(&p.ID, &p.Label, &p.Description, &p.ReaderStandard, &p.IsActive,
		&p.Icon, &p.SensorConfigKey, &p.MeasurementFieldsSchema, &p.DefaultParamsSchema)
}

// GET /api/v1/protocols — list all active protocols
func listProtocolsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(context.Background(),
			`SELECT `+protocolSelectCols+` FROM protocols WHERE is_active = TRUE ORDER BY label`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		protocols := []protocolRow{}
		for rows.Next() {
			var p protocolRow
			if err := scanProtocol(rows, &p); err != nil {
				continue
			}
			protocols = append(protocols, p)
		}
		writeJSON(w, http.StatusOK, protocols)
	}
}

// GET /api/v1/admin/protocols — list ALL protocols including inactive (superadmin)
func listAllProtocolsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(context.Background(),
			`SELECT `+protocolSelectCols+` FROM protocols ORDER BY label`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		protocols := []protocolRow{}
		for rows.Next() {
			var p protocolRow
			if err := scanProtocol(rows, &p); err != nil {
				continue
			}
			protocols = append(protocols, p)
		}
		writeJSON(w, http.StatusOK, protocols)
	}
}

// POST /api/v1/admin/protocols — create a new protocol (superadmin)
func createProtocolHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID             string `json:"id"`
			Label          string `json:"label"`
			Description    string `json:"description"`
			ReaderStandard string `json:"reader_standard"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		body.ID = strings.TrimSpace(body.ID)
		body.Label = strings.TrimSpace(body.Label)
		if body.ID == "" || body.Label == "" {
			writeError(w, http.StatusBadRequest, "id and label are required")
			return
		}
		if body.ReaderStandard != "endpoint" && body.ReaderStandard != "multi_target" {
			body.ReaderStandard = "endpoint"
		}

		var p protocolRow
		err := pool.QueryRow(context.Background(),
			`INSERT INTO protocols (id, label, description, reader_standard, is_active)
			 VALUES ($1, $2, $3, $4, TRUE)
			 RETURNING `+protocolSelectCols,
			body.ID, body.Label, body.Description, body.ReaderStandard,
		).Scan(&p.ID, &p.Label, &p.Description, &p.ReaderStandard, &p.IsActive,
			&p.Icon, &p.SensorConfigKey, &p.MeasurementFieldsSchema, &p.DefaultParamsSchema)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				writeError(w, http.StatusConflict, "protocol id already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	}
}

// PUT /api/v1/admin/protocols/{id} — update protocol label/description/standard/active (superadmin)
func updateProtocolHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var body struct {
			Label          string `json:"label"`
			Description    string `json:"description"`
			ReaderStandard string `json:"reader_standard"`
			IsActive       bool   `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if strings.TrimSpace(body.Label) == "" {
			writeError(w, http.StatusBadRequest, "label is required")
			return
		}
		if body.ReaderStandard != "endpoint" && body.ReaderStandard != "multi_target" {
			body.ReaderStandard = "endpoint"
		}

		var p protocolRow
		err := pool.QueryRow(context.Background(),
			`UPDATE protocols SET label=$2, description=$3, reader_standard=$4, is_active=$5
			 WHERE id=$1
			 RETURNING `+protocolSelectCols,
			id, body.Label, body.Description, body.ReaderStandard, body.IsActive,
		).Scan(&p.ID, &p.Label, &p.Description, &p.ReaderStandard, &p.IsActive,
			&p.Icon, &p.SensorConfigKey, &p.MeasurementFieldsSchema, &p.DefaultParamsSchema)
		if err != nil {
			writeError(w, http.StatusNotFound, "protocol not found")
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// DELETE /api/v1/admin/protocols/{id} — delete protocol (superadmin)
func deleteProtocolHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		// Check if any readers use this protocol
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM readers WHERE protocol=$1`, id).Scan(&count)
		if count > 0 {
			writeError(w, http.StatusConflict, "protocol is in use by existing readers — remove all readers first")
			return
		}

		tag, err := pool.Exec(context.Background(), `DELETE FROM protocols WHERE id=$1`, id)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "protocol not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "protocol deleted"})
	}
}
