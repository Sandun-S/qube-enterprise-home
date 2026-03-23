package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/templates — list global + org templates, filterable by protocol
func listTemplatesHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		protocol := r.URL.Query().Get("protocol") // optional filter

		query := `SELECT id, org_id, name, protocol, description,
		                 config_json, influx_fields_json, ui_mapping_json,
		                 is_global, created_at
		          FROM sensor_templates
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
			var id, name, protocol, description string
			var orgIDPtr *string
			var cfgRaw, fieldsRaw, uiRaw []byte
			var isGlobal bool
			var createdAt time.Time
			if err := rows.Scan(&id, &orgIDPtr, &name, &protocol, &description,
				&cfgRaw, &fieldsRaw, &uiRaw, &isGlobal, &createdAt); err != nil {
				continue
			}
			var cfg, fields, ui any
			json.Unmarshal(cfgRaw, &cfg)
			json.Unmarshal(fieldsRaw, &fields)
			json.Unmarshal(uiRaw, &ui)
			result = append(result, map[string]any{
				"id":                 id,
				"org_id":             orgIDPtr,
				"name":               name,
				"protocol":           protocol,
				"description":        description,
				"config_json":        cfg,
				"influx_fields_json": fields,
				"ui_mapping_json":    ui,
				"is_global":          isGlobal,
				"created_at":         createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// GET /api/v1/templates/:id — full template detail including all registers/nodes/OIDs
func getTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		tmplID := chi.URLParam(r, "id")

		var id, name, protocol, description string
		var orgIDPtr *string
		var cfgRaw, fieldsRaw, uiRaw []byte
		var isGlobal bool
		var createdAt time.Time

		err := pool.QueryRow(context.Background(),
			`SELECT id, org_id, name, protocol, description, config_json,
			        influx_fields_json, ui_mapping_json, is_global, created_at
			 FROM sensor_templates
			 WHERE id=$1 AND (is_global=TRUE OR org_id=$2)`, tmplID, orgID,
		).Scan(&id, &orgIDPtr, &name, &protocol, &description,
			&cfgRaw, &fieldsRaw, &uiRaw, &isGlobal, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		var cfg, fields, ui any
		json.Unmarshal(cfgRaw, &cfg)
		json.Unmarshal(fieldsRaw, &fields)
		json.Unmarshal(uiRaw, &ui)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "org_id": orgIDPtr, "name": name, "protocol": protocol,
			"description": description, "config_json": cfg,
			"influx_fields_json": fields, "ui_mapping_json": ui,
			"is_global": isGlobal, "created_at": createdAt,
		})
	}
}

// POST /api/v1/templates — create template (org-scoped, or global if admin+superadmin role)
// IoT team uses this to add new device types to the catalog.
func createTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)

		var req struct {
			Name             string `json:"name"`
			Protocol         string `json:"protocol"`
			Description      string `json:"description"`
			ConfigJSON       any    `json:"config_json"`
			InfluxFieldsJSON any    `json:"influx_fields_json"`
			UIMappingJSON    any    `json:"ui_mapping_json"`
			// IsGlobal can only be set true by superadmin role
			IsGlobal         bool   `json:"is_global"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.Protocol == "" {
			writeError(w, http.StatusBadRequest, "name and protocol are required")
			return
		}
		validProtocols := map[string]bool{
			"modbus_tcp": true, "mqtt": true, "opcua": true, "snmp": true,
		}
		if !validProtocols[req.Protocol] {
			writeError(w, http.StatusBadRequest, "protocol must be modbus_tcp, mqtt, opcua, or snmp")
			return
		}

		// Only superadmin can create global templates (IoT team internal role)
		isGlobal := req.IsGlobal && role == "superadmin"

		cfg, _    := json.Marshal(req.ConfigJSON)
		fields, _ := json.Marshal(req.InfluxFieldsJSON)
		ui, _     := json.Marshal(req.UIMappingJSON)
		if cfg == nil    { cfg = []byte("{}") }
		if fields == nil { fields = []byte("{}") }
		if ui == nil     { ui = []byte("{}") }

		var id string
		err := pool.QueryRow(context.Background(),
			`INSERT INTO sensor_templates
			 (org_id, name, protocol, description, config_json, influx_fields_json, ui_mapping_json, is_global)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			orgID, req.Name, req.Protocol, req.Description, cfg, fields, ui, isGlobal,
		).Scan(&id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create template")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":        id,
			"is_global": isGlobal,
			"message":   "template created",
		})
	}
}

// PUT /api/v1/templates/:id — update full template (name, description, full config_json replacement)
// IoT team uses this to update the entire register map / OID list for a device type.
func updateTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		// Check ownership — global templates only editable by superadmin
		var existingOrgID *string
		var isGlobal bool
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global FROM sensor_templates WHERE id=$1`, tmplID,
		).Scan(&existingOrgID, &isGlobal)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		if role != "superadmin" {
			if isGlobal {
				writeError(w, http.StatusForbidden, "global templates can only be updated by superadmin")
				return
			}
			if existingOrgID == nil || *existingOrgID != orgID {
				writeError(w, http.StatusForbidden, "cannot update another org's template")
				return
			}
		}

		var req struct {
			Name             string `json:"name"`
			Description      string `json:"description"`
			ConfigJSON       any    `json:"config_json"`
			InfluxFieldsJSON any    `json:"influx_fields_json"`
			UIMappingJSON    any    `json:"ui_mapping_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		cfg, _    := json.Marshal(req.ConfigJSON)
		fields, _ := json.Marshal(req.InfluxFieldsJSON)
		ui, _     := json.Marshal(req.UIMappingJSON)

		_, err = pool.Exec(context.Background(),
			`UPDATE sensor_templates
			 SET name=$1, description=$2, config_json=$3, influx_fields_json=$4, ui_mapping_json=$5
			 WHERE id=$6`,
			req.Name, req.Description, cfg, fields, ui, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": true, "id": tmplID})
	}
}

// PATCH /api/v1/templates/:id/registers — add or update individual register/node/OID entries
// without replacing the whole config_json. Useful for IoT team to fix one register.
// Body: {"action":"add"|"update"|"delete", "index":0, "entry":{...register object...}}
func patchTemplateRegistersHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		var existingOrgID *string
		var isGlobal bool
		var cfgRaw []byte
		var protocol string
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global, config_json, protocol FROM sensor_templates WHERE id=$1`, tmplID,
		).Scan(&existingOrgID, &isGlobal, &cfgRaw, &protocol)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		// superadmin can edit any template (global or org-scoped)
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
			Action string `json:"action"` // "add", "update", "delete"
			Index  int    `json:"index"`  // for update/delete: 0-based index in array
			Entry  any    `json:"entry"`  // the register/node/OID object
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		// Determine the array key based on protocol
		arrayKey := protocolArrayKey(protocol)

		var cfg map[string]any
		json.Unmarshal(cfgRaw, &cfg)
		if cfg == nil { cfg = map[string]any{} }

		arr, _ := cfg[arrayKey].([]any)
		if arr == nil { arr = []any{} }

		switch req.Action {
		case "add":
			arr = append(arr, req.Entry)
		case "update":
			if req.Index < 0 || req.Index >= len(arr) {
				writeError(w, http.StatusBadRequest, "index out of range")
				return
			}
			arr[req.Index] = req.Entry
		case "delete":
			if req.Index < 0 || req.Index >= len(arr) {
				writeError(w, http.StatusBadRequest, "index out of range")
				return
			}
			arr = append(arr[:req.Index], arr[req.Index+1:]...)
		default:
			writeError(w, http.StatusBadRequest, "action must be add, update, or delete")
			return
		}

		cfg[arrayKey] = arr
		newCfg, _ := json.Marshal(cfg)

		_, err = pool.Exec(context.Background(),
			`UPDATE sensor_templates SET config_json=$1 WHERE id=$2`, newCfg, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		var result any
		json.Unmarshal(newCfg, &result)
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":      true,
			"action":       req.Action,
			"total_entries": len(arr),
			"config_json":  result,
		})
	}
}

