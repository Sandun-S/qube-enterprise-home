# Qube Enterprise — Deep Feature Plan

---

## Core Principles

1. **Zero-touch Qube** — After initial claim, the Qube is never manually configured again. Everything flows from Cloud → TP-API → Conf-Agent.
2. **Cloud is source of truth** — All state lives in Cloud Postgres. The Qube is a mirror that executes.
3. **API-first** — Every action the UI performs must be achievable via the same API (no backdoors, no SSH, no manual file edits).
4. **Hash-driven sync** — Conf-Agent only acts on change. No change = no redeploy. Avoids unnecessary container churn.
5. **Template-driven sensors** — No free-form CSV editing by users. Templates encode all protocol knowledge.

---

## Module 1: Data Model (Cloud Postgres)

### `organisations`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| name | text | |
| mqtt_namespace | text | e.g. `mitesp/secondfloor` |
| created_at | timestamptz | |

### `users`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| org_id | uuid FK → organisations | |
| email | text | unique |
| role | enum | `admin`, `editor`, `viewer` |
| password_hash | text | |
| created_at | timestamptz | |

### `qubes`
| Column | Type | Notes |
|---|---|---|
| id | text PK | e.g. `Q-1302`, derived from MAC |
| org_id | uuid FK → organisations | null = unclaimed |
| claimed_at | timestamptz | |
| auth_token_hash | text | HMAC(qube_id + org_secret) |
| last_seen | timestamptz | updated by heartbeat |
| status | enum | `online`, `offline`, `unclaimed` |
| location_label | text | e.g. "Second Floor Server Room" |

### `gateways`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| qube_id | text FK → qubes | |
| name | text | user-defined label |
| protocol | enum | `modbus_tcp`, `mqtt`, `opcua` |
| host | text | IP or broker URL |
| port | int | default by protocol |
| config_json | jsonb | protocol-specific (see Module 5) |
| service_image | text | Docker image for this gateway |
| status | enum | `active`, `disabled` |
| created_at | timestamptz | |

### `sensors`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| gateway_id | uuid FK → gateways | |
| name | text | e.g. `UPS_Main` |
| template_id | uuid FK → sensor_templates | |
| address_params | jsonb | unit_id (Modbus), topic suffix (MQTT), node_id (OPC-UA) |
| tags_json | jsonb | e.g. `{"name":"MainUPS","location":"rack1"}` |
| status | enum | `active`, `disabled` |
| created_at | timestamptz | |

### `sensor_templates`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| org_id | uuid nullable | null = global/shared template |
| name | text | e.g. `Schneider PM5100` |
| protocol | enum | `modbus_tcp`, `mqtt`, `opcua` |
| description | text | |
| config_json | jsonb | Register map / OID map / topic variable paths |
| ui_mapping_json | jsonb | Grafana panel definitions |
| influx_fields_json | jsonb | field_key → {unit, display_label, data_type} |
| is_global | bool | visible to all orgs |
| created_at | timestamptz | |

### `services`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| qube_id | text FK → qubes | |
| name | text | Docker service name (unique per Qube) |
| image | text | Docker image + tag |
| port | int | Host port mapping |
| env_json | jsonb | Environment variables |
| gateway_id | uuid nullable FK → gateways | link to gateway if gateway-type service |
| status | enum | `active`, `disabled` |
| created_at | timestamptz | |

### `service_csv_rows`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| service_id | uuid FK → services | |
| sensor_id | uuid nullable FK → sensors | which sensor generated this row |
| csv_type | enum | `registers`, `devices`, `topics` |
| row_data | jsonb | column values as key-value map |
| row_order | int | for stable CSV ordering |

### `config_state`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| qube_id | text FK → qubes | |
| hash | text | SHA-256 of full config payload |
| generated_at | timestamptz | |
| config_snapshot | jsonb | the full config at hash time (cached) |

### `qube_commands`
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| qube_id | text FK → qubes | |
| command | enum | `ping`, `restart_qube`, `restart_service`, `reload_config`, `get_logs` |
| payload | jsonb | e.g. `{"service":"modbus-gw"}` for restart_service |
| status | enum | `pending`, `executed`, `failed`, `timeout` |
| result | jsonb | execution result / stdout |
| created_at | timestamptz | |
| executed_at | timestamptz | |

