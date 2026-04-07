package tpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /v1/sync/state — conf-agent polls this to check for config changes
func syncStateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		var hash string
		var configVersion int
		var updatedAt time.Time
		err := pool.QueryRow(context.Background(),
			`SELECT hash, config_version, generated_at FROM config_state WHERE qube_id=$1`, qubeID,
		).Scan(&hash, &configVersion, &updatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config state not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"qube_id":        qubeID,
			"hash":           hash,
			"config_version": configVersion,
			"updated_at":     updatedAt,
		})
	}
}

// GET /v1/sync/config — conf-agent downloads full config when hash changes
// v2: Returns JSON data that conf-agent writes to SQLite + docker-compose.yml
// No more CSV files — all config lives in SQLite on the Qube.
func syncConfigHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		ctx := context.Background()

		var hash, location string
		var configVersion int
		err := pool.QueryRow(ctx,
			`SELECT cs.hash, cs.config_version, q.location_label
			 FROM config_state cs JOIN qubes q ON q.id=cs.qube_id
			 WHERE cs.qube_id=$1`, qubeID,
		).Scan(&hash, &configVersion, &location)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config not found")
			return
		}

		// Load all readers + their sensors
		readers, err := loadReadersForQube(ctx, pool, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error loading readers")
			return
		}

		// Load containers
		containers, err := loadContainersForQube(ctx, pool, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error loading containers")
			return
		}

		// Load coreswitch settings
		csSettings, err := loadCoreSwitchSettings(ctx, pool, qubeID)
		if err != nil {
			csSettings = map[string]string{}
		}

		// Load telemetry settings
		telemetrySettings, err := loadTelemetrySettings(ctx, pool, qubeID)
		if err != nil {
			telemetrySettings = []map[string]any{}
		}

		// Build docker-compose.yml
		composeYML := buildComposeYML(pool, qubeID, location, containers)

		writeJSON(w, http.StatusOK, map[string]any{
			"hash":                hash,
			"config_version":      configVersion,
			"docker_compose_yml":  composeYML,
			"readers":             readers,
			"containers":          containers,
			"coreswitch_settings": csSettings,
			"telemetry_settings":  telemetrySettings,
		})
	}
}

// POST /v1/heartbeat
func heartbeatHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		_, err := pool.Exec(context.Background(),
			`UPDATE qubes SET last_seen=NOW(), status='online' WHERE id=$1`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "heartbeat failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"acknowledged": true,
			"server_time":  time.Now().UTC(),
			"qube_id":      qubeID,
		})
	}
}

// ─── Data loaders ────────────────────────────────────────────────────────────

type readerConfig struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Protocol string           `json:"protocol"`
	Config   any              `json:"config_json"`
	Sensors  []sensorConfig   `json:"sensors"`
}

type sensorConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Config    any    `json:"config_json"`
	Tags      any    `json:"tags_json"`
	Output    string `json:"output"`
	TableName string `json:"table_name"`
}

type containerConfig struct {
	ID       string `json:"id"`
	ReaderID *string `json:"reader_id"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	Env      any    `json:"env_json"`
	Protocol string `json:"protocol"`
}

func loadReadersForQube(ctx context.Context, pool *pgxpool.Pool, qubeID string) ([]readerConfig, error) {
	rows, err := pool.Query(ctx,
		`SELECT rd.id, rd.name, rd.protocol, rd.config_json
		 FROM readers rd
		 WHERE rd.qube_id=$1 AND rd.status='active'
		 ORDER BY rd.created_at ASC`, qubeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var readers []readerConfig
	for rows.Next() {
		var rd readerConfig
		var cfgRaw []byte
		if err := rows.Scan(&rd.ID, &rd.Name, &rd.Protocol, &cfgRaw); err != nil {
			continue
		}
		json.Unmarshal(cfgRaw, &rd.Config)

		// Load sensors
		srows, _ := pool.Query(ctx,
			`SELECT id, name, config_json, tags_json, output, table_name
			 FROM sensors WHERE reader_id=$1 AND status='active'
			 ORDER BY created_at ASC`, rd.ID)
		for srows.Next() {
			var s sensorConfig
			var sCfgRaw, sTagsRaw []byte
			if err := srows.Scan(&s.ID, &s.Name, &sCfgRaw, &sTagsRaw, &s.Output, &s.TableName); err == nil {
				json.Unmarshal(sCfgRaw, &s.Config)
				json.Unmarshal(sTagsRaw, &s.Tags)
				rd.Sensors = append(rd.Sensors, s)
			}
		}
		srows.Close()
		readers = append(readers, rd)
	}
	return readers, nil
}

func loadContainersForQube(ctx context.Context, pool *pgxpool.Pool, qubeID string) ([]containerConfig, error) {
	rows, err := pool.Query(ctx,
		`SELECT c.id, c.reader_id, c.name, c.image, c.env_json,
		        COALESCE(rd.protocol, '') AS protocol
		 FROM containers c
		 LEFT JOIN readers rd ON rd.id = c.reader_id
		 WHERE c.qube_id=$1 AND c.status='active'
		 ORDER BY c.created_at ASC`, qubeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []containerConfig
	for rows.Next() {
		var c containerConfig
		var envRaw []byte
		if err := rows.Scan(&c.ID, &c.ReaderID, &c.Name, &c.Image, &envRaw, &c.Protocol); err != nil {
			continue
		}
		json.Unmarshal(envRaw, &c.Env)
		containers = append(containers, c)
	}
	return containers, nil
}

func loadCoreSwitchSettings(ctx context.Context, pool *pgxpool.Pool, qubeID string) (map[string]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT key, value_json FROM coreswitch_settings WHERE qube_id=$1`, qubeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			settings[k] = v
		}
	}
	return settings, nil
}

