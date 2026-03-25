package tpapi

import (
	"bytes"
	"context"
	"os"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func syncStateHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		var hash string
		var updatedAt time.Time
		err := pool.QueryRow(context.Background(),
			`SELECT hash, generated_at FROM config_state WHERE qube_id=$1`, qubeID,
		).Scan(&hash, &updatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config state not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"qube_id":    qubeID,
			"hash":       hash,
			"updated_at": updatedAt,
		})
	}
}

func syncConfigHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID, _ := r.Context().Value(ctxQubeID).(string)
		ctx := context.Background()

		var hash, location string
		err := pool.QueryRow(ctx,
			`SELECT cs.hash, q.location_label
			 FROM config_state cs JOIN qubes q ON q.id=cs.qube_id
			 WHERE cs.qube_id=$1`, qubeID,
		).Scan(&hash, &location)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config not found")
			return
		}

		// Load all active services + their gateway details
		svcRows, err := pool.Query(ctx,
			`SELECT svc.id, svc.gateway_id, svc.name, svc.image, svc.port, svc.env_json,
			        g.protocol, g.host, g.port AS gw_port, g.config_json, g.name AS gw_name
			 FROM services svc
			 JOIN gateways g ON g.id = svc.gateway_id
			 WHERE svc.qube_id=$1 AND svc.status='active' AND g.status='active'
			 ORDER BY svc.created_at ASC`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error loading services")
			return
		}
		defer svcRows.Close()

		var services []svcMeta
		for svcRows.Next() {
			var s svcMeta
			var envRaw, gwCfgRaw []byte
			if err := svcRows.Scan(&s.ID, &s.GwID, &s.Name, &s.Image, &s.Port,
				&envRaw, &s.Protocol, &s.Host, &s.GwPort, &gwCfgRaw, &s.GwName); err != nil {
				continue
			}
			json.Unmarshal(envRaw, &s.EnvJSON)
			json.Unmarshal(gwCfgRaw, &s.GwCfgJSON)
			if s.EnvJSON == nil { s.EnvJSON = map[string]any{} }
			if s.GwCfgJSON == nil { s.GwCfgJSON = map[string]any{} }
			services = append(services, s)
		}
		svcRows.Close()

		// Build CSV files and sensor_map for each service
		csvFiles := map[string]string{}
		sensorMap := map[string]string{}

		for _, svc := range services {
			rows, err := pool.Query(ctx,
				`SELECT cr.row_data, cr.csv_type, s.id, s.name
				 FROM service_csv_rows cr
				 JOIN sensors s ON s.id = cr.sensor_id
				 WHERE cr.service_id=$1
				 ORDER BY cr.row_order ASC`, svc.ID)
			if err != nil { continue }

			var entries []csvEntry
			for rows.Next() {
				var e csvEntry
				var rawData []byte
				if err := rows.Scan(&rawData, &e.CSVType, &e.SensorID, &e.SensorName); err == nil {
					json.Unmarshal(rawData, &e.Data)
					if e.Data == nil { e.Data = map[string]any{} }
					entries = append(entries, e)
				}
			}
			rows.Close()

			if len(entries) == 0 { continue }

			// Render the main data file (registers.csv / nodes.csv / devices.csv / topics)
			mainFile, extraFiles := renderGatewayFiles(svc.Protocol, svc.Name, entries)
			csvFiles[fmt.Sprintf("configs/%s/config.csv", svc.Name)] = mainFile

			// Extra files (e.g. per-device SNMP OID files)
			for path, content := range extraFiles {
				csvFiles[path] = content
			}

			// Also write configs.yml for this gateway
			cfgYML := renderGatewayConfig(svc)
			csvFiles[fmt.Sprintf("configs/%s/configs.yml", svc.Name)] = cfgYML

			// Build sensor_map: "Equipment.Reading" → sensor_id
			for _, e := range entries {
				if reading, ok := e.Data["Reading"].(string); ok && reading != "" {
					key := fmt.Sprintf("%s.%s", e.SensorName, reading)
					sensorMap[key] = e.SensorID
				}
				// OPC-UA uses Device field
				if reading, ok := e.Data["Reading"].(string); ok {
					if device, ok2 := e.Data["Device"].(string); ok2 {
						key := fmt.Sprintf("%s.%s", device, reading)
						sensorMap[key] = e.SensorID
					}
				}
			}
		}

		composeYML := buildFullComposeYML(pool, qubeID, location, services)
		envFiles := map[string]string{}

		writeJSON(w, http.StatusOK, map[string]any{
			"hash":               hash,
			"docker_compose_yml": composeYML,
			"csv_files":          csvFiles,
			"env_files":          envFiles,
			"sensor_map":         sensorMap,
		})
	}
}

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