### `sensor_readings` (telemetry, append-only)
| Column | Type | Notes |
|---|---|---|
| time | timestamptz | |
| qube_id | text | |
| sensor_id | uuid | |
| field_key | text | e.g. `active_power_w` |
| value | float8 | |
| unit | text | e.g. `W` |

---

## Module 2: Qube Claiming & Org Management

### Claim Flow
1. User enters Qube ID (e.g. `Q-1302`) in the UI.
2. `POST /api/qubes/claim { qube_id, org_id }` — API checks:
   - Qube exists in `qubes` table (factory pre-registered).
   - `org_id` is null (unclaimed).
3. On success: sets `org_id`, generates `auth_token_hash = HMAC(qube_id + org_secret)`, sets `claimed_at`.
4. Qube picks up its org on the next TP-API poll, which now returns config.

### Unclaim Flow
Admin calls `DELETE /api/qubes/:id/claim`. Clears `org_id`, rotates/invalidates auth token. Qube goes back to factory state on next sync.

### Transfer Between Orgs
Admin-only. `POST /api/qubes/:id/transfer { target_org_id }`. Logs the transfer event. Old org loses access immediately (token hash changes).

### Qube Online Status
Conf-Agent sends `POST /v1/heartbeat` on every poll cycle. Cloud sets `last_seen = now()`. A background job marks Qubes as `offline` if `last_seen > 2 minutes` ago.

---

## Module 3: Gateway Management & Auto-Provisioning

### Add Gateway Flow
1. User calls `POST /api/gateways` with `{ qube_id, name, protocol, host, port, config_json }`.
2. Cloud creates the `gateway` record.
3. Cloud auto-creates a `service` record with default Docker image for the protocol:
   - `modbus_tcp` → `ghcr.io/qube/modbus-gateway:latest`, port 502
   - `mqtt` → `ghcr.io/qube/mqtt-gateway:latest`
   - `opcua` → `ghcr.io/qube/opcua-gateway:latest`
4. Config hash for the Qube is recalculated and stored in `config_state`.
5. On next Conf-Agent poll (≤60s), hash mismatch detected → full config downloaded → `docker-compose.yml` regenerated → `docker stack deploy`.
6. Empty CSV file is created for the service (ready for sensor rows).

### Protocol-Specific Gateway Config (`config_json`)

**Modbus TCP:**
```json
{
  "unit_id": 1,
  "poll_interval_ms": 5000,
  "timeout_ms": 3000,
  "max_retries": 3
}
```

**MQTT:**
```json
{
  "broker_url": "mqtt://broker.example.com",
  "port": 1883,
  "username": "iotuser",
  "password_ref": "secret:mqtt_pw",
  "base_topic": "mitesp/secondfloor/iotteam",
  "qos": 1,
  "keepalive_s": 60
}
```

**OPC-UA:**
```json
{
  "endpoint_url": "opc.tcp://192.168.1.10:4840",
  "security_mode": "None",
  "security_policy": "None",
  "namespace_index": 2,
  "poll_interval_ms": 5000
}
```

### Update/Delete Gateway
- `PUT /api/gateways/:id` → updates record, triggers hash recalculation.
- `DELETE /api/gateways/:id` → removes gateway, service, and all associated sensor/CSV rows. Hash recalculated → Conf-Agent removes container on next sync.

---

## Module 4: Sensor Management & CSV Auto-Generation

### Add Sensor Flow
1. `POST /api/sensors` with `{ gateway_id, name, template_id, address_params, tags_json }`.
2. Cloud fetches the sensor template's `config_json`.
3. Cloud generates `service_csv_rows` from the template register map + sensor params.
4. Config hash for the Qube recalculated.
5. On next Conf-Agent poll: CSV file regenerated from `service_csv_rows` → container restarted (or config reloaded if hot-reload supported).

### Example: Modbus TCP Sensor

