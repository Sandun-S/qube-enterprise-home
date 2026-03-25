# Adding New Protocols to Qube Enterprise

This guide covers everything needed to add a new gateway protocol (e.g. LoRaWAN, Wialon, Teltonica). Follow every step in order. Each step is labelled with what it changes and whether the API handles it automatically or requires manual code/SQL changes.

---

## How Protocols Work — The Full Chain

```
protocols table (DB)
  ↓ defines: container image, connection fields to ask user, sensor address fields
  ↓
gateways table (DB)
  ↓ one row per running container on a Qube
  ↓
service_csv_rows table (DB)
  ↓ one row per register/node/OID/topic per sensor
  ↓
TP-API sync.go → renderGatewayConfig()   → configs.yml  (written to Qube)
              → renderGatewayFiles()    → config.csv   (written to Qube)
              → buildFullComposeYML()   → docker-compose.yml
              → conf-agent deploys it
```

When you add a new protocol, you touch each layer of this chain.

---

## Step 1 — Build the Gateway Container (no code changes)

Build a Go/Python service that:
- Reads `configs.yml` at startup for connection settings
- Reads `config.csv` (or equivalent) for what to poll/subscribe
- POSTs data to `http://core-switch:8585/v3/batch`
- POSTs alerts to `http://core-switch:8585/v3/alerts`

Mount points the Enterprise system will provide:
- `/app/configs.yml` — connection config (host, port, credentials)
- `/app/config.csv` — device/sensor map (what to read)
- `/app/maps/` — optional subfolder for extra map files (e.g. SNMP OID maps)

Push the image to your registry:
```bash
docker buildx build --platform linux/arm64 -t ghcr.io/your-org/lorawan-gateway:arm64.latest .
docker push ghcr.io/your-org/lorawan-gateway:arm64.latest
```

---

## Step 2 — Insert into `protocols` table (SQL, no API yet)

This drives the UI dropdowns and schema-based field rendering. No code change needed — just a DB INSERT.

```sql
INSERT INTO protocols (
    id, label, image_name, default_port, description,
    connection_params_schema, addr_params_schema
) VALUES (
    'lorawan_4g',
    'LoRaWAN 4G',
    'lorawan-gateway',    -- Docker image name suffix (registry prefix added at runtime)
    9080,
    'LoRaWAN 4G TCP gateway — temperature and humidity sensors via binary packet decode',

    -- connection_params_schema: fields shown when customer adds a GATEWAY
    -- type: text | number | select
    '[
      {"key":"host",  "label":"Gateway IP",  "type":"text",   "required":true,
       "placeholder":"192.168.1.50", "hint":"IP of the 4G gateway device"},
      {"key":"port",  "label":"TCP port",    "type":"number", "default":9080, "required":true}
    ]'::jsonb,

    -- addr_params_schema: fields shown when customer adds a SENSOR to this gateway
    '[
      {"key":"imei",      "label":"Gateway IMEI", "type":"text", "required":true,
       "hint":"15-digit IMEI printed on the 4G gateway"},
      {"key":"sensor_id", "label":"Sensor ID (hex)", "type":"text", "required":true,
       "hint":"Hex sensor ID from the gateway binary packet data (e.g. 824913)"}
    ]'::jsonb
);
```

After this INSERT, the UI immediately shows LoRaWAN 4G in all protocol dropdowns. No rebuild needed.

**Field types for connection_params_schema / addr_params_schema:**

| type | Renders as | Extra fields |
|------|-----------|--------------|
| `text` | `<input type="text">` | `placeholder`, `hint` |
| `number` | `<input type="number">` | `default` |
| `select` | `<select>` | `options: ["opt1","opt2"]`, `default` |
| `password` | `<input type="password">` (future) | `hint` |

---

## Step 3 — Add template(s) for device types on this protocol

Templates define what data to collect (register map / OID list / packet fields).

```sql
INSERT INTO sensor_templates (name, protocol, description, is_global, config_json, influx_fields_json)
VALUES (
  'Generic LoRaWAN Temperature Sensor',
  'lorawan_4g',
  'LoRaWAN temperature sensor — decoded from binary 4G packet',
  TRUE,
  '{
    "packet_format": "tz_4g_v1",
    "sensors": [
      {"field_key": "temperature_c",  "json_path": "$.temperature", "unit": "C"},
      {"field_key": "humidity_pct",   "json_path": "$.humidity",    "unit": "%"},
      {"field_key": "battery_v",      "json_path": "$.battery",     "unit": "V"}
    ]
  }',
  '{
    "temperature_c": {"display_label": "Temperature", "unit": "°C"},
    "humidity_pct":  {"display_label": "Humidity",    "unit": "%"},
    "battery_v":     {"display_label": "Battery",     "unit": "V"}
  }'
);
```

