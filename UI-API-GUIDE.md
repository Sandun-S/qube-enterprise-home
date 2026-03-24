# Qube Enterprise — UI & API Guide

This document explains how the test UI (`test-ui/index.html`) works, which API calls it makes at each step, and how the underlying data model connects. Anyone reading this should understand both how to use the UI and how to call the APIs directly.

---

## Overview — Three Layers

```
Protocols table       →  Templates (devices)    →  Sensors (instances)
─────────────────────    ─────────────────────    ──────────────────────
modbus_tcp               Schneider PM5100         Main_Meter on Q-1001
  image: modbus-gateway    registers: 4 entries     unit_id: 1
  asks user: host, port    fields: power, voltage    host: 192.168.1.100
                                                     → config.csv row 1

snmp                     GXT RT UPS               UPS_Room2 on Q-1001
  image: snmp-gateway      oids: 6 entries          community: public
  asks user: host, port    fields: battery, load     host: 192.168.1.200
```

**Protocol** — defines the Docker container image and what connection fields to ask the user.  
**Template** — defines the register map / OID list / MQTT topics for one specific device type. Reusable across customers.  
**Sensor** — one instance of a template on a specific gateway. Generates one block in `config.csv`.  
**Gateway** — one running container on the Qube. Many sensors share one gateway if they use the same protocol and host.

---

## Config Tab

Set the Cloud API base URL before doing anything else.

```
UI field:  Cloud API Base URL
Default:   http://localhost:8080
```

**API called:**
```
GET /health
→ 200 {"status": "ok"}
```

No auth required. Use this to confirm the API is reachable.

---

## Customer Tab — Step by Step

### Step 1 — Login or Register

**Login:**
```
POST /api/v1/auth/login
Body: {"email": "admin@acme.com", "password": "yourpassword"}

Response:
{
  "token":   "eyJ...",     ← JWT, stored in memory, sent as Bearer token on all subsequent calls
  "org_id":  "uuid",
  "role":    "admin"
}
```

**Register new org** (first time):
```
POST /api/v1/auth/register
Body: {"org_name": "Acme Factory", "email": "admin@acme.com", "password": "yourpassword"}

Response: same as login — token + org_id + role
```

After login, the UI also calls:
```
GET /api/v1/protocols     ← public, no auth needed
```
This loads the protocol list into all dropdowns. If this endpoint fails (old DB without protocols table), the UI falls back to a hardcoded list of the 4 standard protocols.

---

### Step 2 — Claim Qube

**List existing Qubes:**
```
GET /api/v1/qubes
Authorization: Bearer <token>

Response: [
  {
    "id":             "Q-1001",
    "status":         "online",       ← updated by conf-agent heartbeat
    "location_label": "Floor A",
    "config_hash":    "abc123...",    ← changes when gateways/sensors are added
    "last_seen":      "2024-01-01T..."
  }
]
```

**Claim a new Qube:**
```
POST /api/v1/qubes/claim
Authorization: Bearer <token>
Body: {"register_key": "TEST-Q1001-REG"}

Response:
{
  "qube_id":    "Q-1001",
  "message":    "claimed",
  "auth_token": "tp-api-token..."    ← used by conf-agent on the device, not by the UI
}
```

Register keys for dev: `TEST-Q1001-REG` through `TEST-Q1020-REG`.  
Register key format in production: printed on the device box or in `/boot/mit.txt`.

After claiming, the Qube's conf-agent polls `GET /v1/sync` on the TP-API every 30 seconds and connects automatically.

---

### Step 3 — Select Device from Catalog

**Load all templates:**
```
GET /api/v1/templates
Authorization: Bearer <token>

Response: [
  {
    "id":                 "uuid",
    "name":               "Schneider PM5100",
    "protocol":           "modbus_tcp",
    "description":        "Three-phase power meter...",
    "is_global":          true,         ← global = available to all orgs (set by superadmin)
    "config_json":        {...},         ← register map / OID list / MQTT topics
    "influx_fields_json": {...}          ← display labels and units for dashboards
  }
]
```

**Filter by protocol:**
```
GET /api/v1/templates?protocol=modbus_tcp
GET /api/v1/templates?protocol=snmp
GET /api/v1/templates?protocol=opcua
GET /api/v1/templates?protocol=mqtt
```

The UI filters client-side on the search field, and uses `?protocol=` for the dropdown.