Template `config_json` for "Schneider PM5100":
```json
{
  "registers": [
    {"reading": "Voltage_L1",    "reg_type": "Holding", "address": 3000, "type": "float32", "output": "influxdb", "table": "Measurements"},
    {"reading": "Current_L1",    "reg_type": "Holding", "address": 3002, "type": "float32", "output": "influxdb", "table": "Measurements"},
    {"reading": "Active_Power",  "reg_type": "Holding", "address": 3004, "type": "float32", "output": "influxdb", "table": "Measurements"},
    {"reading": "Energy_kWh",    "reg_type": "Holding", "address": 3008, "type": "float32", "output": "influxdb", "table": "Measurements"}
  ]
}
```

Sensor added with `name = "PM5100_Rack1"`, `tags_json = {"name":"PM5100_Rack1","location":"rack1"}`.

Generated CSV rows (`registers.csv`):
```
PM5100_Rack1, Voltage_L1,   Holding, 3000, float32, influxdb, Measurements, name=PM5100_Rack1,location=rack1
PM5100_Rack1, Current_L1,   Holding, 3002, float32, influxdb, Measurements, name=PM5100_Rack1,location=rack1
PM5100_Rack1, Active_Power,  Holding, 3004, float32, influxdb, Measurements, name=PM5100_Rack1,location=rack1
PM5100_Rack1, Energy_kWh,    Holding, 3008, float32, influxdb, Measurements, name=PM5100_Rack1,location=rack1
```

### Update/Delete Sensor
- `PUT /api/sensors/:id` → updates `service_csv_rows` for this sensor, recalculates hash.
- `DELETE /api/sensors/:id` → removes all `service_csv_rows` for this sensor, recalculates hash.

---

## Module 5: Protocol-Specific Sensor Configs

### Modbus TCP

Sensor `address_params`:
```json
{
  "unit_id": 1,
  "register_offset": 0
}
```
The `register_offset` allows a single template to serve multiple identical devices at different address bases.

CSV generation: For each register in `config_json.registers`, produce one `service_csv_row` with columns: `Equipment, Reading, RegType, Address + offset, type, Output, Table, Tags`.

### MQTT (New — Filling the Gap)

MQTT sensors don't have register addresses. Instead, each sensor has a **topic** and a **payload schema**.

Sensor `address_params`:
```json
{
  "topic_suffix": "sensor_001",
  "full_topic": "mitesp/secondfloor/iotteam/sensor_001"
}
```

Template `config_json` for MQTT:
```json
{
  "variables": [
    {"name": "voltage",      "json_path": "$.data.voltage",  "unit": "V",   "influx_field": "voltage_v"},
    {"name": "current",      "json_path": "$.data.current",  "unit": "A",   "influx_field": "current_a"},
    {"name": "active_power", "json_path": "$.data.power",    "unit": "W",   "influx_field": "active_power_w"},
    {"name": "energy_kwh",   "json_path": "$.data.energy",   "unit": "kWh", "influx_field": "energy_kwh"}
  ]
}
```

Generated `topics.csv`:
```
sensor_001, voltage,      $.data.voltage, influxdb, Measurements, name=sensor_001
sensor_001, current,      $.data.current, influxdb, Measurements, name=sensor_001
sensor_001, active_power, $.data.power,   influxdb, Measurements, name=sensor_001
sensor_001, energy_kwh,   $.data.energy,  influxdb, Measurements, name=sensor_001
```

The MQTT gateway service subscribes to all topics listed, extracts values via JSON paths, and sends structured JSON to Coreswitch:
```json
{
  "Equipment": "sensor_001",
  "Reading": "active_power_w",
  "Value": 1250.5,
  "Output": "influxdb",
  "Table": "Measurements",
  "Tags": {"name": "sensor_001"}
}
```

### OPC-UA

Sensor `address_params`:
```json
{
  "node_ids": [
    {"reading": "Temperature", "node_id": "ns=2;i=1001"},
    {"reading": "Pressure",    "node_id": "ns=2;i=1002"}
  ]
}
```

Template `config_json` for OPC-UA:
```json
{
  "nodes": [
    {"reading": "Temperature", "output": "influxdb", "table": "Measurements", "type": "float"},
    {"reading": "Pressure",    "output": "influxdb", "table": "Measurements", "type": "float"}
  ]
}
```

Generated `nodes.csv`:
```
Sensor_A, Temperature, ns=2;i=1001, float, influxdb, Measurements, name=Sensor_A
Sensor_A, Pressure,    ns=2;i=1002, float, influxdb, Measurements, name=Sensor_A
```