The structure of `config_json` is completely up to you — it just needs to match what you write in `generateCSVRows` (Step 5).

---

## Step 4 — Add `configs.yml` generator in `cloud/internal/tpapi/sync.go`

File: `cloud/internal/tpapi/sync.go`
Function: `renderGatewayConfig(svc svcMeta) string`

Add a new `case` for your protocol:

```go
case "lorawan_4g":
    // Read any connection fields from svc.GwCfgJSON (comes from gateway's config_json)
    port := 9080
    if v, ok := svc.GwCfgJSON["port"].(float64); ok { port = int(v) }
    fmt.Fprintf(&b, `loglevel: "INFO"
lorawan:
  host: "%s"
  port: %d
  packet_format: "tz_4g_v1"
  data_file: "config.csv"
http:
  data_url: "http://core-switch:8585/v3/batch"
  alerts_url: "http://core-switch:8585/v3/alerts"
`, svc.Host, port)
```

`svc` fields available:
- `svc.Host` — gateway host/IP from gateways table
- `svc.GwPort` — gateway port
- `svc.GwCfgJSON` — full config_json map from gateways table (all connection fields)
- `svc.Name` — gateway service name (used for file paths)
- `svc.Protocol` — protocol ID

---

## Step 5 — Add CSV row generator in `cloud/internal/api/sensors.go`

File: `cloud/internal/api/sensors.go`
Function: `generateCSVRows(...) ([]map[string]any, string, error)`

Add a new `case` that converts your template's `config_json` + sensor's `address_params` into CSV rows:

```go
case "lorawan_4g":
    sensors, _ := tmplCfg["sensors"].([]any)
    imei        := strVal(ap["imei"], "")
    sensorID    := strVal(ap["sensor_id"], "")

    rows := make([]map[string]any, 0, len(sensors))
    for _, s := range sensors {
        sm, ok := s.(map[string]any)
        if !ok { continue }
        rows = append(rows, map[string]any{
            "IMEI":      imei,
            "SensorID":  sensorID,
            "Reading":   strVal(sm["field_key"], "value"),
            "JSONPath":  strVal(sm["json_path"], "$.value"),
            "Unit":      strVal(sm["unit"], ""),
            "Output":    "influxdb",
            "Table":     "Measurements",
            "Tags":      "name=" + sensorName,
        })
    }
    return rows, "lorawan_sensors", nil
```

The second return value (`"lorawan_sensors"`) is the `csv_type` stored in `service_csv_rows`. It's used by `renderGatewayFiles` to know which format to write.

---

## Step 6 — Add CSV file writer in `cloud/internal/tpapi/sync.go`

File: `cloud/internal/tpapi/sync.go`
Function: `renderGatewayFiles(protocol, svcName string, rows []csvEntry) (string, map[string]string)`

Add a new `case` that writes the actual CSV/config file content:

```go
case "lorawan_4g":
    // Write config.csv for LoRaWAN gateway
    // Format defined by your gateway binary — match it exactly
    b.WriteString("#IMEI,SensorID,Reading,JSONPath,Output,Table,Tags\n")
    for _, r := range rows {
        fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n",
            sv(r.Data, "IMEI"),
            sv(r.Data, "SensorID"),
            sv(r.Data, "Reading"),
            sv(r.Data, "JSONPath"),
            sv(r.Data, "Output"),
            sv(r.Data, "Table"),
            sv(r.Data, "Tags"))
    }
    // If you need extra files (like SNMP uses maps/ folder):
    // extraFiles["configs/"+svcName+"/extra.yml"] = content
```

---

## Step 7 — Mount extra files in docker-compose (if needed)

File: `cloud/internal/tpapi/sync.go`
Function: `buildFullComposeYML(...)`

Find the volumes section of the per-gateway loop (around line 500):

```go
fmt.Fprintf(&b, "      - /opt/qube/configs/%s/config.csv:/app/config.csv:ro\n", svc.Name)
// Add extra mounts for your protocol:
if svc.Protocol == "lorawan_4g" {
    fmt.Fprintf(&b, "      - /opt/qube/configs/%s/decode_keys.json:/app/decode_keys.json:ro\n", svc.Name)
}
```

