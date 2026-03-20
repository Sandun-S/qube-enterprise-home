package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/gateways/:gateway_id/sensors
func listSensorsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		gwID := chi.URLParam(r, "gateway_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT g.qube_id FROM gateways g JOIN qubes q ON q.id=g.qube_id
			 WHERE g.id=$1 AND q.org_id=$2`, gwID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "gateway not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT s.id, s.name, s.template_id, t.name AS template_name,
			        s.address_params, s.tags_json, s.status, s.created_at
			 FROM sensors s
			 JOIN sensor_templates t ON t.id = s.template_id
			 WHERE s.gateway_id=$1
			 ORDER BY s.created_at ASC`, gwID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, tmplID, tmplName, status string
			var apRaw, tagsRaw []byte
			var createdAt time.Time
			if err := rows.Scan(&id, &name, &tmplID, &tmplName, &apRaw, &tagsRaw, &status, &createdAt); err != nil {
				continue
			}
			var ap, tags any
			json.Unmarshal(apRaw, &ap)
			json.Unmarshal(tagsRaw, &tags)
			result = append(result, map[string]any{
				"id": id, "name": name, "template_id": tmplID,
				"template_name": tmplName, "address_params": ap,
				"tags_json": tags, "status": status, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// POST /api/v1/gateways/:gateway_id/sensors
func createSensorHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		gwID := chi.URLParam(r, "gateway_id")

		var qubeID, gwProtocol, gwHost string
		var gwPort int
		var gwCfgRaw []byte
		err := pool.QueryRow(context.Background(),
			`SELECT g.qube_id, g.protocol, g.host, g.port, g.config_json
			 FROM gateways g JOIN qubes q ON q.id=g.qube_id
			 WHERE g.id=$1 AND q.org_id=$2`, gwID, orgID,
		).Scan(&qubeID, &gwProtocol, &gwHost, &gwPort, &gwCfgRaw)
		if err != nil {
			writeError(w, http.StatusNotFound, "gateway not found")
			return
		}

		var req struct {
			Name          string `json:"name"`
			TemplateID    string `json:"template_id"`
			AddressParams any    `json:"address_params"`
			TagsJSON      any    `json:"tags_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.TemplateID == "" {
			writeError(w, http.StatusBadRequest, "name and template_id are required")
			return
		}

		ctx := context.Background()

		var tmplProtocol string
		var tmplCfgRaw []byte
		err = pool.QueryRow(ctx,
			`SELECT protocol, config_json FROM sensor_templates
			 WHERE id=$1 AND (is_global=TRUE OR org_id=$2)`,
			req.TemplateID, orgID,
		).Scan(&tmplProtocol, &tmplCfgRaw)
		if err != nil {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		if tmplProtocol != gwProtocol {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("template protocol (%s) does not match gateway protocol (%s)",
					tmplProtocol, gwProtocol))
			return
		}

		var svcID string
		err = pool.QueryRow(ctx, `SELECT id FROM services WHERE gateway_id=$1`, gwID).Scan(&svcID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "service not found for gateway")
			return
		}

		apBytes, _ := json.Marshal(req.AddressParams)
		tagsBytes, _ := json.Marshal(req.TagsJSON)
		if apBytes == nil { apBytes = []byte("{}") }
		if tagsBytes == nil { tagsBytes = []byte("{}") }

		tx, err := pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)

		var sensorID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO sensors (gateway_id, name, template_id, address_params, tags_json)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			gwID, req.Name, req.TemplateID, apBytes, tagsBytes,
		).Scan(&sensorID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create sensor")
			return
		}

		// Generate CSV rows using real gateway CSV formats
		csvRows, csvType, err := generateCSVRows(
			sensorID, req.Name, tmplProtocol, tmplCfgRaw,
			req.AddressParams, req.TagsJSON, gwCfgRaw, gwHost, gwPort,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "csv generation failed: "+err.Error())
			return
		}

		var maxOrder int
		tx.QueryRow(ctx, `SELECT COALESCE(MAX(row_order),0) FROM service_csv_rows WHERE service_id=$1`, svcID).Scan(&maxOrder)

		for i, row := range csvRows {
			rowBytes, _ := json.Marshal(row)
			if _, err := tx.Exec(ctx,
				`INSERT INTO service_csv_rows (service_id, sensor_id, csv_type, row_data, row_order)
				 VALUES ($1,$2,$3,$4,$5)`,
				svcID, sensorID, csvType, rowBytes, maxOrder+i+1,
			); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to insert csv row")
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "commit failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"sensor_id": sensorID,
			"csv_rows":  len(csvRows),
			"csv_type":  csvType,
			"new_hash":  hash,
			"message":   "Sensor created. CSV rows generated. Conf-Agent will sync within the next poll interval.",
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
			 JOIN gateways g ON g.id = s.gateway_id
			 JOIN qubes q ON q.id = g.qube_id
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

// ─── CSV Generation — matches EXACT real gateway file formats ─────────────────
//
// Modbus registers.csv:  #Equipment,Reading,RegType,Address,type,Output,Table,Tags
// OPC-UA nodes.csv:      #Table,Device,Reading,OpcNode,Type,Freq,Output,Tags
// SNMP devices.csv:      #Table,Device,SNMP_csv,Community,Version,Output,Tags
// (SNMP has separate device rows + OID rows in two files — we use one unified row per reading)

func generateCSVRows(
	sensorID, sensorName, protocol string,
	tmplCfgRaw []byte,
	addrParams, tagsJSON any,
	gwCfgRaw []byte,
	gwHost string, gwPort int,
) ([]map[string]any, string, error) {

	var tmplCfg map[string]any
	json.Unmarshal(tmplCfgRaw, &tmplCfg)

	ap, _ := addrParams.(map[string]any)
	if ap == nil { ap = map[string]any{} }

	tags, _ := tagsJSON.(map[string]any)
	if tags == nil { tags = map[string]any{} }

	var gwCfg map[string]any
	json.Unmarshal(gwCfgRaw, &gwCfg)
	if gwCfg == nil { gwCfg = map[string]any{} }

	tagsStr := flattenTags(tags)

	switch protocol {

	// ── Modbus TCP ──────────────────────────────────────────────────────────
	// Real format: Equipment,Reading,RegType,Address,type,Output,Table,Tags
	// gateway reads registers.csv and posts batch to core-switch /v3/batch
	case "modbus_tcp":
		registers, _ := tmplCfg["registers"].([]any)
		offset := toInt(ap["register_offset"], 0)
		rows := make([]map[string]any, 0, len(registers))
		for _, r := range registers {
			reg, ok := r.(map[string]any)
			if !ok { continue }
			addr := toInt(reg["address"], 0) + offset
			rows = append(rows, map[string]any{
				// Column order matches real registers.csv exactly
				"Equipment": sensorName,
				"Reading":   strVal(reg["field_key"], "value"),
				"RegType":   strVal(reg["register_type"], "Holding"),
				"Address":   addr,
				"Type":      strVal(reg["data_type"], "uint16"),
				"Output":    "influxdb",
				"Table":     strVal(reg["table"], "Measurements"),
				"Tags":      tagsStr,
			})
		}
		return rows, "registers", nil

	// ── OPC-UA ──────────────────────────────────────────────────────────────
	// Real format: Table,Device,Reading,OpcNode,Type,Freq,Output,Tags
	case "opcua":
		nodes, _ := tmplCfg["nodes"].([]any)
		freq := toInt(ap["freq_sec"], 10)
		rows := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			nm, ok := n.(map[string]any)
			if !ok { continue }
			// node_id can be overridden per-sensor in address_params
			nodeID := strVal(ap["node_id"], strVal(nm["node_id"], ""))
			rows = append(rows, map[string]any{
				"Table":    strVal(nm["table"], "Measurements"),
				"Device":   sensorName,
				"Reading":  strVal(nm["field_key"], "value"),
				"OpcNode":  nodeID,
				"Type":     strVal(nm["data_type"], "float"),
				"Freq":     freq,
				"Output":   "influxdb",
				"Tags":     tagsStr,
			})
		}
		return rows, "nodes", nil

	// ── SNMP ────────────────────────────────────────────────────────────────
	// Real format: Table,Device,SNMP_csv,Community,Version,Output,Tags
	// One row per device (the OID list goes into a separate snmp csv file)
	// We store the OIDs inline in row_data as a JSON field for the compose builder to write
	case "snmp":
		oids, _ := tmplCfg["oids"].([]any)
		community := strVal(ap["community"], strVal(gwCfg["community"], "public"))
		version   := strVal(ap["version"], strVal(gwCfg["version"], "2c"))
		snmpFile  := sensorName + ".csv" // each device gets its own OID csv
		rows := []map[string]any{{
			// Main devices.csv row
			"Table":    "Measurements",
			"Device":   sensorName,
			"SNMP_csv": snmpFile,
			"Community": community,
			"Version":  version,
			"Output":   "influxdb",
			"Tags":     tagsStr,
			// Embedded OID list — written as a separate file by compose builder
			"_oids":    oids,
			"_snmp_file": snmpFile,
		}}
		return rows, "devices", nil

	// ── MQTT ────────────────────────────────────────────────────────────────
	// mqtt-reader uses mapping.yml (YAML, not CSV)
	// We store as structured rows, compose builder writes mapping.yml
	case "mqtt":
		readings, _ := tmplCfg["readings"].([]any)
		topicPattern, _ := tmplCfg["topic_pattern"].(string)
		baseTopic, _ := gwCfg["base_topic"].(string)
		topicSuffix, _ := ap["topic_suffix"].(string)
		topic := buildTopic(topicPattern, baseTopic, topicSuffix)

		rows := make([]map[string]any, 0, len(readings))
		for _, rd := range readings {
			rdMap, ok := rd.(map[string]any)
			if !ok { continue }
			rows = append(rows, map[string]any{
				"Topic":    topic,
				"Table":    "Measurements",
				"Device":   sensorName,
				"Reading":  strVal(rdMap["field_key"], "value"),
				"JSONPath": strVal(rdMap["json_path"], "$.value"),
				"Output":   "influxdb",
				"Tags":     tagsStr,
			})
		}
		return rows, "topics", nil

	default:
		return nil, "", fmt.Errorf("unknown protocol: %s", protocol)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func buildTopic(pattern, baseTopic, topicSuffix string) string {
	if pattern == "" {
		if topicSuffix != "" { return baseTopic + "/" + topicSuffix }
		return baseTopic
	}
	result := pattern
	result = replaceAll(result, "{base_topic}", baseTopic)
	result = replaceAll(result, "{topic_suffix}", topicSuffix)
	return result
}

func replaceAll(s, old, newStr string) string {
	out := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			out += newStr; i += len(old)
		} else {
			out += string(s[i]); i++
		}
	}
	return out
}

func flattenTags(tags map[string]any) string {
	if len(tags) == 0 { return "" }
	out := ""
	for k, v := range tags {
		if out != "" { out += "," }
		out += fmt.Sprintf("%s=%v", k, v)
	}
	return out
}

func strVal(v any, def string) string {
	if s, ok := v.(string); ok && s != "" { return s }
	return def
}

func toInt(v any, def int) int {
	switch n := v.(type) {
	case float64: return int(n)
	case int:     return n
	case int64:   return int(n)
	}
	return def
}

func floatVal(v any, def float64) float64 {
	if f, ok := v.(float64); ok { return f }
	return def
}

// ─── Sensor CSV Row CRUD ──────────────────────────────────────────────────────
// After a sensor is created, individual register rows can be viewed and updated.
// Useful when a device has a non-standard register address for one specific field.

// GET /api/v1/sensors/:sensor_id/rows — list all CSV rows for a sensor
func listSensorRowsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")

		// Verify ownership
		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT cr.id, cr.csv_type, cr.row_data, cr.row_order
			 FROM service_csv_rows cr
			 JOIN services svc ON svc.id=cr.service_id
			 WHERE cr.sensor_id=$1
			 ORDER BY cr.row_order ASC`, sensorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, csvType string
			var rowOrder int
			var rawData []byte
			if err := rows.Scan(&id, &csvType, &rawData, &rowOrder); err != nil {
				continue
			}
			var data any
			json.Unmarshal(rawData, &data)
			result = append(result, map[string]any{
				"id":        id,
				"csv_type":  csvType,
				"row_data":  data,
				"row_order": rowOrder,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sensor_id": sensorID,
			"rows":      result,
			"count":     len(result),
		})
	}
}

// PUT /api/v1/sensors/:sensor_id/rows/:row_id — update a single register row
// Use this to fix a wrong address, change output, update tags etc.
func updateSensorRowHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")
		rowID := chi.URLParam(r, "row_id")

		// Verify ownership via sensor → gateway → qube → org
		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		var req struct {
			RowData any `json:"row_data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RowData == nil {
			writeError(w, http.StatusBadRequest, "row_data is required")
			return
		}

		newData, _ := json.Marshal(req.RowData)
		ctx := context.Background()
		tag, err := pool.Exec(ctx,
			`UPDATE service_csv_rows SET row_data=$1 WHERE id=$2 AND sensor_id=$3`,
			newData, rowID, sensorID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "row not found")
			return
		}

		// Recalculate config hash — this change needs to sync to Qube
		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":  true,
			"new_hash": hash,
			"message":  "Row updated. Conf-Agent will sync within next poll interval.",
		})
	}
}

// POST /api/v1/sensors/:sensor_id/rows — add a new register row to an existing sensor
// Use when you need to add an extra reading to a device that wasn't in the template.
func addSensorRowHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")

		var qubeID, svcID, csvType string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id, svc.id, COALESCE(cr.csv_type,'registers')
			 FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 JOIN services svc ON svc.gateway_id=g.id
			 LEFT JOIN service_csv_rows cr ON cr.sensor_id=s.id
			 WHERE s.id=$1 AND q.org_id=$2
			 LIMIT 1`, sensorID, orgID,
		).Scan(&qubeID, &svcID, &csvType)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		var req struct {
			RowData any    `json:"row_data"`
			CSVType string `json:"csv_type"` // optional override
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RowData == nil {
			writeError(w, http.StatusBadRequest, "row_data is required")
			return
		}
		if req.CSVType != "" { csvType = req.CSVType }

		// Get max row_order for this service
		var maxOrder int
		pool.QueryRow(context.Background(),
			`SELECT COALESCE(MAX(row_order),0) FROM service_csv_rows WHERE service_id=$1`, svcID,
		).Scan(&maxOrder)

		rowBytes, _ := json.Marshal(req.RowData)
		ctx := context.Background()
		var rowID string
		err = pool.QueryRow(ctx,
			`INSERT INTO service_csv_rows (service_id, sensor_id, csv_type, row_data, row_order)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			svcID, sensorID, csvType, rowBytes, maxOrder+1,
		).Scan(&rowID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add row")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"row_id":   rowID,
			"new_hash": hash,
			"message":  "Row added. Conf-Agent will sync within next poll interval.",
		})
	}
}

// DELETE /api/v1/sensors/:sensor_id/rows/:row_id — remove a register row
func deleteSensorRowHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		sensorID := chi.URLParam(r, "sensor_id")
		rowID := chi.URLParam(r, "row_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT q.id FROM sensors s
			 JOIN gateways g ON g.id=s.gateway_id
			 JOIN qubes q ON q.id=g.qube_id
			 WHERE s.id=$1 AND q.org_id=$2`, sensorID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "sensor not found")
			return
		}

		ctx := context.Background()
		tag, err := pool.Exec(ctx,
			`DELETE FROM service_csv_rows WHERE id=$1 AND sensor_id=$2`, rowID, sensorID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "row not found")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "new_hash": hash})
	}
}