---

## Module 6: Sensor Variable Templates

Templates are the knowledge base of the system. They encode everything the platform needs to know about a device type.

### Template Structure

```json
{
  "name": "Schneider PM5100",
  "protocol": "modbus_tcp",
  "description": "Schneider Electric PM5100 Power Meter",
  "is_global": true,
  "config_json": {
    "registers": [ ... ]
  },
  "influx_fields_json": {
    "Voltage_L1":   {"unit": "V",   "display_label": "Voltage L1",   "data_type": "float"},
    "Active_Power": {"unit": "W",   "display_label": "Active Power",  "data_type": "float"},
    "Energy_kWh":   {"unit": "kWh", "display_label": "Energy",        "data_type": "float"}
  },
  "ui_mapping_json": {
    "panels": [
      {"type": "gauge",    "title": "Active Power",  "field": "Active_Power",  "unit": "W",   "max": 10000},
      {"type": "timeseries","title": "Voltage Trend","field": "Voltage_L1",    "unit": "V"},
      {"type": "stat",     "title": "Total Energy",  "field": "Energy_kWh",    "unit": "kWh"}
    ]
  }
}
```

### Template API

| Method | Path | Description |
|---|---|---|
| GET | /api/templates | List all (global + org-owned), filter by `protocol` |
| GET | /api/templates/:id | Single template detail |
| POST | /api/templates | Create org-specific template |
| PUT | /api/templates/:id | Update (org templates only) |
| DELETE | /api/templates/:id | Delete (org templates only) |
| POST | /api/templates/:id/clone | Clone global template to org |
| GET | /api/templates/:id/preview-csv | Preview CSV output for a given sensor params |

### Template Versioning
Templates have a `version` int. When a template is updated, existing sensors are **not** automatically regenerated (to avoid accidental mass-redeploy). A `PUT /api/sensors/:id/sync-template` endpoint explicitly re-generates CSV rows from the latest template version.

---

## Module 7: Service / Container Management

Services = Docker containers running on the Qube. Normally auto-managed by the gateway/sensor flows, but can be managed directly for custom containers.

### Service CRUD API

| Method | Path | Description |
|---|---|---|
| GET | /api/qubes/:id/services | List services for a Qube |
| POST | /api/qubes/:id/services | Add custom service |
| PUT | /api/services/:id | Update service config |
| DELETE | /api/services/:id | Remove service |

### CSV Row Management API

| Method | Path | Description |
|---|---|---|
| GET | /api/services/:id/csv | Get all CSV rows (rendered as CSV text) |
| GET | /api/services/:id/csv/rows | Get rows as JSON array |
| POST | /api/services/:id/csv/rows | Add a manual row |
| PUT | /api/services/:id/csv/rows/:row_id | Update a row |
| DELETE | /api/services/:id/csv/rows/:row_id | Delete a row |
| POST | /api/services/:id/csv/import | Bulk import (CSV file upload, parsed to row_data) |

### Docker Compose Generation

When config hash changes, the Conf-Agent downloads from TP-API a `docker_compose_yml` string. The Cloud builds this from all active services for the Qube:

```yaml
version: "3.8"
services:
  coreswitch:
    image: ghcr.io/qube/coreswitch:latest
    ports: ["8080:8080"]
    restart: always

  modbus-gateway-rack1:
    image: ghcr.io/qube/modbus-gateway:latest
    volumes:
      - ./configs/modbus-rack1/registers.csv:/app/registers.csv
    environment:
      HOST: 192.168.1.50
      PORT: 502
      CORESWITCH_URL: http://coreswitch:8080
    restart: always

  mqtt-gateway-floor2:
    image: ghcr.io/qube/mqtt-gateway:latest
    volumes:
      - ./configs/mqtt-floor2/topics.csv:/app/topics.csv
    environment:
      BROKER_URL: mqtt://broker.internal:1883
      CORESWITCH_URL: http://coreswitch:8080
    restart: always
```

Each gateway service gets its own named CSV directory. Conf-Agent writes both `docker-compose.yml` and all CSV files, then runs `docker stack deploy`.