func loadTelemetrySettings(ctx context.Context, pool *pgxpool.Pool, qubeID string) ([]map[string]any, error) {
	rows, err := pool.Query(ctx,
		`SELECT device, reading, agg_time_min, agg_func, sensor_id, tag_names
		 FROM telemetry_settings WHERE qube_id=$1
		 ORDER BY device ASC`, qubeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []map[string]any
	for rows.Next() {
		var device, reading, aggFunc string
		var aggTime int
		var sensorID, tagNames *string
		if err := rows.Scan(&device, &reading, &aggTime, &aggFunc, &sensorID, &tagNames); err != nil {
			continue
		}
		settings = append(settings, map[string]any{
			"device":       device,
			"reading":      reading,
			"agg_time_min": aggTime,
			"agg_func":     aggFunc,
			"sensor_id":    sensorID,
			"tag_names":    tagNames,
		})
	}
	return settings, nil
}

// ─── Docker Compose Builder (v2) ─────────────────────────────────────────────
// Generates docker-compose.yml for Swarm deployment.
// v2 changes:
//   - No CSV volume mounts — readers read from shared SQLite
//   - Shared qube-data volume for SQLite
//   - READER_ID + SQLITE_PATH + CORESWITCH_URL env vars
//   - No MQTT broker service (removed from Qube)

func buildComposeYML(pool *pgxpool.Pool, qubeID, location string, containers []containerConfig) string {
	var b bytes.Buffer
	loc := location
	if loc == "" {
		loc = "unset"
	}

	confAgentImage := imageForService(pool, "conf_agent")
	influxSQLImage := imageForService(pool, "influx_sql")

	fmt.Fprintf(&b, `version: "3.8"
# Generated by Qube Enterprise v2 TP-API
# Qube: %s | Location: %s
# Deploy: docker stack deploy -c docker-compose.yml qube

networks:
  qube-net:
    external: true

volumes:
  qube-data:
    driver: local

services:

  # ── Enterprise Conf-Agent ──────────────────────────────────────────────────
  enterprise-conf-agent:
    image: %s
    hostname: enterprise-conf-agent
    networks:
      - qube-net
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/qube:/opt/qube
      - qube-data:/opt/qube/data
    environment:
      - QUBE_ID=%s
      - SQLITE_PATH=/opt/qube/data/qube.db
      - TPAPI_URL=${TPAPI_URL}
      - QUBE_TOKEN=${QUBE_TOKEN}
      - CLOUD_WS_URL=${CLOUD_WS_URL}
      - POLL_INTERVAL=${POLL_INTERVAL:-30}
    logging:
      driver: "local"
      options:
        max-size: "10mb"
        max-file: "20"
    deploy:
      replicas: 1
      restart_policy:
        condition: any

  # ── Enterprise Influx-to-SQL ───────────────────────────────────────────────
  enterprise-influx-to-sql:
    image: %s
    hostname: enterprise-influx-to-sql
    networks:
      - qube-net
    volumes:
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
      - qube-data:/opt/qube/data:ro
    environment:
      - INFLUX_URL=http://influxdb:8086
      - INFLUX_DB=edgex
      - SQLITE_PATH=/opt/qube/data/qube.db
      - TPAPI_URL=${TPAPI_URL}
      - QUBE_ID=%s
      - QUBE_TOKEN=${QUBE_TOKEN}
    logging:
      driver: "local"
      options:
        max-size: "10mb"
        max-file: "20"
    deploy:
      replicas: 1
      restart_policy:
        condition: any

`, qubeID, loc, confAgentImage, qubeID, influxSQLImage, qubeID)

	// One service per reader container
	for _, c := range containers {
		fmt.Fprintf(&b, "  # Reader: %s (%s)\n", c.Name, c.Protocol)
		fmt.Fprintf(&b, "  %s:\n", c.Name)
		fmt.Fprintf(&b, "    image: %s\n", c.Image)
		fmt.Fprintf(&b, "    hostname: %s\n", c.Name)
		fmt.Fprintf(&b, "    networks:\n      - qube-net\n")
		fmt.Fprintf(&b, "    volumes:\n")
		fmt.Fprintf(&b, "      - /etc/timezone:/etc/timezone:ro\n")
		fmt.Fprintf(&b, "      - /etc/localtime:/etc/localtime:ro\n")
		fmt.Fprintf(&b, "      - qube-data:/opt/qube/data:ro\n")
		fmt.Fprintf(&b, "    environment:\n")

		// Write env vars from container config
		if envMap, ok := c.Env.(map[string]any); ok {
			for k, v := range envMap {
				fmt.Fprintf(&b, "      - %s=%v\n", k, v)
			}
		}

		fmt.Fprintf(&b, "    logging:\n")
		fmt.Fprintf(&b, "      driver: \"local\"\n")
		fmt.Fprintf(&b, "      options:\n")
		fmt.Fprintf(&b, "        max-size: \"10mb\"\n")
		fmt.Fprintf(&b, "        max-file: \"20\"\n")
		fmt.Fprintf(&b, "    deploy:\n")
		fmt.Fprintf(&b, "      replicas: 1\n")
		fmt.Fprintf(&b, "      restart_policy:\n")
		fmt.Fprintf(&b, "        condition: any\n\n")
	}

	return b.String()
}

// imageForService resolves Docker image from registry_config
func imageForService(pool *pgxpool.Pool, serviceKey string) string {
	rows, err := pool.Query(context.Background(),
		`SELECT key, value FROM registry_config`)
	if err == nil {
		defer rows.Close()
		cfg := map[string]string{}
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			cfg[k] = v
		}
		arch := cfg["arch"]
		if arch == "" {
			arch = "arm64"
		}
		archTag := arch + ".latest"
		mode := cfg["mode"]
		switch mode {
		case "github":
			base := cfg["github_base"]
			if base == "" {
				base = "ghcr.io/sandun-s/qube-enterprise-home"
			}
			shortNames := map[string]string{
				"conf_agent": "conf-agent",
				"influx_sql": "influx-to-sql",
			}
			if name, ok := shortNames[serviceKey]; ok {
				return base + "/" + name + ":" + archTag
			}
			return base + "/" + serviceKey + ":" + archTag
		case "gitlab", "custom":
			imgKey := "img_" + serviceKey
			if v, ok := cfg[imgKey]; ok && v != "" {
				return v
			}
			base := cfg["gitlab_base"]
			if base == "" {
				base = "registry.gitlab.com/iot-team4/product"
			}
			prefixMap := map[string]string{
				"conf_agent": "enterprise-conf-agent",
				"influx_sql": "enterprise-influx-to-sql",
			}
			if name, ok := prefixMap[serviceKey]; ok {
				return base + "/" + name + ":" + archTag
			}
			return base + "/" + serviceKey + ":" + archTag
		}
	}
	reg := os.Getenv("QUBE_IMAGE_REGISTRY")
	if reg == "" {
		reg = "ghcr.io/sandun-s/qube-enterprise-home"
	}
	shortNames := map[string]string{
		"conf_agent": "conf-agent",
		"influx_sql": "influx-to-sql",
	}
	if name, ok := shortNames[serviceKey]; ok {
		return reg + "/" + name + ":arm64.latest"
	}
	return reg + "/" + serviceKey + ":arm64.latest"
}