**Note on the filter bug (fixed):** The original WHERE clause was `WHERE is_global=TRUE OR org_id=$1 AND protocol=$2` — due to SQL operator precedence, `AND` bound tighter than `OR`, so the protocol filter only applied to org templates. Fixed to `WHERE (is_global=TRUE OR org_id=$1) AND protocol=$2`.

---

### Step 4 — Configure and Add to Qube

This is where the UI does the most work behind the scenes. The user only sees: device IP + sensor name. Everything else is automatic.

#### What the UI renders

When the user selects a device from the catalog, the UI calls:
```
GET /api/v1/protocols      ← already loaded at login, no extra call
```

It reads `connection_params_schema` from the matching protocol to know what connection fields to show:

```json
// Modbus TCP protocol schema — what gets rendered as form fields:
"connection_params_schema": [
  {"key":"host",  "label":"Device IP address", "type":"text",   "required":true, "placeholder":"192.168.1.100"},
  {"key":"port",  "label":"Modbus port",        "type":"number", "default":502,   "required":true}
]

// SNMP protocol schema:
"connection_params_schema": [
  {"key":"host",      "label":"Device IP address", "type":"text",   "required":true},
  {"key":"port",      "label":"SNMP port",          "type":"number", "default":161},
]

// MQTT protocol schema (includes credentials):
"connection_params_schema": [
  {"key":"host",      "label":"Broker URL",  "type":"text",   "required":true, "placeholder":"tcp://..."},
  {"key":"port",      "label":"Port",        "type":"number", "default":1883},
  {"key":"base_topic","label":"Base topic",  "type":"text",   "placeholder":"factory/floor2"},
  {"key":"username",  "label":"Username",    "type":"text"},
  {"key":"password",  "label":"Password",    "type":"text"},
  {"key":"client_id", "label":"Client ID",   "type":"text"}
]
```

It also reads `addr_params_schema` to pre-fill the address params textarea:

```json
// Modbus:
"addr_params_schema": [
  {"key":"unit_id",         "default":1,  "hint":"1-247, slave address on the device"},
  {"key":"register_offset", "default":0,  "hint":"Usually 0. Shift all addresses by this amount."}
]

// SNMP:
"addr_params_schema": [
  {"key":"community", "default":"public"},
  {"key":"version",   "type":"select", "options":["2c","1","3"], "default":"2c"}
]

// MQTT:
"addr_params_schema": [
  {"key":"topic_suffix", "placeholder":"sensor_01", "hint":"Full topic = base_topic/topic_suffix"}
]
```

This schema-driven approach means adding a new protocol (e.g. LoRaWAN) requires only inserting a row into the `protocols` table — the UI automatically renders the correct fields.

#### Gateway auto-assign (key concept)

When the user enters the device IP and clicks "Add Sensor to Qube", the UI does NOT ask the user about gateways. Instead:

**Step A — load existing gateways:**
```
GET /api/v1/qubes/{qube_id}/gateways
Authorization: Bearer <token>

Response: [
  {
    "id":           "uuid",
    "name":         "Schneider_PM5100_GW",
    "protocol":     "modbus_tcp",
    "host":         "192.168.1.100",
    "port":         502,
    "sensor_count": 3
  }
]
```

**Step B — match by protocol + host:**
```javascript
const existing = qubeGateways.find(g =>
  g.protocol === selectedTemplate.protocol &&
  g.host === enteredHost
);
```

**Step C — reuse or create:**

If a match is found → use its `gateway_id` directly. No gateway API call.

If no match → create one automatically:
```
POST /api/v1/qubes/{qube_id}/gateways
Authorization: Bearer <token>
Body: {
  "name":       "Schneider_PM5100_GW",   ← auto-generated from device name
  "protocol":   "modbus_tcp",
  "host":       "192.168.1.100",
  "port":       502,
  "config_json": {                        ← extra connection fields from schema
    "host": "192.168.1.100",
    "port": 502
  }
}

Response:
{
  "gateway_id":    "uuid",
  "service_name":  "schneider_pm5100_gw",   ← becomes the Docker service name
  "new_hash":      "abc123..."               ← config hash updated, conf-agent will sync
}
```

This auto-assign logic is **protocol-agnostic**. It works the same for Modbus, SNMP, OPC-UA, MQTT, and any future protocol added to the `protocols` table. No code changes needed.