// ─── Service metadata ─────────────────────────────────────────────────────────

type svcMeta struct {
	ID        string
	GwID      string
	GwName    string
	Name      string
	Image     string
	Port      int
	EnvJSON   map[string]any
	Protocol  string
	Host      string
	GwPort    int
	GwCfgJSON map[string]any
}

type csvEntry struct {
	Data       map[string]any
	CSVType    string
	SensorID   string
	SensorName string
}

// ─── Gateway configs.yml generator ───────────────────────────────────────────
// Generates the configs.yml that the real gateway binary reads.
// Each gateway reads its own configs.yml at startup.

func renderGatewayConfig(svc svcMeta) string {
	var b bytes.Buffer
	switch svc.Protocol {
	case "modbus_tcp":
		// server URL: tcp://host:port  (real gateway format)
		port := svc.GwPort
		if port == 0 { port = 502 }
		server := fmt.Sprintf("tcp://%s:%d", svc.Host, port)
		freqSec := 20
		if v, ok := svc.GwCfgJSON["freq_sec"].(float64); ok && int(v) > 0 { freqSec = int(v) }
		singleRead := 120
		if v, ok := svc.GwCfgJSON["single_read_count"].(float64); ok && int(v) > 0 { singleRead = int(v) }
		fmt.Fprintf(&b, `loglevel: "info"
modbus:
  server: "%s"
  readingsfile: "config.csv"
  freqsec: %d
  singlereadcount: %d
http:
  data: "http://core-switch:8585/batch"
  alerts: "http://core-switch:8585/alerts"
`, server, freqSec, singleRead)

	case "opcua":
		// OpcEndPoint: full endpoint URL stored in host field
		// e.g. opc.tcp://192.168.1.18:52520/OPCUA/N4OpcUaServer
		endpoint := svc.Host
		fmt.Fprintf(&b, `LogLevel: "Info"
OpcUA:
  OpcEndPoint: "%s"
  PointsFile: "config.csv"
HTTP:
  Data: "http://core-switch:8585/batch"
  Alerts: "http://core-switch:8585/alerts"
`, endpoint)

	case "snmp":
		fetchInterval := 15
		if v, ok := svc.GwCfgJSON["fetch_interval"].(float64); ok { fetchInterval = int(v) }
		workerCount := 2
		if v, ok := svc.GwCfgJSON["worker_count"].(float64); ok { workerCount = int(v) }
		fmt.Fprintf(&b, `loglevel: "INFO"
snmp:
  fetch_interval: %d
  connect_timeout: 10
  worker_count: %d
  devices_file: "config.csv"
  maps_folder: "./maps"
http:
  data_url: "http://core-switch:8585/v3/batch"
  alerts_url: "http://core-switch:8585/v3/alerts"
`, fetchInterval, workerCount)

	case "mqtt":
		brokerURL := strVal(svc.GwCfgJSON["broker_url"], fmt.Sprintf("tcp://%s:%d", svc.Host, svc.GwPort))
		username  := strVal(svc.GwCfgJSON["username"], "")
		password  := strVal(svc.GwCfgJSON["password"], "")
		fmt.Fprintf(&b, `LogLevel: "Info"
MappingFile: "config.csv"
MQTT:
  Host: "%s"
  Port: %d
  User: "%s"
  Pass: "%s"
HTTP:
  Data: "http://core-switch:8080/v3/data"
  Alerts: "http://core-switch:8080/v3/alerts"
  IdleTO: 5
  MaxIdle: 3
`, brokerURL, svc.GwPort, username, password)
	}
	return b.String()
}

// ─── CSV/config file renderers ────────────────────────────────────────────────
// Returns (main file content, map of extra files)