// ─── Device Self-Registration ────────────────────────────────────────────────

func deviceRegisterHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DeviceID    string `json:"device_id"`
			RegisterKey string `json:"register_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.DeviceID == "" || req.RegisterKey == "" {
			writeError(w, http.StatusBadRequest, "device_id and register_key required")
			return
		}

		ctx := context.Background()

		var orgID *string
		var orgSecret string
		var claimed bool

		err := pool.QueryRow(ctx,
			`SELECT q.org_id,
			        COALESCE(o.org_secret, ''),
			        (q.org_id IS NOT NULL) AS claimed
			 FROM qubes q
			 LEFT JOIN organisations o ON o.id = q.org_id
			 WHERE q.id=$1 AND q.register_key=$2`,
			req.DeviceID, req.RegisterKey,
		).Scan(&orgID, &orgSecret, &claimed)

		if err != nil {
			writeError(w, http.StatusUnauthorized, "device not found or invalid register key")
			return
		}

		if !claimed || orgID == nil {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":     "pending",
				"device_id":  req.DeviceID,
				"message":    "Device not yet claimed. Customer must register device in the portal first.",
				"retry_secs": 60,
			})
			return
		}

		authToken := computeHMAC(req.DeviceID, orgSecret)

		pool.Exec(ctx,
			`UPDATE qubes SET last_seen=NOW(), status='online' WHERE id=$1`, req.DeviceID)

		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "claimed",
			"device_id":  req.DeviceID,
			"qube_token": authToken,
			"message":    "Device claimed. Save qube_token and begin sync.",
		})
	}
}
