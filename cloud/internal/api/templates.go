package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ═══════════════════════════════════════════════════════════════════════════════
// DEVICE TEMPLATES — what data to collect (registers, OIDs, nodes, json_paths)
// ═══════════════════════════════════════════════════════════════════════════════

// GET /api/v1/device-templates
func listDeviceTemplatesHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		protocol := r.URL.Query().Get("protocol")

		query := `SELECT id, org_id, protocol, name, manufacturer, model, description,
		                 sensor_config, sensor_params_schema, reader_template_id,
		                 is_global, version, created_at
		          FROM device_templates
		          WHERE (is_global = TRUE OR org_id = $1)`
		args := []any{orgID}

		if protocol != "" {
			query += " AND protocol = $2"
			args = append(args, protocol)
		}
		query += " ORDER BY is_global DESC, protocol ASC, name ASC"

		rows, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, protocol, name, manufacturer, model, description string
			var orgIDPtr *string
			var sensorCfgRaw, paramsSchemaRaw []byte
			var isGlobal bool
			var version int
			var createdAt time.Time
			var readerTemplateIDPtr *string
			if err := rows.Scan(&id, &orgIDPtr, &protocol, &name, &manufacturer, &model,
				&description, &sensorCfgRaw, &paramsSchemaRaw, &readerTemplateIDPtr,
				&isGlobal, &version, &createdAt); err != nil {
				continue
			}
			var sensorCfg, paramsSchema any
			json.Unmarshal(sensorCfgRaw, &sensorCfg)
			json.Unmarshal(paramsSchemaRaw, &paramsSchema)
			result = append(result, map[string]any{
				"id": id, "org_id": orgIDPtr, "protocol": protocol,
				"name": name, "manufacturer": manufacturer, "model": model,
				"description": description, "reader_template_id": readerTemplateIDPtr,
				"sensor_config": sensorCfg, "sensor_params_schema": paramsSchema,
				"is_global": isGlobal, "version": version, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// GET /api/v1/device-templates/:id
func getDeviceTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		tmplID := chi.URLParam(r, "id")

		var id, protocol, name, manufacturer, model, description string
		var orgIDPtr *string
		var sensorCfgRaw, paramsSchemaRaw []byte
		var isGlobal bool
		var version int
		var createdAt time.Time
		var readerTemplateID *string

		err := pool.QueryRow(context.Background(),
			`SELECT id, org_id, protocol, name, manufacturer, model, description,
			        sensor_config, sensor_params_schema, reader_template_id,
			        is_global, version, created_at
			 FROM device_templates
			 WHERE id=$1 AND (is_global=TRUE OR org_id=$2)`, tmplID, orgID,
		).Scan(&id, &orgIDPtr, &protocol, &name, &manufacturer, &model, &description,
			&sensorCfgRaw, &paramsSchemaRaw, &readerTemplateID, &isGlobal, &version, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "device template not found")
			return
		}
		var sensorCfg, paramsSchema any
		json.Unmarshal(sensorCfgRaw, &sensorCfg)
		json.Unmarshal(paramsSchemaRaw, &paramsSchema)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "org_id": orgIDPtr, "protocol": protocol,
			"name": name, "manufacturer": manufacturer, "model": model,
			"description": description, "reader_template_id": readerTemplateID,
			"sensor_config": sensorCfg, "sensor_params_schema": paramsSchema,
			"is_global": isGlobal, "version": version, "created_at": createdAt,
		})
	}
}