// DELETE /api/v1/templates/:id
func deleteTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		role, _ := r.Context().Value(ctxRole).(string)
		tmplID := chi.URLParam(r, "id")

		var existingOrgID *string
		var isGlobal bool
		err := pool.QueryRow(context.Background(),
			`SELECT org_id, is_global FROM sensor_templates WHERE id=$1`, tmplID,
		).Scan(&existingOrgID, &isGlobal)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
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

		_, err = pool.Exec(context.Background(),
			`DELETE FROM sensor_templates WHERE id=$1`, tmplID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

// GET /api/v1/templates/:id/preview — preview the CSV that would be generated
// for a given set of address_params. Useful for IoT team to validate a template.
func previewTemplateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		tmplID := chi.URLParam(r, "id")

		var protocol string
		var cfgRaw []byte
		err := pool.QueryRow(context.Background(),
			`SELECT protocol, config_json FROM sensor_templates
			 WHERE id=$1 AND (is_global=TRUE OR org_id=$2)`, tmplID, orgID,
		).Scan(&protocol, &cfgRaw)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}

		// Parse address_params from query string or body
		var addrParams any = map[string]any{"unit_id": 1}
		if ap := r.URL.Query().Get("address_params"); ap != "" {
			json.Unmarshal([]byte(ap), &addrParams)
		}

		csvRows, csvType, err := generateCSVRows(
			"preview-sensor-id", "PreviewSensor", protocol, cfgRaw,
			addrParams, map[string]any{"preview": "true"},
			[]byte("{}"), "192.168.1.1", 502,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "preview failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"protocol":  protocol,
			"csv_type":  csvType,
			"row_count": len(csvRows),
			"rows":      csvRows,
		})
	}
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func protocolArrayKey(protocol string) string {
	switch protocol {
	case "modbus_tcp": return "registers"
	case "opcua":      return "nodes"
	case "snmp":       return "oids"
	case "mqtt":       return "readings"
	default:           return "entries"
	}
}