---

## Step 8 — Add to test suite `test/test_api.sh` (optional but recommended)

Find section 14 (Gateways — all 4 protocols) and add:

```bash
# LoRaWAN gateway
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"lorawan_gw","protocol":"lorawan_4g","host":"192.168.1.50","port":9080}' "$TOKEN")
assert_status "create lorawan gateway" "201" "$(code "$R")"
LORAWAN_GW_ID=$(split "$R" | jq -r .gateway_id)
assert_field "lorawan gateway_id" "$LORAWAN_GW_ID"
```

And add sensor test if you have a template in migration:
```bash
LORAWAN_TMPL=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=lorawan_4g" | jq -r '.[0].id')

R=$(api POST "/api/v1/gateways/$LORAWAN_GW_ID/sensors" \
  "{\"name\":\"Temp_Sensor_01\",\"template_id\":\"$LORAWAN_TMPL\",
    \"address_params\":{\"imei\":\"0868822046344121\",\"sensor_id\":\"824913\"}}" \
  "$TOKEN")
assert_status "create lorawan sensor" "201" "$(code "$R")"
```

---

## Step 9 — Update TESTING.md / UI-API-GUIDE.md

Add a section to `UI-API-GUIDE.md` under "Protocols — How to Add a New One" documenting:
- What `config_json` looks like for your template
- What `addr_params` the user fills in
- What `configs.yml` gets generated
- What `config.csv` gets generated

---

## Summary — What Changes Where

| What | File | How |
|------|------|-----|
| Protocol registration | `002_gateways_sensors.sql` (or SQL INSERT on live DB) | SQL only, no code |
| Device templates | `003_device_catalog.sql` (or `POST /api/v1/templates`) | SQL or API |
| `configs.yml` generator | `cloud/internal/tpapi/sync.go` → `renderGatewayConfig()` | Add `case` |
| CSV row builder | `cloud/internal/api/sensors.go` → `generateCSVRows()` | Add `case` |
| CSV file writer | `cloud/internal/tpapi/sync.go` → `renderGatewayFiles()` | Add `case` |
| Extra volume mounts | `cloud/internal/tpapi/sync.go` → `buildFullComposeYML()` | Add `if svc.Protocol ==` |
| Test suite | `test/test_api.sh` | Add assertions |

**What does NOT need to change:**
- conf-agent — it just deploys whatever docker-compose.yml it receives
- TP-API router, auth, JWT — protocol-agnostic
- Cloud API gateways.go / sensors.go CRUD — validates protocol via FK to protocols table
- Test UI — renders fields from `connection_params_schema` and `addr_params_schema` automatically

---

## Real Format Reference

### Modbus TCP

**`configs.yml`:**
```yaml
LogLevel: "Info"
Modbus:
  Server: "modbus-tcp://{host}:{port}"
  ReadingsFile: "config.csv"
  FreqSec: 5
  SingleReadCount: 100
HTTP:
  Data: "http://core-switch:8585/v3/batch"
  Alerts: "http://core-switch:8585/v3/alerts"
```

**`config.csv`:**
```
#Equipment,Reading,RegType,Address,type,Output,Table,Tags
Main_Meter,active_power_w,Holding,3000,float32,influxdb,Measurements,name=Main_Meter
Main_Meter,voltage_ll_v,Holding,3028,float32,influxdb,Measurements,name=Main_Meter
Sub_Meter,active_power_w,Holding,3000,float32,influxdb,Measurements,name=Sub_Meter
```
Multiple sensors → multiple rows in same file, different Equipment name.

---

### OPC-UA

**`configs.yml`:**
```yaml
LogLevel: "Info"
OpcUA:
  OpcEndPoint: "opc.tcp://{host}:{port}/path/to/server"
  PointsFile: "config.csv"
HTTP:
  Data: "http://core-switch:8585/v3/batch"
  Alerts: "http://core-switch:8585/v3/alerts"
```

**`config.csv`:**
```
#Table,Device,Reading,OpcNode,Type,Freq,Output,Tags
Measurements,Sensor_A,active_power_w,ns=2;points/ActivePower,float,10,influxdb,name=Sensor_A
Measurements,Sensor_A,voltage_v,ns=2;points/Voltage,float,10,influxdb,name=Sensor_A
Measurements,Sensor_B,active_power_w,ns=2;points/PM2_Power,float,10,influxdb,name=Sensor_B
```
Multiple sensors → multiple rows, different Device name.