// POST /api/v1/device-templates
func createDeviceTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)

		var req struct {
			Name              string `json:"name"`
			Protocol          string `json:"protocol"`
			Manufacturer      string `json:"manufacturer"`
			Model             string `json:"model"`
			Description       string `json:"description"`
			SensorConfig       any    `json:"sensor_config"`
			SensorParamsSchema any    `json:"sensor_params_schema"`
			ReaderTemplateID   string `json:"reader_template_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.Protocol == "" {
			writeError(w, http.StatusBadRequest, "name and protocol are required")
			return
		}

		var protoExists bool
		pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM protocols WHERE id=$1 AND is_active=TRUE)`,
			req.Protocol).Scan(&protoExists)
		if !protoExists {
			writeError(w, http.StatusBadRequest, "unknown or inactive protocol: "+req.Protocol)
			return
		}

		isGlobal := role == "superadmin"

		sensorCfg, _ := json.Marshal(req.SensorConfig)
		paramsSchema, _ := json.Marshal(req.SensorParamsSchema)
		if sensorCfg == nil {
			sensorCfg = []byte("{}")
		}
		if paramsSchema == nil {
			paramsSchema = []byte("{}")
		}

		var id string
		err := pool.QueryRow(context.Background(),
			`INSERT INTO device_templates
			 (org_id, protocol, name, manufacturer, model, description,
			  sensor_config, sensor_params_schema, reader_template_id, is_global)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::UUID,$10) RETURNING id`,
			orgID, req.Protocol, req.Name, req.Manufacturer, req.Model,
			req.Description, sensorCfg, paramsSchema, req.ReaderTemplateID, isGlobal,
		).Scan(&id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create device template")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": id, "is_global": isGlobal, "message": "device template created",
		})
	}
}

// PUT /api/v1/device-templates/:id
func updateDeviceTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		var existingOrgID *string
		var isGlobal bool
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global FROM device_templates WHERE id=$1`, tmplID,
		).Scan(&existingOrgID, &isGlobal)
		if err != nil {
			writeError(w, http.StatusNotFound, "device template not found")
			return
		}
		if role != "superadmin" {
			if isGlobal {
				writeError(w, http.StatusForbidden, "global templates only editable by superadmin")
				return
			}
			if existingOrgID == nil || *existingOrgID != orgID {
				writeError(w, http.StatusForbidden, "not your template")
				return
			}
		}

		var req struct {
			Name               string `json:"name"`
			Manufacturer       string `json:"manufacturer"`
			Model              string `json:"model"`
			Description        string `json:"description"`
			SensorConfig       any    `json:"sensor_config"`
			SensorParamsSchema any    `json:"sensor_params_schema"`
			ReaderTemplateID   string `json:"reader_template_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		sensorCfg, _ := json.Marshal(req.SensorConfig)
		paramsSchema, _ := json.Marshal(req.SensorParamsSchema)

		_, err = pool.Exec(context.Background(),
			`UPDATE device_templates
			 SET name=$1, manufacturer=$2, model=$3, description=$4,
			     sensor_config=$5, sensor_params_schema=$6, 
			     reader_template_id=NULLIF($7,'')::UUID, version=version+1
			 WHERE id=$8`,
			req.Name, req.Manufacturer, req.Model, req.Description,
			sensorCfg, paramsSchema, req.ReaderTemplateID, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": true, "id": tmplID})
	}
}

