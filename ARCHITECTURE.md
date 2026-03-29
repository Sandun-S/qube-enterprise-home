# Qube Enterprise v2 — Architecture

## Components

| Service | Where | What it does |
|---------|-------|-------------|
| `cloud-api` | Cloud VM | Cloud API (JWT :8080) + TP-API (HMAC :8081) + WebSocket (/ws) |
| `conf-agent` | Every Qube | Self-registers, WebSocket client, SQLite writer, Docker manager |
| `enterprise-influx-to-sql` | Every Qube | Reads InfluxDB v1 → maps via SQLite → POSTs SenML to TP-API |
| `core-switch` | Every Qube | Receives JSON from readers → routes to InfluxDB or live WebSocket |
| `modbus-reader` | Qube (auto-deployed) | Reads Modbus TCP registers from SQLite config |
| `snmp-reader` | Qube (auto-deployed) | Polls SNMP targets from SQLite config |
| `mqtt-reader` | Qube (auto-deployed) | Subscribes to MQTT topics from SQLite config |
| `opcua-reader` | Qube (auto-deployed) | Reads OPC-UA nodes from SQLite config |
| `http-reader` | Qube (auto-deployed) | Polls HTTP/REST endpoints from SQLite config |

---

## Data flow

```
USER
  │  JWT
  ▼
Cloud API (:8080)
  │ creates/updates readers + sensors + templates
  │ → recomputeConfigHash() → config_state.hash changes
  │
  │ WebSocket push: {"type":"config_update","hash":"..."}
  ▼
conf-agent (on Qube)                    ◄── also polls TP-API :8081 as fallback
  │ GET /v1/sync/config
  │   returns: {readers, sensors, containers, docker_compose_yml}
  │
  │ → writes to SQLite /opt/qube/data/qube.db (WAL mode, only writer)
  │ → Docker API: stop affected reader containers
  │   (Docker Swarm auto-recreates them)
  ▼
Reader containers (modbus-reader, snmp-reader, etc.)
  │ start → read config from SQLite (read-only, shared volume)
  │ poll devices / subscribe to topics
  │
  │ POST /v3/batch to core-switch:8585
  ▼
core-switch
  ├── output=influxdb → write InfluxDB v1 (line protocol, db=edgex)
  └── output=live    → WebSocket push to cloud (:8080/ws/dashboard)

enterprise-influx-to-sql (on Qube, polling InfluxDB)
  │ reads sensor_map from SQLite (sensors table: equipment → sensor_uuid)
  │ POST /v1/telemetry/ingest to TP-API :8081
  │   body: SenML {readings:[{time,sensor_id,field_key,value,unit}]}
  ▼
TP-API → TimescaleDB (qubedata.sensor_readings hypertable)

USER
  │ JWT
  ▼
Cloud API GET /api/v1/data/sensors/:id/latest
  │       GET /api/v1/data/readings?sensor_id=...
  ▼
TimescaleDB → response
```

---

## Ports

| Port | Service | Auth | Direction |
|------|---------|------|-----------|
| 8080 | Cloud API + WebSocket | JWT Bearer | Frontend → Cloud; Qube → Cloud (WS) |
| 8081 | TP-API | HMAC-SHA256 | Qube → Cloud only |
| 5432 | PostgreSQL | password | Cloud API internal |
| 8086 | InfluxDB v1 | none | Edge internal (readers → core-switch → InfluxDB) |
| 8585 | core-switch | none | Edge internal (readers → core-switch) |
| 8888 | Test UI | none | Dev only |

---

## Key design decisions

### SQLite on Qube (replaces CSV files)
Reader containers read their config from a shared SQLite database at
`/opt/qube/data/qube.db`. conf-agent is the **only writer** (WAL mode). Readers open
read-only on startup. Config reload = Docker stop → Swarm recreate → reads fresh SQLite.
No polling of SQLite — no live reload complexity.

### WebSocket sync (replaces HTTP polling)
Cloud pushes `config_update` events to conf-agent via WebSocket immediately when hash
changes. HTTP polling via TP-API (:8081) is a fallback for networks that don't support
persistent connections.

### TimescaleDB for telemetry
`qubedata` is a separate PostgreSQL database running the TimescaleDB extension.
`sensor_readings` is a hypertable partitioned by time. This scales to millions of readings
without manual partitioning. Management data stays in `qubedb`.

### Split templates
- **device_template**: what the sensor reports (registers, OIDs, MQTT topics). Per-org or global (superadmin). Version-tracked. Used by sensors.
- **reader_template**: what container to deploy (image_suffix, connection_schema, env_defaults). Managed by IoT team (superadmin only). Defines the UI form for reader connection params.

### No ORM, no internal MQTT
Raw SQL with `pgx`. Responses are `map[string]any` — no DTO structs.
Internal MQTT broker removed (no Grafana on Qube anymore). MQTT is only used when a
user explicitly adds an MQTT reader to connect to an external broker.

### Zero-touch provisioning
Qubes self-register from `/boot/mit.txt` written at flash time. They never need manual
SSH or file transfer after the factory flash.

---

## Authentication

### Cloud API (port 8080) — JWT
```
POST /api/v1/auth/login → {token: "<jwt>"}
Authorization: Bearer <jwt>
```
Roles (least to most): `viewer`, `editor`, `admin`, `superadmin`

### TP-API (port 8081) — HMAC
```
X-Qube-ID: Q-1001
Authorization: Bearer <hmac_token>

token = HMAC-SHA256(key=org.org_secret, data=qubeID+":"+org.org_secret)
```
Token is stable per org/qube pair. Rotates only on re-claim.

---

## Database schema (management — qubedb)

```
organisations ──┬── users
                ├── qubes ──┬── readers ──── sensors
                │           ├── containers
                │           ├── config_state
                │           ├── coreswitch_settings
                │           └── qube_commands
                └── (via readers) ── device_templates

reader_templates (global — superadmin managed)
protocols (global — superadmin managed)
registry_settings (global — one row)
```

## Database schema (telemetry — qubedata)

```
sensor_readings (TimescaleDB hypertable)
  time, qube_id, sensor_id, field_key, value, unit, tags
```
