package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/qubes/:id/telemetry-settings
// Lists all InfluxDB → sensor_id mappings for a Qube.
// These are synced to SQLite by conf-agent and read by enterprise-influx-to-sql.
func listTelemetrySettingsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		// Verify Qube belongs to org
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT ts.id, ts.device, ts.reading, ts.agg_time_min, ts.agg_func,
			        ts.sensor_id, s.name AS sensor_name, ts.tag_names, ts.updated_at
			 FROM telemetry_settings ts
			 LEFT JOIN sensors s ON s.id = ts.sensor_id
			 WHERE ts.qube_id=$1
			 ORDER BY ts.device ASC, ts.reading ASC`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, device, reading, aggFunc string
			var aggTime int
			var sensorID *string
			var sensorName *string
			var tagNames *string
			var updatedAt any
			if err := rows.Scan(&id, &device, &reading, &aggTime, &aggFunc,
				&sensorID, &sensorName, &tagNames, &updatedAt); err != nil {
				continue
			}
			result = append(result, map[string]any{
				"id":           id,
				"device":       device,
				"reading":      reading,
				"agg_time_min": aggTime,
				"agg_func":     aggFunc,
				"sensor_id":    sensorID,
				"sensor_name":  sensorName,
				"tag_names":    tagNames,
				"updated_at":   updatedAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// POST /api/v1/qubes/:id/telemetry-settings
// Creates a new InfluxDB device+reading → sensor mapping.
//
// Required: device, sensor_id
// Optional: reading (default "*" = all fields), agg_time_min, agg_func, tag_names
func createTelemetrySettingHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		// Verify Qube belongs to org
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		var req struct {
			Device     string `json:"device"`
			Reading    string `json:"reading"`
			AggTimeMin int    `json:"agg_time_min"`
			AggFunc    string `json:"agg_func"`
			SensorID   string `json:"sensor_id"`
			TagNames   any    `json:"tag_names"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.Device == "" {
			writeError(w, http.StatusBadRequest, "device is required")
			return
		}
		if req.SensorID == "" {
			writeError(w, http.StatusBadRequest, "sensor_id is required")
			return
		}
		if req.Reading == "" {
			req.Reading = "*"
		}
		if req.AggTimeMin <= 0 {
			req.AggTimeMin = 1
		}
		if req.AggFunc == "" {
			req.AggFunc = "LAST"
		}

		ctx := context.Background()

		// Verify sensor belongs to this Qube (via reader)
		var sensorCount int
		pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sensors s
			 JOIN readers rd ON rd.id = s.reader_id
			 WHERE s.id=$1 AND rd.qube_id=$2`, req.SensorID, qubeID).Scan(&sensorCount)
		if sensorCount == 0 {
			writeError(w, http.StatusBadRequest, "sensor_id does not belong to this qube")
			return
		}

		tagNamesJSON, _ := json.Marshal(req.TagNames)
		if tagNamesJSON == nil || string(tagNamesJSON) == "null" {
			tagNamesJSON = []byte("[]")
		}

		var tsID string
		err := pool.QueryRow(ctx,
			`INSERT INTO telemetry_settings (qube_id, device, reading, agg_time_min, agg_func, sensor_id, tag_names)
			 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			qubeID, req.Device, req.Reading, req.AggTimeMin, req.AggFunc, req.SensorID, string(tagNamesJSON),
		).Scan(&tsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create telemetry setting: "+err.Error())
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":       tsID,
			"new_hash": hash,
			"message":  "Telemetry mapping created. Will sync to Qube on next config pull.",
		})
	}
}

// PUT /api/v1/qubes/:id/telemetry-settings/:ts_id
// Updates device, reading, agg_time_min, agg_func, sensor_id, or tag_names.
func updateTelemetrySettingHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")
		tsID := chi.URLParam(r, "ts_id")

		// Verify ownership
		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM telemetry_settings ts
			 JOIN qubes q ON q.id = ts.qube_id
			 WHERE ts.id=$1 AND ts.qube_id=$2 AND q.org_id=$3`, tsID, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "telemetry setting not found")
			return
		}

		var req struct {
			Device     *string `json:"device"`
			Reading    *string `json:"reading"`
			AggTimeMin *int    `json:"agg_time_min"`
			AggFunc    *string `json:"agg_func"`
			SensorID   *string `json:"sensor_id"`
			TagNames   any     `json:"tag_names"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		ctx := context.Background()

		// If sensor_id is being changed, verify it belongs to this Qube
		if req.SensorID != nil {
			var sensorCount int
			pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM sensors s
				 JOIN readers rd ON rd.id = s.reader_id
				 WHERE s.id=$1 AND rd.qube_id=$2`, *req.SensorID, qubeID).Scan(&sensorCount)
			if sensorCount == 0 {
				writeError(w, http.StatusBadRequest, "sensor_id does not belong to this qube")
				return
			}
		}

		_, err := pool.Exec(ctx,
			`UPDATE telemetry_settings SET
			   device       = COALESCE($1, device),
			   reading      = COALESCE($2, reading),
			   agg_time_min = COALESCE($3, agg_time_min),
			   agg_func     = COALESCE($4, agg_func),
			   sensor_id    = COALESCE($5, sensor_id),
			   tag_names    = COALESCE($6, tag_names),
			   updated_at   = NOW()
			 WHERE id=$7`,
			req.Device, req.Reading, req.AggTimeMin, req.AggFunc, req.SensorID,
			tagNamesArg(req.TagNames), tsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed: "+err.Error())
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "telemetry setting updated",
			"new_hash": hash,
		})
	}
}

// DELETE /api/v1/qubes/:id/telemetry-settings/:ts_id
func deleteTelemetrySettingHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")
		tsID := chi.URLParam(r, "ts_id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM telemetry_settings ts
			 JOIN qubes q ON q.id = ts.qube_id
			 WHERE ts.id=$1 AND ts.qube_id=$2 AND q.org_id=$3`, tsID, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "telemetry setting not found")
			return
		}

		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM telemetry_settings WHERE id=$1`, tsID); err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":  true,
			"new_hash": hash,
		})
	}
}

// autoCreateTelemetryMapping inserts a default telemetry_settings row when a sensor is created.
// Uses the reader name as the InfluxDB device tag (readers write device=<reader.name>),
// reading="*" to match all fields from that device, and LAST aggregation.
//
// If a mapping for this reader+sensor already exists it is skipped (no error).
// This is called automatically from createSensorHandler and smartCreateSensorHandler
// so users never have to touch the telemetry-settings API for the normal flow.
func autoCreateTelemetryMapping(ctx context.Context, pool *pgxpool.Pool, qubeID, readerID, sensorID, sensorName string) {
	// device must match what readers write as Equipment in line protocol (sensor.Name),
	// not the reader name — readers use the sensor name, not the reader name, as Equipment.
	// Skip if an identical mapping already exists (e.g. sensor re-added to same reader)
	var existing int
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM telemetry_settings
		 WHERE qube_id=$1 AND device=$2 AND reading='*' AND sensor_id=$3`,
		qubeID, sensorName, sensorID).Scan(&existing)
	if existing > 0 {
		return
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO telemetry_settings (qube_id, device, reading, agg_time_min, agg_func, sensor_id, tag_names)
		 VALUES ($1, $2, '*', 1, 'LAST', $3, '[]')`,
		qubeID, sensorName, sensorID)
	if err != nil {
		log.Printf("[telemetry_settings] auto-mapping insert failed: %v", err)
	}
}

// tagNamesArg marshals tag_names to a JSON string for SQL COALESCE, or returns nil if input is nil.
func tagNamesArg(v any) *string {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}