// PATCH /api/v1/device-templates/:id/config — add/update/delete entries in sensor_config
func patchDeviceTemplateConfigHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		var existingOrgID *string
		var isGlobal bool
		var cfgRaw []byte
		var protocol string
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global, sensor_config, protocol FROM device_templates WHERE id=$1`,
			tmplID,
		).Scan(&existingOrgID, &isGlobal, &cfgRaw, &protocol)
		if err != nil {
			writeError(w, http.StatusNotFound, "device template not found")
			return
		}
		if role != "superadmin" {
			if isGlobal {
				writeError(w, http.StatusForbidden, "global templates only editable by superadmin")
				return
			}
			if existingOrgID == nil || *existingOrgID != orgID {
				writeError(w, http.StatusForbidden, "not your template")
				return
			}
		}

		var rawReq map[string]any
		if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		var newCfg []byte
		var totalEntries int

		if sc, ok := rawReq["sensor_config"]; ok {
			// Full sensor_config replacement
			newCfg, _ = json.Marshal(sc)
			if arr, ok2 := sc.(map[string]any)[protocolArrayKey(protocol)].([]any); ok2 {
				totalEntries = len(arr)
			}
		} else {
			// Fine-grained action: add / update / delete
			action, _ := rawReq["action"].(string)
			index := 0
			if v, ok2 := rawReq["index"].(float64); ok2 {
				index = int(v)
			}
			entry := rawReq["entry"]

			arrayKey := protocolArrayKey(protocol)

			var cfg map[string]any
			json.Unmarshal(cfgRaw, &cfg)
			if cfg == nil {
				cfg = map[string]any{}
			}

			arr, _ := cfg[arrayKey].([]any)
			if arr == nil {
				arr = []any{}
			}

			switch action {
			case "add":
				arr = append(arr, entry)
			case "update":
				if index < 0 || index >= len(arr) {
					writeError(w, http.StatusBadRequest, "index out of range")
					return
				}
				arr[index] = entry
			case "delete":
				if index < 0 || index >= len(arr) {
					writeError(w, http.StatusBadRequest, "index out of range")
					return
				}
				arr = append(arr[:index], arr[index+1:]...)
			default:
				writeError(w, http.StatusBadRequest, "action must be add, update, or delete")
				return
			}

			cfg[arrayKey] = arr
			newCfg, _ = json.Marshal(cfg)
			totalEntries = len(arr)
		}

		_, err = pool.Exec(context.Background(),
			`UPDATE device_templates SET sensor_config=$1, version=version+1 WHERE id=$2`,
			newCfg, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		var result any
		json.Unmarshal(newCfg, &result)
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":       true,
			"total_entries": totalEntries,
			"sensor_config": result,
		})
	}
}

// DELETE /api/v1/device-templates/:id
func deleteDeviceTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		var existingOrgID *string
		var isGlobal bool
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global FROM device_templates WHERE id=$1`, tmplID,
		).Scan(&existingOrgID, &isGlobal)
		if err != nil {
			writeError(w, http.StatusNotFound, "device template not found")
			return
		}
		if isGlobal && role != "superadmin" {
			writeError(w, http.StatusForbidden, "global templates can only be deleted by superadmin")
			return
		}
		if !isGlobal && (existingOrgID == nil || *existingOrgID != orgID) {
			writeError(w, http.StatusForbidden, "not your template")
			return
		}

		ctx := context.Background()
		// Detach sensors before deleting (FK: sensors.template_id → device_templates.id)
		pool.Exec(ctx, `UPDATE sensors SET template_id=NULL WHERE template_id=$1`, tmplID)
		_, err = pool.Exec(ctx, `DELETE FROM device_templates WHERE id=$1`, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// READER TEMPLATES — container config (Docker image, connection schema)
// Managed by IoT team (superadmin only)
// ═══════════════════════════════════════════════════════════════════════════════

// GET /api/v1/reader-templates
func listReaderTemplatesHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		protocol := r.URL.Query().Get("protocol")

		query := `SELECT id, protocol, name, description, image_suffix,
		                 connection_schema, env_defaults, version, created_at
		          FROM reader_templates`
		args := []any{}
		if protocol != "" {
			query += " WHERE protocol = $1"
			args = append(args, protocol)
		}
		query += " ORDER BY protocol ASC, name ASC"

		rows, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, protocol, name, description, imageSuffix string
			var connSchemaRaw, envDefaultsRaw []byte
			var version int
			var createdAt time.Time
			if err := rows.Scan(&id, &protocol, &name, &description, &imageSuffix,
				&connSchemaRaw, &envDefaultsRaw, &version, &createdAt); err != nil {
				continue
			}
			var connSchema, envDefaults any
			json.Unmarshal(connSchemaRaw, &connSchema)
			json.Unmarshal(envDefaultsRaw, &envDefaults)
			result = append(result, map[string]any{
				"id": id, "protocol": protocol, "name": name,
				"description": description, "image_suffix": imageSuffix,
				"connection_schema": connSchema, "env_defaults": envDefaults,
				"version": version, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// GET /api/v1/reader-templates/:id
func getReaderTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmplID := chi.URLParam(r, "id")

		var id, protocol, name, description, imageSuffix string
		var connSchemaRaw, envDefaultsRaw []byte
		var version int
		var createdAt time.Time

		err := pool.QueryRow(context.Background(),
			`SELECT id, protocol, name, description, image_suffix,
			        connection_schema, env_defaults, version, created_at
			 FROM reader_templates WHERE id=$1`, tmplID,
		).Scan(&id, &protocol, &name, &description, &imageSuffix,
			&connSchemaRaw, &envDefaultsRaw, &version, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader template not found")
			return
		}
		var connSchema, envDefaults any
		json.Unmarshal(connSchemaRaw, &connSchema)
		json.Unmarshal(envDefaultsRaw, &envDefaults)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "protocol": protocol, "name": name,
			"description": description, "image_suffix": imageSuffix,
			"connection_schema": connSchema, "env_defaults": envDefaults,
			"version": version, "created_at": createdAt,
		})
	}
}