func renderGatewayFiles(protocol, svcName string, rows []csvEntry) (string, map[string]string) {
	var b bytes.Buffer
	extraFiles := map[string]string{}

	switch protocol {

	case "modbus_tcp":
		// Real format: #Section,Equipment,Reading,RegType,Address,type,Output
		// Section = InfluxDB measurement name (maps to "table" field in template registers)
		// No Tags column in this format
		b.WriteString("#Section,Equipment,Reading,RegType,Address,type,Output\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%v,%s,%s\n",
				sv(r.Data, "Section"),    // measurement/table name
				sv(r.Data, "Equipment"),  // device name
				sv(r.Data, "Reading"),    // field key
				sv(r.Data, "RegType"),    // Holding/Input/Coil
				r.Data["Address"],        // register address
				sv(r.Data, "Type"),       // uint16/float32 etc
				sv(r.Data, "Output"))     // influxdb
		}

	case "opcua":
		// Real format: #Table,Device,Reading,OpcNode,Type,Freq,Output,Tags
		b.WriteString("#Table,Device,Reading,OpcNode,Type,Freq,Output,Tags\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%v,%s,%s\n",
				sv(r.Data, "Table"), sv(r.Data, "Device"),
				sv(r.Data, "Reading"), sv(r.Data, "OpcNode"),
				sv(r.Data, "Type"), r.Data["Freq"],
				sv(r.Data, "Output"), sv(r.Data, "Tags"))
		}

	case "snmp":
		// Real devices.csv format: #Table, Device, SNMP csv, Community, Version, Output, Tags
		// Device = IP address of the SNMP device (from addr_params.device_ip)
		// SNMP csv = template map filename (e.g. gxt-rt-ups.csv) — stored in template config_json.map_file
		// OID map file: field_name,OID  — two columns, NO header line
		b.WriteString("#Table, Device, SNMP csv, Community, Version, Output, Tags\n")

		writtenMaps := map[string]bool{}
		for _, r := range rows {
			snmpFile := sv(r.Data, "SNMP_csv")
			deviceIP  := sv(r.Data, "DeviceIP")
			if deviceIP == "" { deviceIP = "0.0.0.0" }

			// tags: always include name=SensorName
			tags := sv(r.Data, "Tags")

			fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n",
				sv(r.Data, "Table"), deviceIP,
				snmpFile,
				sv(r.Data, "Community"), sv(r.Data, "Version"),
				sv(r.Data, "Output"), tags)

			// Write OID map file once per unique template map file
			// Format: field_name,OID  (no header — matches real gateway maps/*.csv)
			if !writtenMaps[snmpFile] {
				writtenMaps[snmpFile] = true
				if oids, ok := r.Data["_oids"].([]any); ok {
					var oidBuf bytes.Buffer
					for _, o := range oids {
						om, ok := o.(map[string]any)
						if !ok { continue }
						fmt.Fprintf(&oidBuf, "%s,%s\n",
							sv(om, "field_key"), sv(om, "oid"))
					}
					extraPath := fmt.Sprintf("configs/%s/maps/%s", svcName, snmpFile)
					extraFiles[extraPath] = oidBuf.String()
				}
			}
		}

	case "mqtt":
		// mqtt-reader uses mapping.yml YAML format
		// Group by topic
		type topicGroup struct {
			Topic    string
			Table    string
			Readings []csvEntry
		}
		topicMap := map[string]*topicGroup{}
		var topicOrder []string
		for _, r := range rows {
			topic := sv(r.Data, "Topic")
			if _, ok := topicMap[topic]; !ok {
				topicMap[topic] = &topicGroup{
					Topic: topic,
					Table: sv(r.Data, "Table"),
				}
				topicOrder = append(topicOrder, topic)
			}
			topicMap[topic].Readings = append(topicMap[topic].Readings, r)
		}
		sort.Strings(topicOrder)
		// Write mapping.yml format
		for _, topic := range topicOrder {
			grp := topicMap[topic]
			fmt.Fprintf(&b, "- Topic: %q\n", grp.Topic)
			fmt.Fprintf(&b, "  Table: %q\n", grp.Table)
			b.WriteString("  Mapping:\n")
			for _, r := range grp.Readings {
				b.WriteString("    - Device: [\"FIXED\", ")
				fmt.Fprintf(&b, "%q]\n", sv(r.Data, "Device"))
				b.WriteString("      Reading: [\"FIXED\", ")
				fmt.Fprintf(&b, "%q]\n", sv(r.Data, "Reading"))
				// JSON path extraction if specified
				if jp := sv(r.Data, "JSONPath"); jp != "" && jp != "$.value" {
					// Strip "$." prefix for mqtt-reader FIELD format
					field := strings.TrimPrefix(jp, "$.")
					b.WriteString("      Value: [\"FIELD\", ")
					fmt.Fprintf(&b, "%q]\n", field)
				} else {
					b.WriteString("      Value: [\"FIELD\", \"value\"]\n")
				}
				b.WriteString("      Output: [\"FIXED\", \"influxdb\"]\n")
				if tags := sv(r.Data, "Tags"); tags != "" {
					b.WriteString("      Tags: [\"FIXED\", ")
					fmt.Fprintf(&b, "%q]\n", tags)
				}
			}
		}

	default:
		for _, r := range rows {
			line, _ := json.Marshal(r.Data)
			b.Write(line)
			b.WriteByte('\n')
		}
	}

	return b.String(), extraFiles
}