**Step D — add the sensor:**
```
POST /api/v1/gateways/{gateway_id}/sensors
Authorization: Bearer <token>
Body: {
  "name":           "Main_Meter",
  "template_id":    "uuid-of-schneider-pm5100",
  "address_params": {"unit_id": 1, "register_offset": 0},
  "tags_json":      {"location": "Floor A"}
}

Response:
{
  "sensor_id":  "uuid",             ← save this for querying telemetry data
  "csv_rows":   4,                  ← number of register rows added to config.csv
  "new_hash":   "def456..."         ← updated hash, conf-agent detects change and syncs
}
```

**What happens on the Qube after this:**

1. Conf-agent polls `GET /v1/sync` every 30s
2. Sees `config_hash` has changed
3. Downloads new `docker-compose.yml` + `config.csv` from TP-API
4. Runs `docker stack deploy` — gateway container starts (or updates its CSV if already running)
5. Gateway container begins polling the device
6. Data flows: device → gateway → core-switch → InfluxDB → enterprise-influx-to-sql → TP-API → Postgres

---

### Step 5 — Done

The done screen shows the sensor UUID. Save it — you need it to query telemetry data.

**Query latest values:**
```
GET /api/v1/data/sensors/{sensor_id}/latest
Authorization: Bearer <token>

Response:
{
  "sensor_id": "uuid",
  "fields": [
    {"field_key": "active_power_w", "value": "4250.5", "unit": "W",   "recorded_at": "2024-01-01T12:00:00Z"},
    {"field_key": "voltage_ll_v",   "value": "399.2",  "unit": "V",   "recorded_at": "2024-01-01T12:00:00Z"},
    {"field_key": "current_a",      "value": "6.1",    "unit": "A",   "recorded_at": "2024-01-01T12:00:00Z"},
    {"field_key": "energy_kwh",     "value": "1204.0", "unit": "kWh", "recorded_at": "2024-01-01T12:00:00Z"}
  ]
}
```

**Query historical readings:**
```
GET /api/v1/data/readings?sensor_id={uuid}&field=active_power_w&from=2024-01-01T00:00:00Z
Authorization: Bearer <token>

Response: [
  {"sensor_id":"uuid","field_key":"active_power_w","value":"4100.0","recorded_at":"..."},
  ...
]
```

**Note on test data:** The dev stack seeder inserts fake readings into InfluxDB. `enterprise-influx-to-sql` then forwards them to TP-API on a 60s cycle. You need the full dev stack running and wait about 2 minutes after adding a sensor before data appears.

---

## MQTT Gateway — Multi-Sensor Explained

One MQTT gateway container handles many sensors. Each sensor subscribes to a different topic.

**Example setup:**
- Gateway: broker at `tcp://192.168.1.10:1883`, base_topic = `factory/sensors`
- Sensor A: topic_suffix = `temp_01` → subscribes to `factory/sensors/temp_01`
- Sensor B: topic_suffix = `temp_02` → subscribes to `factory/sensors/temp_02`
- Sensor C: topic_suffix = `humidity_01` → subscribes to `factory/sensors/humidity_01`

All three sensors use one `mqtt-gateway` container. The `mapping.yml` config file groups them:

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
      ...
```

**With credentials:**
```
POST /api/v1/qubes/{id}/gateways
Body: {
  "name":     "MQTT_Factory_GW",
  "protocol": "mqtt",
  "host":     "tcp://192.168.1.10:1883",
  "port":     1883,
  "config_json": {
    "host":       "tcp://192.168.1.10:1883",
    "port":       1883,
    "base_topic": "factory/sensors",
    "username":   "mqttuser",
    "password":   "secret123",
    "client_id":  "qube-q1001-gw"
  }
}
```

These flow directly into `configs.yml` on the Qube:
```yaml
MQTT:
  Host: "tcp://192.168.1.10:1883"
  Port: 1883
  User: "mqttuser"
  Pass: "secret123"