---

## Module 8: TP-API Sync Engine

The TP-API is the bridge between the Cloud Postgres state and the Qube's Conf-Agent. **The Qube never calls the main Cloud API** — only TP-API endpoints, all authenticated by the Qube's auth token.

### Endpoints

#### `GET /v1/sync/state`
Header: `Authorization: Bearer <qube_auth_token>`

Response:
```json
{
  "qube_id": "Q-1302",
  "hash": "a3f8c2d1...",
  "updated_at": "2025-01-15T10:23:00Z"
}
```

Conf-Agent compares this hash with its locally stored hash. If matching: no action. If different: call `/v1/sync/config`.

#### `GET /v1/sync/config`
Returns the full config payload for the Qube:

```json
{
  "hash": "a3f8c2d1...",
  "docker_compose_yml": "version: \"3.8\"\nservices:\n  ...",
  "csv_files": {
    "configs/modbus-rack1/registers.csv": "Equipment,Reading,...\nUPS_Main,...",
    "configs/mqtt-floor2/topics.csv": "sensor_001,voltage,..."
  },
  "env_files": {
    "configs/modbus-rack1/.env": "HOST=192.168.1.50\nPORT=502"
  }
}
```

Conf-Agent writes each file to the local filesystem, then runs `docker stack deploy`.

#### `POST /v1/commands/poll`
Conf-Agent calls this on every sync cycle. Returns the next batch of pending commands:

```json
{
  "commands": [
    {"id": "cmd-001", "command": "ping", "payload": {"target": "192.168.1.50"}},
    {"id": "cmd-002", "command": "restart_service", "payload": {"service": "modbus-gateway-rack1"}}
  ]
}
```

#### `POST /v1/commands/:id/ack`
After executing a command:

```json
{
  "status": "executed",
  "result": {"latency_ms": 12, "success": true}
}
```

#### `POST /v1/heartbeat`
Called every sync cycle. Updates `last_seen`.

#### `POST /v1/telemetry/ingest`
Bulk telemetry from `influx-to-sql` service:

```json
{
  "readings": [
    {"time": "2025-01-15T10:22:55Z", "sensor_id": "uuid...", "field_key": "Active_Power", "value": 1250.5},
    ...
  ]
}
```

### Hash Computation

The config hash is computed server-side whenever any of the following change:
- A service is added/updated/deleted for the Qube.
- A gateway is added/updated/deleted.
- A sensor is added/updated/deleted.
- Any `service_csv_rows` changes.

```
hash = SHA256(
  sort_by_id(services) +
  sort_by_id(gateways) +
  sort_by_service_id_then_row_order(csv_rows) +
  docker_compose_template_version
)
```

A background job recomputes and stores the hash in `config_state` after every relevant DB mutation.

---

## Module 9: Device Command Queue

All device operations are asynchronous — they go through the command queue and are picked up by Conf-Agent on its next poll.

### Supported Commands

| Command | Payload | Result |
|---|---|---|
| `ping` | `{"target": "ip_or_hostname"}` | `{"latency_ms": N, "success": bool}` |
| `restart_qube` | `{}` | `{"rebooting": true}` |
| `restart_service` | `{"service": "service_name"}` | `{"restarted": true, "stdout": "..."}` |
| `reload_config` | `{}` | Force immediate re-sync, bypasses interval wait |
| `get_logs` | `{"service": "name", "lines": 100}` | `{"logs": "..."}` |
| `list_containers` | `{}` | `{"containers": [...]}` |

### API

| Method | Path | Description |
|---|---|---|
| POST | /api/qubes/:id/commands | Send a command |
| GET | /api/qubes/:id/commands | List recent commands (status, result) |
| GET | /api/commands/:id | Poll single command for result |

### Result Delivery
Frontend polls `GET /api/commands/:id` every 2s. Once `status = executed` or `failed`, it displays the result. Timeout after 120s if Qube never picks up (Qube offline).

---

## Module 10: Data Pipeline (InfluxDB → Postgres)

The `influx-to-sql` service runs on the Qube. It:
1. Reads new data from InfluxDB (last 60s on each cycle).
2. Maps InfluxDB measurements to `sensor_id` via gateway/sensor name matching.
3. Calls `POST /v1/telemetry/ingest` to push readings to Cloud Postgres `sensor_readings` table.

