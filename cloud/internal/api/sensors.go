package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/readers/:reader_id/sensors
func listSensorsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		readerID := chi.URLParam(r, "reader_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT rd.qube_id FROM readers rd JOIN qubes q ON q.id=rd.qube_id
			 WHERE rd.id=$1 AND q.org_id=$2`, readerID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT s.id, s.name, s.template_id, dt.name AS template_name,
			        s.config_json, s.tags_json, s.output, s.table_name,
			        s.status, s.version, s.created_at
			 FROM sensors s
			 LEFT JOIN device_templates dt ON dt.id = s.template_id
			 WHERE s.reader_id=$1
			 ORDER BY s.created_at ASC`, readerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, status, output, tableName string
			var tmplID, tmplName *string
			var version int
			var cfgRaw, tagsRaw []byte
			var createdAt time.Time
			if err := rows.Scan(&id, &name, &tmplID, &tmplName,
				&cfgRaw, &tagsRaw, &output, &tableName,
				&status, &version, &createdAt); err != nil {
				continue
			}
			var cfg, tags any
			json.Unmarshal(cfgRaw, &cfg)
			json.Unmarshal(tagsRaw, &tags)
			result = append(result, map[string]any{
				"id": id, "name": name,
				"template_id": tmplID, "template_name": tmplName,
				"config_json": cfg, "tags_json": tags,
				"output": output, "table_name": tableName,
				"status": status, "version": version,
				"created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// POST /api/v1/readers/:reader_id/sensors
func createSensorHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		readerID := chi.URLParam(r, "reader_id")

		var qubeID, readerProtocol string
		err := pool.QueryRow(context.Background(),
			`SELECT rd.qube_id, rd.protocol
			 FROM readers rd JOIN qubes q ON q.id=rd.qube_id
			 WHERE rd.id=$1 AND q.org_id=$2`, readerID, orgID,
		).Scan(&qubeID, &readerProtocol)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader not found")
			return
		}

		var req struct {
			Name       string `json:"name"`
			TemplateID string `json:"template_id"` // device_template UUID
			Params     any    `json:"params"`       // per-sensor params (unit_id, ip_address, etc.)
			TagsJSON   any    `json:"tags_json"`
			Output     string `json:"output"`       // "influxdb", "live", "influxdb,live"
			TableName  string `json:"table_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if req.Output == "" {
			req.Output = "influxdb"
		}
		if req.TableName == "" {
			req.TableName = "Measurements"
		}

		ctx := context.Background()

		// Build config_json by merging template sensor_config + user params
		configJSON := req.Params
		if req.TemplateID != "" {
			var tmplProtocol string
			var tmplSensorConfig []byte
			err = pool.QueryRow(ctx,
				`SELECT protocol, sensor_config FROM device_templates
				 WHERE id=$1 AND (is_global=TRUE OR org_id=$2)`,
				req.TemplateID, orgID,
			).Scan(&tmplProtocol, &tmplSensorConfig)
			if err != nil {
				writeError(w, http.StatusNotFound, "device template not found")
				return
			}
			if tmplProtocol != readerProtocol {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("template protocol (%s) != reader protocol (%s)",
						tmplProtocol, readerProtocol))
				return
			}

			// Merge: template sensor_config + user params
			configJSON = mergeSensorConfig(tmplSensorConfig, req.Params)
		}

		cfgBytes, _ := json.Marshal(configJSON)
		if cfgBytes == nil {
			cfgBytes = []byte("{}")
		}
		tagsBytes, _ := json.Marshal(req.TagsJSON)
		if tagsBytes == nil {
			tagsBytes = []byte("{}")
		}

		templateIDArg := any(nil)
		if req.TemplateID != "" {
			templateIDArg = req.TemplateID
		}

		var sensorID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO sensors (reader_id, name, template_id, config_json, tags_json, output, table_name)
			 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			readerID, req.Name, templateIDArg, cfgBytes, tagsBytes, req.Output, req.TableName,
		).Scan(&sensorID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create sensor: "+err.Error())
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"sensor_id": sensorID,
			"new_hash":  hash,
			"message":   "Sensor created. Config will sync to Qube SQLite.",
		})
	}
}

// PUT /api/v1/sensors/:sensor_id
func updateSensorHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id FROM sensors s
			 JOIN readers rd ON rd.id = s.reader_id
			 JOIN qubes q ON q.id = rd.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		var req struct {
			Name       *string `json:"name"`
			ConfigJSON any     `json:"config_json"`
			TagsJSON   any     `json:"tags_json"`
			Output     *string `json:"output"`
			TableName  *string `json:"table_name"`
			Status     *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		setParts := []string{}
		args := []any{}
		i := 1
		if req.Name != nil {
			setParts = append(setParts, fmt.Sprintf("name=$%d", i))
			args = append(args, *req.Name)
			i++
		}
		if req.ConfigJSON != nil {
			b, _ := json.Marshal(req.ConfigJSON)
			setParts = append(setParts, fmt.Sprintf("config_json=$%d", i))
			args = append(args, b)
			i++
		}
		if req.TagsJSON != nil {
			b, _ := json.Marshal(req.TagsJSON)
			setParts = append(setParts, fmt.Sprintf("tags_json=$%d", i))
			args = append(args, b)
			i++
		}
		if req.Output != nil {
			setParts = append(setParts, fmt.Sprintf("output=$%d", i))
			args = append(args, *req.Output)
			i++
		}
		if req.TableName != nil {
			setParts = append(setParts, fmt.Sprintf("table_name=$%d", i))
			args = append(args, *req.TableName)
			i++
		}
		if req.Status != nil && (*req.Status == "active" || *req.Status == "disabled") {
			setParts = append(setParts, fmt.Sprintf("status=$%d", i))
			args = append(args, *req.Status)
			i++
		}
		if len(setParts) == 0 {
			writeError(w, http.StatusBadRequest, "nothing to update")
			return
		}

		setParts = append(setParts, "version=version+1, updated_at=NOW()")
		query := fmt.Sprintf("UPDATE sensors SET %s WHERE id=$%d",
			strings.Join(setParts, ", "), i)
		args = append(args, sensorID)
		if _, err := pool.Exec(context.Background(), query, args...); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		hash, _ := recomputeConfigHash(context.Background(), pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "sensor updated",
			"new_hash": hash,
		})
	}
}

// DELETE /api/v1/sensors/:sensor_id
func deleteSensorHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id FROM sensors s
			 JOIN readers rd ON rd.id = s.reader_id
			 JOIN qubes q ON q.id = rd.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM sensors WHERE id=$1`, sensorID); err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "new_hash": hash})
	}
}

