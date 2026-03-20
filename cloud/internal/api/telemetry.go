package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/data/readings?sensor_id=uuid&field=field_key&from=iso&to=iso
func readingsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := r.URL.Query().Get("sensor_id")
		fieldKey := r.URL.Query().Get("field")
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")

		if sensorID == "" {
			writeError(w, http.StatusBadRequest, "sensor_id is required")
			return
		}

		// Verify sensor belongs to org
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		from := time.Now().Add(-24 * time.Hour)
		to := time.Now()
		if fromStr != "" {
			if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
				from = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse(time.RFC3339, toStr); err == nil {
				to = t
			}
		}

		query := `SELECT time, field_key, value, unit FROM sensor_readings
		          WHERE sensor_id=$1 AND time BETWEEN $2 AND $3`
		args := []any{sensorID, from, to}
		if fieldKey != "" {
			query += " AND field_key=$4"
			args = append(args, fieldKey)
		}
		query += " ORDER BY time ASC LIMIT 10000"

		rows, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type reading struct {
			Time     time.Time `json:"time"`
			FieldKey string    `json:"field_key"`
			Value    float64   `json:"value"`
			Unit     string    `json:"unit"`
		}
		result := make([]reading, 0)
		for rows.Next() {
			var rd reading
			if err := rows.Scan(&rd.Time, &rd.FieldKey, &rd.Value, &rd.Unit); err == nil {
				result = append(result, rd)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sensor_id": sensorID,
			"from":      from,
			"to":        to,
			"count":     len(result),
			"readings":  result,
		})
	}
}

// GET /api/v1/data/sensors/:id/latest
func latestReadingHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "id")

		// Verify ownership
		var sensorName string
		err := pool.QueryRow(context.Background(),
			`SELECT s.name FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&sensorName)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT DISTINCT ON (field_key) time, field_key, value, unit
			 FROM sensor_readings
			 WHERE sensor_id=$1
			 ORDER BY field_key, time DESC`, sensorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		fields := make([]map[string]any, 0)
		for rows.Next() {
			var t time.Time
			var fk, unit string
			var val float64
			if err := rows.Scan(&t, &fk, &val, &unit); err == nil {
				fields = append(fields, map[string]any{
					"field_key": fk, "value": val, "unit": unit, "time": t,
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sensor_id":   sensorID,
			"sensor_name": sensorName,
			"fields":      fields,
		})
	}
}

// GET /api/v1/qubes/:id/sensors — all sensors across all gateways for a qube
func listAllSensorsForQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		rows, err := pool.Query(context.Background(),
			`SELECT s.id, s.name, g.id AS gw_id, g.name AS gw_name, g.protocol,
			        t.name AS template_name, s.status, s.created_at
			 FROM sensors s
			 JOIN gateways g ON g.id = s.gateway_id
			 JOIN sensor_templates t ON t.id = s.template_id
			 JOIN qubes q ON q.id = g.qube_id
			 WHERE g.qube_id=$1 AND q.org_id=$2
			 ORDER BY g.created_at ASC, s.created_at ASC`, qubeID, orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var sid, sname, gwid, gwname, proto, tmpl, status string
			var createdAt time.Time
			if err := rows.Scan(&sid, &sname, &gwid, &gwname, &proto, &tmpl, &status, &createdAt); err == nil {
				result = append(result, map[string]any{
					"id": sid, "name": sname, "gateway_id": gwid,
					"gateway_name": gwname, "protocol": proto,
					"template_name": tmpl, "status": status, "created_at": createdAt,
				})
			}
		}
		writeJSON(w, http.StatusOK, result)
	}
}