### Sensor ID Mapping
`influx-to-sql` needs to know which InfluxDB `Equipment` + `Reading` tag combination maps to which `sensor_id` in Postgres. This mapping is included in the `/v1/sync/config` response:

```json
{
  "sensor_map": {
    "PM5100_Rack1.Active_Power": "uuid-sensor-1",
    "PM5100_Rack1.Voltage_L1":   "uuid-sensor-2"
  }
}
```

### Telemetry Query API
Frontend can query historical readings:

```
GET /api/data/readings
  ?sensor_id=uuid
  &field=Active_Power
  &from=2025-01-15T00:00:00Z
  &to=2025-01-15T23:59:59Z
  &interval=5m          (optional aggregation)
  &agg=mean             (mean, max, min, last)
```

### Grafana Integration
- Grafana connects to Cloud Postgres via a read-only user.
- `ui_mapping_json` from sensor templates defines panel configuration.
- A background job auto-provisions Grafana dashboards via Grafana API when a sensor is added (if Grafana API key is configured for the org).

---

## Module 11: Frontend UX Flows

### Org Dashboard
- List of all claimed Qubes with online/offline pill, # gateways, # sensors, last sync time.
- "Claim Qube" button → modal to enter Qube ID.
- Quick actions: ping, reload config.

### Qube Detail Page
- **Overview tab**: status, location, last seen, config hash, sync history.
- **Gateways tab**: list of gateways with protocol badges. Add Gateway button.
- **Services tab**: all containers with status, last deploy time. Manual CSV management.
- **Commands tab**: command history and send panel.

### Add Gateway Modal
1. Enter name, select protocol.
2. Protocol-specific fields appear dynamically (host/port for Modbus, broker URL for MQTT, endpoint for OPC-UA).
3. Optional: advanced config (timeouts, poll interval).
4. Submit → gateway created → success toast with "container deploying" indicator.

### Sensor Management (inside Gateway Detail)
1. "Add Sensor" → select template from filtered list (by protocol).
2. Template preview shows variables that will be collected.
3. Enter sensor-specific params (IP / unit ID / topic suffix / node IDs).
4. Enter tags (key-value pairs).
5. "Preview CSV" → shows the CSV rows that will be generated before confirming.
6. Confirm → sensor created → hash recalculated → "deploying in ≤60s" message.

### Template Manager
- Global templates: read-only list with "Clone to my templates" button.
- Org templates: full CRUD.
- Template editor has three tabs:
  - **Variables** (config_json): add/edit/delete register entries, MQTT paths, OPC-UA node IDs.
  - **InfluxDB fields** (influx_fields_json): field keys, units, display labels.
  - **Dashboard layout** (ui_mapping_json): define panel types and field bindings.

---

## Module 12: Security & Auth

### Frontend ↔ Cloud API
- JWT tokens, 1h expiry with refresh token (7d).
- All endpoints require `Authorization: Bearer <jwt>`.
- Middleware enforces `org_id` scoping — every query joins on org_id.

### Qube ↔ TP-API
- Qube auth token: `HMAC-SHA256(qube_id + ":" + org_secret)` where `org_secret` is a per-org secret stored in Cloud.
- Conf-Agent sends token in `Authorization: Bearer <qube_token>` header.
- TP-API validates: recompute expected token from `qube_id` + looked-up `org_secret`, compare. If mismatch or qube unclaimed → 401.
- Token rotates when Qube is reclaimed (new org or same org re-claim).

### Role Permissions

| Action | Admin | Editor | Viewer |
|---|---|---|---|
| Claim / Unclaim / Transfer Qube | ✓ | — | — |
| Add / delete Gateway | ✓ | ✓ | — |
| Add / delete Sensor | ✓ | ✓ | — |
| Send commands (ping, restart) | ✓ | ✓ | — |
| Manage org templates | ✓ | ✓ | — |
| View all data | ✓ | ✓ | ✓ |
| Invite users | ✓ | — | — |

### Rate Limiting
- TP-API sync endpoints: 10 req/min per Qube (one poll/heartbeat cycle is ≤3 calls).
- Command dispatch: 30 commands/min per org.
- Telemetry ingest: 1000 rows/call, 60 calls/min per Qube.