// GET /api/v1/qubes/:id/sensors
func listAllSensorsForQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		rows, err := pool.Query(context.Background(),
			`SELECT s.id, s.name, rd.id AS reader_id, rd.name AS reader_name, rd.protocol,
			        COALESCE(dt.name, '') AS template_name, s.output, s.status, s.created_at
			 FROM sensors s
			 JOIN readers rd ON rd.id = s.reader_id
			 LEFT JOIN device_templates dt ON dt.id = s.template_id
			 JOIN qubes q ON q.id = rd.qube_id
			 WHERE rd.qube_id=$1 AND q.org_id=$2
			 ORDER BY rd.created_at ASC, s.created_at ASC`, qubeID, orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var sid, sname, rdid, rdname, proto, tmpl, output, status string
			var createdAt time.Time
			if err := rows.Scan(&sid, &sname, &rdid, &rdname, &proto, &tmpl, &output, &status, &createdAt); err == nil {
				result = append(result, map[string]any{
					"id": sid, "name": sname, "reader_id": rdid,
					"reader_name": rdname, "protocol": proto,
					"template_name": tmpl, "output": output,
					"status": status, "created_at": createdAt,
				})
			}
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// mergeSensorConfig combines template sensor_config with user-provided params.
// Template provides registers/oids/nodes/json_paths, user provides unit_id, ip_address, etc.
func mergeSensorConfig(tmplConfigRaw []byte, userParams any) map[string]any {
	var tmplConfig map[string]any
	json.Unmarshal(tmplConfigRaw, &tmplConfig)
	if tmplConfig == nil {
		tmplConfig = map[string]any{}
	}

	params, _ := userParams.(map[string]any)
	if params == nil {
		return tmplConfig
	}

	// Merge user params into template config
	for k, v := range params {
		tmplConfig[k] = v
	}
	return tmplConfig
}

func flattenTags(tags map[string]any) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ",")
}

func strVal(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func toInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return def
}