```

---

## Superadmin Tab

### Login
```
POST /api/v1/auth/login
Body: {"email": "iotteam@internal.local", "password": "iotteam2024"}
→ role: "superadmin"
```

Superadmin can create and edit global templates (visible to all orgs). Admins can only create org-specific templates.

### Browse and filter templates
```
GET /api/v1/templates                      ← all protocols
GET /api/v1/templates?protocol=modbus_tcp  ← filter by protocol
```

### Create a new template
```
POST /api/v1/templates
Authorization: Bearer <superadmin_token>
Body: {
  "name":        "Schneider PM5100",
  "protocol":    "modbus_tcp",
  "description": "Three-phase power meter",
  "config_json": {
    "registers": [
      {"address":3000, "register_type":"Holding", "data_type":"float32",
       "count":2, "scale":1.0, "field_key":"active_power_w", "unit":"W"},
      {"address":3028, "register_type":"Holding", "data_type":"float32",
       "count":2, "scale":1.0, "field_key":"voltage_ll_v", "unit":"V"}
    ]
  },
  "influx_fields_json": {
    "active_power_w": {"display_label": "Active Power", "unit": "W"},
    "voltage_ll_v":   {"display_label": "Voltage L-L",  "unit": "V"}
  }
}

Response: {"id": "uuid", "name": "Schneider PM5100", ...}
```

### Edit a template (full replace)
```
PUT /api/v1/templates/{id}
Authorization: Bearer <superadmin_token>
Body: { ...same as create... }
```

### Patch individual registers (safer for live templates)
```
PATCH /api/v1/templates/{id}/registers
Authorization: Bearer <superadmin_token>

# Add a register:
Body: {
  "action": "add",
  "entry": {
    "address":3054, "register_type":"Holding", "data_type":"float32",
    "count":2, "scale":1.0, "field_key":"current_a", "unit":"A"
  }
}

# Delete by index:
Body: {"action": "delete", "index": 2}

Response: {"total_entries": 3}
```

Use PATCH when editing a template that's already deployed on live Qubes — it's surgical and won't accidentally wipe all registers if someone made a JSON typo.

### Preview what config.csv will look like
```
GET /api/v1/templates/{id}/preview
Authorization: Bearer <superadmin_token>
→ optional: ?address_params={"unit_id":1}

Response:
{
  "protocol":  "modbus_tcp",
  "csv_type":  "modbus",
  "row_count": 4,
  "rows": [
    {"Equipment":"sensor-id","Reading":"active_power_w","RegType":"Holding","Address":3000,...},
    ...
  ]
}
```

The UI formats these into a human-readable CSV preview. This is exactly what gets written to `/opt/qube/configs/{gateway_name}/config.csv` on the Qube.

### Delete a template
```
DELETE /api/v1/templates/{id}
Authorization: Bearer <superadmin_token>
→ Only org templates can be deleted (global templates are protected)
```

---

## Protocols — How to Add a New One

The `protocols` table drives everything. Adding a new protocol requires no code changes.

### See current protocols
```
GET /api/v1/protocols    ← public, no auth
Response: [
  {
    "id":           "modbus_tcp",
    "label":        "Modbus TCP",
    "image_name":   "modbus-gateway",      ← Docker image suffix
    "default_port": 502,
    "description":  "Industrial PLCs...",
    "connection_params_schema": [...],     ← what fields to show when adding a gateway
    "addr_params_schema":       [...]      ← what fields to show when adding a sensor
  }
]
```

### Add a new protocol (e.g. LoRaWAN)

1. Run this SQL on your Postgres instance:
```sql
INSERT INTO protocols (id, label, image_name, default_port, description,
                       connection_params_schema, addr_params_schema)
VALUES (
  'lorawan_4g',
  'LoRaWAN 4G',
  'lorawan-gateway',
  9080,
  'LoRaWAN gateway via 4G TCP — temperature and humidity sensors',
  '[
    {"key":"host","label":"Gateway IP","type":"text","required":true},
    {"key":"port","label":"TCP port","type":"number","default":9080}
  ]'::jsonb,
  '[
    {"key":"imei","label":"Gateway IMEI","type":"text","required":true,
     "hint":"IMEI printed on the 4G gateway device"},
    {"key":"sensor_id","label":"Sensor ID (hex)","type":"text","required":true,
     "hint":"Sensor hardware ID from the gateway packet data"}
  ]'::jsonb
);
```

2. Build and push the container:
```bash
docker build -t ghcr.io/your-org/lorawan-gateway:arm64.latest .
docker push ghcr.io/your-org/lorawan-gateway:arm64.latest
```

3. Update `QUBE_IMAGE_REGISTRY` env var if using a different registry.

4. Refresh the UI — `GET /api/v1/protocols` returns the new protocol, dropdowns update automatically, connection fields are rendered from the schema.

The gateway auto-assign logic in the customer UI is protocol-agnostic — it works identically for LoRaWAN as it does for Modbus. No UI changes needed.

---

## User Management APIs

These are used by admins to invite teammates. Org is auto-assigned from the calling admin's JWT — you cannot add users to a different org by mistake.

```
GET  /api/v1/users/me           ← any role: get your own profile
GET  /api/v1/users              ← admin+: list all users in your org
POST /api/v1/users              ← admin+: invite a new user
PATCH /api/v1/users/{id}        ← admin+: change someone's role
DELETE /api/v1/users/{id}       ← admin+: remove from org
```

**Invite a user (password optional):**
```
POST /api/v1/users
Authorization: Bearer <admin_token>
Body: {
  "email":    "engineer@acme.com",
  "role":     "editor",
  "password": "optional-custom-password"
}