---

## Module 13: API Surface Reference

All paths are prefixed `/api/v1`.

### Org & Users
```
GET    /orgs/me
PUT    /orgs/me
GET    /orgs/me/users
POST   /orgs/me/users        (invite)
DELETE /orgs/me/users/:id
```

### Qubes
```
GET    /qubes
POST   /qubes/claim
GET    /qubes/:id
PUT    /qubes/:id
DELETE /qubes/:id/claim       (unclaim)
POST   /qubes/:id/transfer
GET    /qubes/:id/status      (online/offline, last_seen, hash)
```

### Gateways
```
GET    /qubes/:id/gateways
POST   /qubes/:id/gateways
GET    /gateways/:id
PUT    /gateways/:id
DELETE /gateways/:id
```

### Sensors
```
GET    /gateways/:id/sensors
POST   /gateways/:id/sensors
GET    /sensors/:id
PUT    /sensors/:id
DELETE /sensors/:id
POST   /sensors/:id/sync-template   (regenerate CSV from latest template)
```

### Templates
```
GET    /templates
POST   /templates
GET    /templates/:id
PUT    /templates/:id
DELETE /templates/:id
POST   /templates/:id/clone
GET    /templates/:id/preview-csv?address_params=...
```

### Services & CSV
```
GET    /qubes/:id/services
POST   /qubes/:id/services
PUT    /services/:id
DELETE /services/:id
GET    /services/:id/csv
GET    /services/:id/csv/rows
POST   /services/:id/csv/rows
PUT    /services/:id/csv/rows/:row_id
DELETE /services/:id/csv/rows/:row_id
POST   /services/:id/csv/import
```

### Commands
```
POST   /qubes/:id/commands
GET    /qubes/:id/commands
GET    /commands/:id
```

### Data / Telemetry
```
GET    /data/readings
GET    /data/sensors/:id/latest     (last known values for each field)
GET    /data/qubes/:id/summary      (all sensors, all latest values)
```

### TP-API (Qube-internal, separate prefix `/v1`)
```
GET    /v1/sync/state
GET    /v1/sync/config
POST   /v1/commands/poll
POST   /v1/commands/:id/ack
POST   /v1/heartbeat
POST   /v1/telemetry/ingest
```

---

## Module 14: Implementation Phases

### Phase 1 — Foundation (Weeks 1–3)
- Data model + migrations.
- Qube claiming and auth token system.
- TP-API: `/v1/sync/state`, `/v1/sync/config`, `/v1/heartbeat`.
- Conf-Agent: polling loop, hash comparison, file write, stack deploy.
- Basic frontend: claim Qube, see online/offline status.

### Phase 2 — Gateway & Sensor Automation (Weeks 4–6)
- Gateway CRUD → auto service creation → hash recalculation.
- Modbus TCP template system → CSV row generation.
- Sensor CRUD → CSV auto-generation → Conf-Agent picks up.
- Preview CSV in frontend before confirming sensor add.
- Manual CSV row management UI.

### Phase 3 — MQTT & OPC-UA (Weeks 7–8)
- MQTT gateway type with JSON path variable mapping.
- OPC-UA gateway type with node ID mapping.
- Template editor: all three protocol types.
- `topics.csv` and `nodes.csv` generation.

### Phase 4 — Commands & Telemetry (Weeks 9–10)
- Command queue system.
- `POST /v1/commands/poll`, `ack`.
- `influx-to-sql` sensor_map injection via sync config.
- `POST /v1/telemetry/ingest` and `sensor_readings` table.
- Frontend: command panel, live status.

### Phase 5 — Dashboards & Templates (Weeks 11–12)
- Template manager UI (full CRUD + clone).
- `ui_mapping_json` editor.
- Grafana auto-provisioning on sensor add.
- Telemetry query API + sparklines in frontend.

### Phase 6 — Polish & Scale (Weeks 13–14)
- Role-based access enforcement everywhere.
- Rate limiting, request logging.
- Qube transfer between orgs.
- Audit log table (who claimed, who added what, when).
- Multi-Qube bulk operations (deploy same config to N Qubes).
