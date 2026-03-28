package tpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// POST /v1/telemetry/ingest
// Called by influx-to-sql with batches of sensor readings.
// v2: Writes to qubedata (telemetry database with TimescaleDB).
// Accepts SenML (RFC 8428) format from influx-to-sql.
func telemetryIngestHandler(telemetryPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)

		var req struct {
			Readings []struct {
				Time     time.Time `json:"time"`
				SensorID string    `json:"sensor_id"`
				FieldKey string    `json:"field_key"`
				Value    float64   `json:"value"`
				Unit     string    `json:"unit"`
				Tags     any       `json:"tags"`
			} `json:"readings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if len(req.Readings) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"inserted": 0})
			return
		}
		if len(req.Readings) > 5000 {
			writeError(w, http.StatusBadRequest, "batch too large — max 5000 readings per request")
			return
		}

		ctx := context.Background()

		batch := &pgx.Batch{}
		for _, rd := range req.Readings {
			t := rd.Time
			if t.IsZero() {
				t = time.Now().UTC()
			}
			tagsJSON, _ := json.Marshal(rd.Tags)
			if tagsJSON == nil {
				tagsJSON = []byte("{}")
			}
			batch.Queue(
				`INSERT INTO sensor_readings (time, qube_id, sensor_id, field_key, value, unit, tags)
				 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				t, qubeID, rd.SensorID, rd.FieldKey, rd.Value, rd.Unit, tagsJSON,
			)
		}

		results := telemetryPool.SendBatch(ctx, batch)
		defer results.Close()

		inserted := 0
		failed := 0
		for i := 0; i < len(req.Readings); i++ {
			if _, err := results.Exec(); err != nil {
				failed++
			} else {
				inserted++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"inserted": inserted,
			"failed":   failed,
			"total":    len(req.Readings),
		})
	}
}