Response (no password given):
{
  "user_id":          "uuid",
  "email":            "engineer@acme.com",
  "role":             "editor",
  "org_id":           "uuid",          ← auto-assigned from admin's JWT
  "is_temp_password": true,
  "temp_password":    "Qube@2024"      ← share this with the new user
}

Response (password given):
{
  "user_id":          "uuid",
  "email":            "engineer@acme.com",
  "role":             "editor",
  "org_id":           "uuid",
  "is_temp_password": false            ← no temp_password field in response
}
```

**Role permissions:**

| Action | viewer | editor | admin | superadmin |
|--------|--------|--------|-------|------------|
| Read Qubes, sensors, data | ✓ | ✓ | ✓ | ✓ |
| Add gateways, sensors, send commands | ✗ | ✓ | ✓ | ✓ |
| Claim Qubes, manage users | ✗ | ✗ | ✓ | ✓ |
| Create/edit global templates | ✗ | ✗ | ✗ | ✓ |
| Edit registry config | ✗ | ✗ | ✗ | ✓ |

---

## Image Registry Config (superadmin only)

Controls where Docker images are pulled from on the Qube.

```
GET /api/v1/admin/registry
PUT /api/v1/admin/registry

# GitHub single-repo mode (dev/testing):
Body: {
  "mode":        "github",
  "github_base": "ghcr.io/sandun-s/qube-enterprise-home"
}
# → image: ghcr.io/sandun-s/qube-enterprise-home/modbus-gateway:arm64.latest

# GitLab separate-repo mode (production):
Body: {
  "mode":         "gitlab",
  "gitlab_base":  "registry.gitlab.com/iot-team4/product"
}
# → image: registry.gitlab.com/iot-team4/product/modbus-gateway:arm64.latest

# Custom per-image overrides:
Body: {
  "mode": "github",
  "github_base": "ghcr.io/...",
  "img_conf_agent":  "custom-registry.io/conf-agent:v2.1",
  "img_modbus":      "custom-registry.io/modbus-gateway:v1.5"
}
```

---

## TP-API — Device-Facing Endpoints

These are called by the conf-agent and gateway containers on the Qube, not by the UI. They use HMAC authentication, not JWT.

```
GET  /v1/sync              ← conf-agent polls this to check config_hash
GET  /v1/config            ← download docker-compose.yml + all config files
POST /v1/heartbeat         ← conf-agent reports status every 30s
GET  /v1/commands/poll     ← check for pending commands
POST /v1/commands/{id}/ack ← acknowledge command execution
POST /v1/telemetry/ingest  ← enterprise-influx-to-sql posts readings here
POST /v1/device/register   ← self-registration on first boot
```

---

## Data Flow Summary

```
1. Admin adds sensor in UI
   → POST /api/v1/gateways/{id}/sensors
   → config_hash changes in DB

2. Conf-agent on Qube polls /v1/sync (every 30s)
   → sees hash mismatch
   → GET /v1/config → downloads docker-compose.yml + config.csv
   → docker stack deploy → gateway container starts/updates

3. Gateway container polls physical device (Modbus/SNMP/OPC-UA/MQTT)
   → sends data to core-switch:8585/v3/data
   → core-switch routes to InfluxDB

4. enterprise-influx-to-sql runs every 60s
   → reads InfluxDB measurements
   → maps sensor_id via sensor_map.json
   → POST /v1/telemetry/ingest to TP-API
   → TP-API writes to sensor_readings table in Postgres

5. UI queries latest data
   → GET /api/v1/data/sensors/{id}/latest
   → reads from sensor_readings table
```

Total latency from sensor add to first data point: ~2 minutes on a live system (30s conf-agent sync + container start + 60s influx relay cycle).