**Note on OPC-UA endpoint:** The `host` field in the gateway stores the full endpoint URL including path (e.g. `opc.tcp://192.168.1.18:52520/OPCUA/N4OpcUaServer`). When adding an OPC-UA gateway, enter the full endpoint URL in the "OPC-UA endpoint URL" field.

---

### SNMP

**`configs.yml`:**
```yaml
loglevel: "INFO"
snmp:
  fetch_interval: 15
  connect_timeout: 10
  worker_count: 2
  devices_file: "config.csv"
  maps_folder: "./maps"
http:
  data_url: "http://core-switch:8585/v3/batch"
  alerts_url: "http://core-switch:8585/v3/alerts"
```

**`config.csv` (devices.csv):**
```
#Table,Device,SNMP_csv,Community,Version,Output,Tags
snmp_data,192.168.1.200,gxt-rt-ups.csv,public,2c,influxdb,name=UPS_Room1
snmp_data,192.168.1.201,apc-ups.csv,public,2c,influxdb,name=UPS_Room2
```
- `Device` = IP address of the SNMP device (from `addr_params.device_ip`)
- `SNMP_csv` = map filename in `maps/` folder (template-based, one file per device type)

**`maps/gxt-rt-ups.csv`:**
```
upsBatteryStatus,1.3.6.1.2.1.33.1.2.1.0
upsEstimatedMinutesRemaining,1.3.6.1.2.1.33.1.2.3.0
upsEstimatedChargeRemaining,1.3.6.1.2.1.33.1.2.4.0
upsInputVoltage,1.3.6.1.2.1.33.1.3.3.1.3.1
upsOutputVoltage,1.3.6.1.2.1.33.1.4.4.1.2.1
upsOutputPercentLoad,1.3.6.1.2.1.33.1.4.4.1.5.1
```
Format: `field_key,OID` — exactly 2 columns, NO header line, no type column.

**Volume mounts in docker-compose:**
```yaml
volumes:
  - /opt/qube/configs/SNMP_GW/configs.yml:/app/configs.yml:ro
  - /opt/qube/configs/SNMP_GW/config.csv:/app/config.csv:ro
  - /opt/qube/configs/SNMP_GW/maps:/app/maps:ro   ← SNMP-specific
```

**Key difference from other protocols:**
- One SNMP container handles ALL SNMP devices on the Qube (regardless of device IP)
- Auto-assign matches by `protocol` only (not protocol+host)
- Each sensor specifies its own device IP in `addr_params.device_ip`
- The OID map (`maps/` file) is shared between sensors of the same template type

---

### MQTT

**`configs.yml`:**
```yaml
LogLevel: "Info"
MappingFile: "config.csv"
MQTT:
  Host: "tcp://{broker_url}:{port}"
  Port: 1883
  User: "{username}"
  Pass: "{password}"
HTTP:
  Data: "http://core-switch:8585/v3/data"
  Alerts: "http://core-switch:8585/v3/alerts"
  IdleTO: 5
  MaxIdle: 3
```

**`config.csv` (mapping.yml YAML format):**
```yaml
- Topic: "factory/sensors/temp_01"
  Table: "Measurements"
  Mapping:
    - Device: ["FIXED", "Sensor_A"]
      Reading: ["FIXED", "temperature_c"]
      Value: ["FIELD", "temperature"]
      Output: ["FIXED", "influxdb"]

- Topic: "factory/sensors/temp_02"
  Table: "Measurements"
  Mapping:
    - Device: ["FIXED", "Sensor_B"]
      Reading: ["FIXED", "temperature_c"]
      Value: ["FIELD", "temperature"]
      Output: ["FIXED", "influxdb"]
```
Multiple sensors → multiple topic blocks in same file.

---

## Protocol Properties Reference

| Property | modbus_tcp | opcua | snmp | mqtt |
|----------|-----------|-------|------|------|
| Gateway matches by | protocol + host | protocol + host | protocol only | protocol + host |
| Config file name | `config.csv` | `config.csv` | `config.csv` | `config.csv` |
| Real gateway file name | `registers.csv` | `nodes.csv` | `devices.csv` | `mapping.yml` |
| Extra files | none | none | `maps/*.csv` | none |
| Sensors per container | many (same host) | many (same server) | all SNMP devices | many (same broker) |
| Device IP at | gateway level | gateway level | sensor addr_params | broker URL at gateway |
| Credentials | none | none | community (per sensor) | username/password at gateway |