// POST /api/v1/reader-templates (superadmin only)
func createReaderTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Protocol         string `json:"protocol"`
			Name             string `json:"name"`
			Description      string `json:"description"`
			ImageSuffix      string `json:"image_suffix"`
			ConnectionSchema any    `json:"connection_schema"`
			EnvDefaults      any    `json:"env_defaults"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.Protocol == "" || req.ImageSuffix == "" {
			writeError(w, http.StatusBadRequest, "name, protocol, and image_suffix are required")
			return
		}

		connSchema, _ := json.Marshal(req.ConnectionSchema)
		envDefaults, _ := json.Marshal(req.EnvDefaults)
		if connSchema == nil {
			connSchema = []byte("{}")
		}
		if envDefaults == nil {
			envDefaults = []byte("{}")
		}

		var id string
		err := pool.QueryRow(context.Background(),
			`INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			req.Protocol, req.Name, req.Description, req.ImageSuffix, connSchema, envDefaults,
		).Scan(&id)
		if err != nil {
			if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
				writeError(w, http.StatusConflict, "a reader template with this protocol and image_suffix already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to create reader template")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "message": "reader template created"})
	}
}

// PUT /api/v1/reader-templates/:id (superadmin only)
func updateReaderTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmplID := chi.URLParam(r, "id")

		var req struct {
			Name             string `json:"name"`
			Description      string `json:"description"`
			ImageSuffix      string `json:"image_suffix"`
			ConnectionSchema any    `json:"connection_schema"`
			EnvDefaults      any    `json:"env_defaults"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		connSchema, _ := json.Marshal(req.ConnectionSchema)
		envDefaults, _ := json.Marshal(req.EnvDefaults)

		tag, err := pool.Exec(context.Background(),
			`UPDATE reader_templates
			 SET name=$1, description=$2, image_suffix=$3,
			     connection_schema=$4, env_defaults=$5, version=version+1
			 WHERE id=$6`,
			req.Name, req.Description, req.ImageSuffix, connSchema, envDefaults, tmplID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "reader template not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": true, "id": tmplID})
	}
}

// DELETE /api/v1/reader-templates/:id (superadmin only)
func deleteReaderTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmplID := chi.URLParam(r, "id")
		tag, err := pool.Exec(context.Background(),
			`DELETE FROM reader_templates WHERE id=$1`, tmplID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "reader template not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func protocolArrayKey(protocol string) string {
	switch protocol {
	case "modbus_tcp":
		return "registers"
	case "opcua":
		return "nodes"
	case "snmp":
		return "oids"
	case "mqtt":
		return "json_paths"
	case "http":
		return "json_paths"
	default:
		return "entries"
	}
}