func sv(m map[string]any, key string) string {
	if v, ok := m[key]; ok { return fmt.Sprintf("%v", v) }
	return ""
}

func strVal(v any, def string) string {
	if s, ok := v.(string); ok && s != "" { return s }
	return def
}

// ─── Docker Swarm Stack Compose Builder ──────────────────────────────────────
// Generates docker-compose.yml for `docker stack deploy -c docker-compose.yml qube`
// Matches the real Qube Lite swarm format exactly:
//   - networks: qube-net external overlay
//   - deploy: replicas + restart_policy blocks
//   - host-path volumes for config files
//   - standard logging config
//
// The conf-agent writes this file to /opt/qube/docker-compose.yml
// then runs: docker stack deploy -c /opt/qube/docker-compose.yml qube
// Resulting service names: qube_<service-name> (same as real Qube Lite)

func buildFullComposeYML(pool *pgxpool.Pool, qubeID, location string, services []svcMeta) string {
	var b bytes.Buffer
	loc := location
	if loc == "" { loc = "unset" }

	// Header — matches real Qube Lite format
	fmt.Fprintf(&b, `version: "3.8"
# Generated by Qube Enterprise TP-API
# Qube: %s | Location: %s
# Deploy: docker stack deploy -c docker-compose.yml qube

networks:
  qube-net:
    external: true

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
    environment:
      - QUBE_ID=%s
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
      - /opt/qube/sensor_map.json:/config/sensor_map.json:ro
    environment:
      - INFLUX_URL=http://influxdb:8086
      - INFLUX_DB=qube-db
      - SENSOR_MAP_PATH=/config/sensor_map.json
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

`, qubeID, loc, imageForService(pool, "conf_agent"), qubeID, imageForService(pool, "influx_sql"), qubeID)

	// One service block per gateway — each gateway = one container
	// Multiple gateways of same protocol = multiple separate containers with different names
	// e.g. two Modbus gateways → qube_panel-a-modbus and qube_panel-b-modbus
	for _, svc := range services {
		image := svc.Image
		if image == "" { image = defaultImageForProtocol(pool, svc.Protocol) }

		fmt.Fprintf(&b, "  # Gateway: %s (%s) → %s\n", svc.GwName, svc.Protocol, svc.Host)
		fmt.Fprintf(&b, "  %s:\n", svc.Name)
		fmt.Fprintf(&b, "    image: %s\n", image)
		fmt.Fprintf(&b, "    hostname: %s\n", svc.Name)
		fmt.Fprintf(&b, "    networks:\n      - qube-net\n")
		fmt.Fprintf(&b, "    volumes:\n")
		fmt.Fprintf(&b, "      - /etc/timezone:/etc/timezone:ro\n")
		fmt.Fprintf(&b, "      - /etc/localtime:/etc/localtime:ro\n")
		// configs.yml — gateway connection settings (server IP, port, poll interval)
		fmt.Fprintf(&b, "      - /opt/qube/configs/%s/configs.yml:/app/configs.yml:ro\n", svc.Name)
		// config.csv — main data file (registers/nodes/devices depending on protocol)
		fmt.Fprintf(&b, "      - /opt/qube/configs/%s/config.csv:/app/config.csv:ro\n", svc.Name)
		// SNMP: mount maps/ folder containing per-device-type OID CSV files
		if svc.Protocol == "snmp" {
			fmt.Fprintf(&b, "      - /opt/qube/configs/%s/maps:/app/maps:ro\n", svc.Name)
		}

		// SNMP gateways may have per-device OID files in the same folder
		if svc.Protocol == "snmp" {
			fmt.Fprintf(&b, "      - /opt/qube/configs/%s:/app/oids:ro\n", svc.Name)
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

// defaultImageForProtocol returns the Docker image for a gateway protocol.
// Override the registry at startup via QUBE_IMAGE_REGISTRY env var.
// Default: ghcr.io/sandun-s (change to your registry in production)
func defaultImageForProtocol(pool *pgxpool.Pool, protocol string) string {
	// Map protocol to registry_config service key
	keyMap := map[string]string{
		"modbus_tcp": "modbus",
		"opcua":      "opcua",
		"snmp":       "snmp",
		"mqtt":       "mqtt_gw",
	}
	if key, ok := keyMap[protocol]; ok {
		return imageForService(pool, key)
	}
	return "busybox:latest"
}

// imageForService resolves a Docker image from registry_config table.
// Switch with: PUT /api/v1/admin/registry {"mode":"github"} or {"mode":"gitlab"}
func imageForService(pool *pgxpool.Pool, serviceKey string) string {
	// Try DB lookup first
	rows, err := pool.Query(context.Background(),
		`SELECT key, value FROM registry_config`)
	if err == nil {
		defer rows.Close()
		cfg := map[string]string{}
		for rows.Next() {
			var k, v string; rows.Scan(&k, &v)
			cfg[k] = v
		}
		mode := cfg["mode"]
		switch mode {
		case "github":
			base := cfg["github_base"]
			if base == "" { base = "ghcr.io/sandun-s/qube-enterprise-home" }
			shortNames := map[string]string{
				"conf_agent": "conf-agent",
				"influx_sql": "influx-to-sql",
			}
			if name, ok := shortNames[serviceKey]; ok {
				return base + "/" + name + ":arm64.latest"
			}
			return base + "/" + serviceKey + ":arm64.latest"
		case "gitlab", "custom":
			imgKey := "img_" + serviceKey
			if v, ok := cfg[imgKey]; ok && v != "" { return v }
			base := cfg["gitlab_base"]
			if base == "" { base = "registry.gitlab.com/iot-team4/product" }
			prefixMap := map[string]string{
				"conf_agent": "enterprise-conf-agent",
				"influx_sql": "enterprise-influx-to-sql",
			}
			if name, ok := prefixMap[serviceKey]; ok {
				return base + "/" + name + ":arm64.latest"
			}
			return base + "/" + serviceKey + ":arm64.latest"
		}
	}
	// Fallback: env var
	reg := os.Getenv("QUBE_IMAGE_REGISTRY")
	if reg == "" { reg = "ghcr.io/sandun-s/qube-enterprise-home" }
	shortNames := map[string]string{
		"conf_agent": "conf-agent",
		"influx_sql": "influx-to-sql",
	}
	if name, ok := shortNames[serviceKey]; ok {
		return reg + "/" + name + ":arm64.latest"
	}
	return reg + "/" + serviceKey + ":arm64.latest"
}


// ─── Device Self-Registration ─────────────────────────────────────────────────
// POST /v1/device/register — called by conf-agent on startup using /boot/mit.txt credentials
// No auth required — uses device_id + register_key to identify the device.
// Returns QUBE_TOKEN if the device has been claimed by a customer, or "pending" if not yet.
//
// Flow:
//   1. Device boots, conf-agent reads /boot/mit.txt → gets device_id + register_key
//   2. Calls this endpoint
//   3a. If claimed → gets QUBE_TOKEN, saves to /opt/qube/.env, starts normal sync
//   3b. If not claimed yet → gets "pending", polls every 60s until customer claims it

func deviceRegisterHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DeviceID    string `json:"device_id"`    // from /boot/mit.txt → deviceid field
			RegisterKey string `json:"register_key"` // from /boot/mit.txt → register field
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.DeviceID == "" || req.RegisterKey == "" {
			writeError(w, http.StatusBadRequest, "device_id and register_key required")
			return
		}

		ctx := context.Background()

		// Look up the device by device_id AND register_key — both must match
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
			// Device not found — could be wrong device_id or register_key
			writeError(w, http.StatusUnauthorized, "device not found or invalid register key")
			return
		}

		if !claimed || orgID == nil {
			// Device exists but customer hasn't claimed it yet
			// Conf-agent will retry every 60s
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":     "pending",
				"device_id":  req.DeviceID,
				"message":    "Device not yet claimed. Customer must register device in the portal first.",
				"retry_secs": 60,
			})
			return
		}

		// Device is claimed — generate the QUBE_TOKEN
		// Same HMAC formula as claimQubeHandler so tokens are consistent
		authToken := computeHMAC(req.DeviceID, orgSecret)

		// Update last_seen so we know the device is online
		pool.Exec(ctx,
			`UPDATE qubes SET last_seen=NOW(), status='online' WHERE id=$1`, req.DeviceID)

		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "claimed",
			"device_id":  req.DeviceID,
			"qube_token": authToken,
			"tpapi_url":  "",  // conf-agent already knows this — it called this endpoint
			"message":    "Device claimed. Save qube_token to /opt/qube/.env and begin sync.",
		})
	}
}
