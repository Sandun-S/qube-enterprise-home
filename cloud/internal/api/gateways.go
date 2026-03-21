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

// protocolImageMap maps protocol → default Docker image (real GitLab registry images)
func protocolImageMap(protocol string) (image string, port int) {
	switch protocol {
	case "modbus_tcp":
		return "registry.gitlab.com/iot-team4/product/modbus-gateway:arm64.latest", 502
	case "opcua":
		return "registry.gitlab.com/iot-team4/product/opc-ua-gateway:arm64.latest", 4840
	case "snmp":
		return "registry.gitlab.com/iot-team4/product/snmp-gateway:arm64.latest", 161
	case "mqtt":
		return "registry.gitlab.com/iot-team4/product/mqtt-gateway:arm64.latest", 1883
	default:
		return "busybox:latest", 0
	}
}

// GET /api/v1/qubes/:id/gateways
func listGatewaysHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT g.id, g.name, g.protocol, g.host, g.port, g.config_json,
			        g.service_image, g.status, g.created_at,
			        COUNT(s.id) AS sensor_count
			 FROM gateways g
			 LEFT JOIN sensors s ON s.gateway_id = g.id AND s.status='active'
			 WHERE g.qube_id=$1
			 GROUP BY g.id ORDER BY g.created_at ASC`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, protocol, host, image, status string
			var port, sensorCount int
			var cfgRaw []byte
			var createdAt time.Time
			if err := rows.Scan(&id, &name, &protocol, &host, &port, &cfgRaw,
				&image, &status, &createdAt, &sensorCount); err != nil {
				continue
			}
			var cfg any
			json.Unmarshal(cfgRaw, &cfg)
			result = append(result, map[string]any{
				"id": id, "name": name, "protocol": protocol,
				"host": host, "port": port, "config_json": cfg,
				"service_image": image, "status": status,
				"sensor_count": sensorCount, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// POST /api/v1/qubes/:id/gateways
func createGatewayHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		var req struct {
			Name       string `json:"name"`
			Protocol   string `json:"protocol"`
			Host       string `json:"host"`
			Port       int    `json:"port"`
			ConfigJSON any    `json:"config_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.Protocol == "" {
			writeError(w, http.StatusBadRequest, "name and protocol are required")
			return
		}

		validProtos := map[string]bool{
			"modbus_tcp": true, "mqtt": true, "opcua": true, "snmp": true,
		}
		if !validProtos[req.Protocol] {
			writeError(w, http.StatusBadRequest, "protocol must be modbus_tcp, mqtt, opcua, or snmp")
			return
		}

		cfgBytes, _ := json.Marshal(req.ConfigJSON)
		if cfgBytes == nil { cfgBytes = []byte("{}") }

		defaultImage, defaultPort := protocolImageMap(req.Protocol)
		if req.Port == 0 { req.Port = defaultPort }

		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)

		var gwID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO gateways (qube_id, name, protocol, host, port, config_json, service_image)
			 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			qubeID, req.Name, req.Protocol, req.Host, req.Port, cfgBytes, defaultImage,
		).Scan(&gwID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create gateway")
			return
		}

		// Auto-create the service entry for this gateway.
		// Service name = sanitized gateway name (must be unique per Qube).
		// Multiple gateways of same protocol = multiple distinct service names = multiple containers.
		serviceName := sanitizeServiceName(req.Name)
		envBytes, _ := json.Marshal(buildEnvJSON(req.Protocol, req.Host, req.Port, req.ConfigJSON, serviceName))

		var svcID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO services (qube_id, gateway_id, name, image, port, env_json)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			qubeID, gwID, serviceName, defaultImage, req.Port, envBytes,
		).Scan(&svcID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create service")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "commit failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"gateway_id":   gwID,
			"service_id":   svcID,
			"service_name": serviceName,
			"image":        defaultImage,
			"new_hash":     hash,
			"message":      "Gateway created. Conf-Agent will deploy within the next poll interval.",
		})
	}
}

// DELETE /api/v1/gateways/:gateway_id
func deleteGatewayHandler(pool *pgxpool.Pool) http.HandlerFunc {
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

		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM gateways WHERE id=$1`, gwID); err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":  true,
			"new_hash": hash,
			"message":  "Gateway deleted. Conf-Agent will remove the container within the next poll interval.",
		})
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// sanitizeServiceName converts a gateway name to a valid Docker service name.
// "Panel A #1" → "panel-a--1"
// Each gateway gets a unique name so multiple Modbus gateways = multiple containers.
func sanitizeServiceName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		} else if c >= 'A' && c <= 'Z' {
			out = append(out, c+32)
		} else {
			out = append(out, '-')
		}
	}
	// Trim leading/trailing dashes
	result := string(out)
	for len(result) > 0 && result[0] == '-' { result = result[1:] }
	for len(result) > 0 && result[len(result)-1] == '-' { result = result[:len(result)-1] }
	return result
}

// buildEnvJSON produces environment variables for the service record (informational).
func buildEnvJSON(protocol, host string, port int, configJSON any, serviceName string) map[string]any {
	env := map[string]any{
		"SERVICE_NAME": serviceName,
	}
	if host != "" { env["TARGET_HOST"] = host }
	if port != 0  { env["TARGET_PORT"] = port }

	if m, ok := configJSON.(map[string]any); ok {
		for k, v := range m {
			switch k {
			case "broker_url":        env["MQTT_BROKER_URL"] = v
			case "base_topic":        env["MQTT_BASE_TOPIC"] = v
			case "username":          env["MQTT_USERNAME"] = v
			case "unit_id":           env["MODBUS_UNIT_ID"] = v
			case "poll_interval_ms":  env["POLL_INTERVAL_MS"] = v
			case "community":         env["SNMP_COMMUNITY"] = v
			case "version":           env["SNMP_VERSION"] = v
			}
		}
	}
	return env
}

// GET /api/v1/gateways/:gateway_id
func getGatewayHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		gwID := chi.URLParam(r, "gateway_id")

		var id, name, protocol, host, image, status string
		var port int
		var cfgRaw []byte
		var createdAt time.Time
		err := pool.QueryRow(context.Background(),
			`SELECT g.id, g.name, g.protocol, g.host, g.port,
			        g.config_json, g.service_image, g.status, g.created_at
			 FROM gateways g JOIN qubes q ON q.id=g.qube_id
			 WHERE g.id=$1 AND q.org_id=$2`, gwID, orgID,
		).Scan(&id, &name, &protocol, &host, &port, &cfgRaw, &image, &status, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "gateway not found")
			return
		}
		var cfg any
		json.Unmarshal(cfgRaw, &cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "name": name, "protocol": protocol,
			"host": host, "port": port, "config_json": cfg,
			"service_image": image, "status": status, "created_at": createdAt,
		})
	}
}

// PUT /api/v1/gateways/:gateway_id — update host, port, config_json
func updateGatewayHandler(pool *pgxpool.Pool) http.HandlerFunc {
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

		var req struct {
			Host       *string `json:"host"`
			Port       *int    `json:"port"`
			ConfigJSON any     `json:"config_json"`
			Status     *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		setParts := []string{}
		args := []any{}
		i := 1
		if req.Host != nil {
			setParts = append(setParts, fmt.Sprintf("host=$%d", i))
			args = append(args, *req.Host); i++
		}
		if req.Port != nil {
			setParts = append(setParts, fmt.Sprintf("port=$%d", i))
			args = append(args, *req.Port); i++
		}
		if req.ConfigJSON != nil {
			b, _ := json.Marshal(req.ConfigJSON)
			setParts = append(setParts, fmt.Sprintf("config_json=$%d", i))
			args = append(args, b); i++
		}
		if req.Status != nil && (*req.Status == "active" || *req.Status == "disabled") {
			setParts = append(setParts, fmt.Sprintf("status=$%d", i))
			args = append(args, *req.Status); i++
		}
		if len(setParts) == 0 {
			writeError(w, http.StatusBadRequest, "nothing to update")
			return
		}

		query := fmt.Sprintf("UPDATE gateways SET %s WHERE id=$%d",
			strings.Join(setParts, ", "), i)
		args = append(args, gwID)
		if _, err := pool.Exec(context.Background(), query, args...); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		hash, _ := recomputeConfigHash(context.Background(), pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "gateway updated — conf-agent will sync within the next poll interval",
			"new_hash": hash,
		})
	}
}
