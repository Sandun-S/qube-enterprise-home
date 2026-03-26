# Qube Enterprise v2 — Complete Architecture & Implementation Plan

> **Date**: 2026-03-26
> **Status**: Planning
> **Scope**: Full architectural rewrite — Cloud API, TP-API, Conf-Agent, Core-Switch, all Readers (gateways→readers), SQLite edge DB, webhook sync, split databases, template unification

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Key Architectural Changes](#2-key-architectural-changes)
3. [Naming Convention Changes](#3-naming-convention-changes)
4. [System Architecture v2](#4-system-architecture-v2)
5. [Edge Architecture — SQLite](#5-edge-architecture--sqlite)
6. [Cloud Architecture — Split Databases](#6-cloud-architecture--split-databases)
7. [Webhook-Based Communication](#7-webhook-based-communication)
8. [Unified Template System](#8-unified-template-system)
9. [Reader (Gateway) Standardization](#9-reader-gateway-standardization)
10. [Core-Switch v2](#10-core-switch-v2)
11. [Protocol Standards & Libraries](#11-protocol-standards--libraries)
12. [Docker Swarm File Generation](#12-docker-swarm-file-generation)
13. [Auto-Discovery Flow](#13-auto-discovery-flow)
14. [Migration Strategy (SQL)](#14-migration-strategy-sql)
15. [API Surface v2](#15-api-surface-v2)
16. [Configuration Formats (JSON, not YAML)](#16-configuration-formats-json-not-yaml)
17. [Implementation Phases](#17-implementation-phases)
18. [Repository Structure v2](#18-repository-structure-v2)
19. [Standards Reference](#19-standards-reference)
20. [Testing & Documentation Updates](#20-testing--documentation-updates)
21. [CI/CD & Registry Considerations](#21-cicd--registry-considerations)
22. [Open Questions & Decisions](#22-open-questions--decisions)

---

## 1. Executive Summary

Qube Enterprise v2 is a ground-up architectural evolution that:

- **Eliminates CSV files** → All reader config stored in **SQLite** on each Qube
- **Eliminates polling** → Cloud pushes config changes via **webhooks** to TP-API on the Qube
- **Renames "gateway" to "reader"** → Accurate naming (these read data, not route traffic)
- **Splits cloud Postgres** into **management DB** + **telemetry DB** (per-qube partitioned tables)
- **Unifies templates** → Single source of truth for reader config, sensor definitions, container specs
- **Standardizes config format** → JSON everywhere (no more YAML configs)
- **Adds HTTP reader** → For REST API sensors and webhooks
- **Uses EMQX** → Production MQTT broker (replaces Mosquitto)
- **Supports incremental sync** → Only changed config is pushed, not the full payload
- **Adds auto-discovery** → Scan endpoints, show available data, let user map to sensors
- **Uses SenML (RFC 8428)** → Standard JSON format for telemetry payloads

### What Stays the Same
- Go 1.22+ (all services)
- PostgreSQL (cloud)
- InfluxDB v1 (edge buffer — still needed for core-switch compatibility)
- Docker Swarm (deployment)
- JWT + HMAC (authentication model)
- pgx driver (raw SQL, no ORM)

---

## 2. Key Architectural Changes

| Area | v1 (Current) | v2 (New) |
|------|-------------|----------|
| Edge config storage | CSV files + YAML configs | **SQLite database** on qube-net |
| Config sync | Polling (conf-agent polls every 30s) | **Webhook push** (cloud → qube TP-API) |
| Config format | YAML (configs.yml) | **JSON** (config.json) |
| Naming | "gateway" | **"reader"** |
| Cloud database | Single Postgres | **2 Postgres databases** (mgmt + telemetry) |
| Telemetry tables | Single `sensor_readings` table | **Per-qube tables** with time partitioning |
| MQTT broker | Mosquitto | **EMQX** |
| Sensor mapping | sensor_map.json file | **SQLite table** (`sensor_map`) |
| Template system | Scattered (template + addr_params + csv_rows) | **Unified template** (reader + sensor + container in one) |
| Config delivery | Full payload every sync | **Incremental** (only changed entities) |
| Telemetry format | Custom JSON | **SenML (RFC 8428)** |
| Compose generation | String concatenation | **Stored in DB** per qube |

---

## 3. Naming Convention Changes

### Rename Map

| v1 Term | v2 Term | Rationale |
|---------|---------|-----------|
| `gateway` | `reader` | These read data from devices, not route traffic |
| `gateways` (table) | `readers` | |
| `gateway_id` | `reader_id` | |
| `modbus-gateway` | `modbus-reader` | Container name |
| `mqtt-gateway` | `mqtt-reader` | |
| `snmp-gateway` | `snmp-reader` | |
| `opcua-gateway` | `opcua-reader` | |
| `service_csv_rows` | *(removed)* | Replaced by SQLite |
| `services` | `containers` | More accurate |
| `config.csv` | *(removed)* | Data lives in SQLite |
| `configs.yml` | `config.json` | JSON format |
| `sensor_map.json` | *(removed)* | Lives in SQLite `sensor_map` table |
| `conf-agent` | `enterprise-conf-agent` | Already used in compose |

### Code Impact
- All Go source files: struct names, function names, variable names, route paths
- Database: table names, column names, FK references
- Docker: image names, service names, volume paths
- API: endpoint paths (`/gateways/` → `/readers/`)
- Tests: all assertions and curl calls
- Documentation: all .md files

---

## 4. System Architecture v2

```
┌─────────────────────────────────────────────────────────────┐
│                        CLOUD                                │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │  Cloud API    │    │  TP-API      │    │  Control API │  │
│  │  :8080 (JWT)  │    │  :8081 (HMAC)│    │  :8082 (JWT) │  │
│  │  User-facing  │    │  Qube-facing │    │  Core-Switch │  │
│  │  management   │    │  webhook push│    │  settings    │  │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘  │
│         │                   │                    │          │
│  ┌──────┴───────────────────┴────────────────────┘          │
│  │                                                          │
│  ├── Management DB (Postgres)                               │
│  │   orgs, users, qubes, readers, sensors, templates,       │
│  │   containers, config_state, commands                     │
│  │                                                          │
│  └── Telemetry DB (Postgres)                                │
│      Per-qube schemas, monthly partitioned tables           │
│      qube_Q1001.readings_2026_03, ...                       │
│                                                             │
└─────────────────────────────┬───────────────────────────────┘
                              │ Webhook (HTTPS)
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     QUBE (Edge Device)                      │
│                      Docker Swarm                           │
│                      Network: qube-net                      │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │ Conf-Agent   │    │ TP-API Local │    │  Core-Switch  │  │
│  │ (orchestrator│◄──►│ :8081        │◄──►│  :8585       │  │
│  │  + deployer) │    │ receives     │    │  influx+mqtt │  │
│  └──────┬───────┘    │ webhooks     │    │  +live path  │  │
│         │            └──────┬───────┘    └──────┬───────┘  │
│         │                   │                    │          │
│  ┌──────┴───────────────────┴────────────────────┘          │
│  │                                                          │
│  ├── SQLite DB (shared on qube-net volume)                  │
│  │   readers, sensors, sensor_map, reader_config,           │
│  │   container_state, pending_changes                       │
│  │                                                          │
│  ├── InfluxDB v1 (edge buffer)                              │
│  │                                                          │
│  └── EMQX (internal MQTT broker)                            │
│                                                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │ Modbus   │ │ SNMP     │ │ MQTT     │ │ OPC-UA   │      │
│  │ Reader   │ │ Reader   │ │ Reader   │ │ Reader   │      │
│  │          │ │          │ │          │ │          │      │
│  │ Reads    │ │ Reads    │ │ Reads    │ │ Reads    │      │
│  │ SQLite   │ │ SQLite   │ │ SQLite   │ │ SQLite   │      │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘      │
│                                                             │
│  ┌──────────────────┐                                       │
│  │ Influx-to-SQL    │ (reads InfluxDB + SQLite sensor_map)  │
│  │ → POST to TP-API │ (telemetry ingest, SenML format)     │
│  └──────────────────┘                                       │
└─────────────────────────────────────────────────────────────┘
```

### New Service: Control API (:8082)

A new API for controlling core-switch settings remotely:

```
POST /api/v1/qubes/{id}/coreswitch/settings
{
  "outputs": {
    "influxdb": {"enabled": true, "url": "http://influxdb:8086", "db": "edgex"},
    "mqtt":     {"enabled": true, "broker": "tcp://emqx:1883", "topic_prefix": "qube/data"},
    "live":     {"enabled": false, "webhook_url": ""}
  },
  "batch_size": 100,
  "flush_interval_ms": 5000
}
```

This gets pushed to the Qube SQLite → core-switch reads it.

---

## 5. Edge Architecture — SQLite

### Why SQLite?

1. **Single source of truth** on the Qube — no scattered CSV/JSON/YAML files
2. **Atomic updates** — transactions ensure config consistency
3. **Queryable** — readers can SELECT exactly what they need
4. **Shared via volume** — all containers on qube-net can access it
5. **Sync-friendly** — change tracking with version numbers
6. **No server process** — file-based, zero overhead

### SQLite Schema (on Qube)

```sql
-- =============================================
-- QUBE EDGE SQLite DATABASE
-- Shared volume: /opt/qube/data/qube.db
-- =============================================

-- Qube identity
CREATE TABLE qube_identity (
    qube_id       TEXT PRIMARY KEY,
    org_id        TEXT,
    qube_token    TEXT,
    cloud_tp_url  TEXT,    -- e.g. "https://cloud.example.com:8081"
    last_sync_at  TEXT,    -- ISO 8601
    config_version INTEGER DEFAULT 0
);

-- Reader definitions (synced from cloud)
CREATE TABLE readers (
    id            TEXT PRIMARY KEY,  -- UUID from cloud
    name          TEXT NOT NULL,
    protocol      TEXT NOT NULL,     -- modbus_tcp, snmp, mqtt, opcua, http
    config_json   TEXT NOT NULL,     -- JSON: host, port, poll_interval, credentials, etc.
    status        TEXT DEFAULT 'active',  -- active, disabled
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Sensor definitions (synced from cloud)
CREATE TABLE sensors (
    id            TEXT PRIMARY KEY,  -- UUID from cloud
    reader_id     TEXT NOT NULL REFERENCES readers(id),
    name          TEXT NOT NULL,
    template_id   TEXT,
    config_json   TEXT NOT NULL,     -- JSON: protocol-specific sensor config
                                    -- Modbus: registers array
                                    -- SNMP: oid_map array
                                    -- MQTT: topic + json_paths
                                    -- OPC-UA: node_ids
    tags_json     TEXT,              -- JSON: user-defined tags
    status        TEXT DEFAULT 'active',
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Sensor map (replaces sensor_map.json)
-- Maps InfluxDB measurement keys → cloud sensor UUIDs
CREATE TABLE sensor_map (
    measurement_key TEXT PRIMARY KEY,  -- "Equipment.Reading" e.g. "Main_Meter.voltage_v"
    sensor_id       TEXT NOT NULL,
    field_key       TEXT NOT NULL,
    unit            TEXT,
    updated_at      TEXT
);

-- Container definitions (synced from cloud)
CREATE TABLE containers (
    id            TEXT PRIMARY KEY,  -- UUID from cloud
    name          TEXT NOT NULL,     -- Docker service name
    image         TEXT NOT NULL,     -- Docker image
    reader_id     TEXT REFERENCES readers(id),
    env_json      TEXT,              -- JSON: environment variables
    volumes_json  TEXT,              -- JSON: volume mounts
    status        TEXT DEFAULT 'active',
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Core-switch settings (synced from cloud via Control API)
CREATE TABLE coreswitch_settings (
    key           TEXT PRIMARY KEY,
    value_json    TEXT NOT NULL,
    updated_at    TEXT
);

-- Docker Swarm file (generated, stored as text)
CREATE TABLE swarm_state (
    id            INTEGER PRIMARY KEY DEFAULT 1,
    compose_yml   TEXT,           -- The generated docker-compose.yml
    config_hash   TEXT,           -- SHA-256 of current config
    deployed_hash TEXT,           -- Hash that was last deployed
    updated_at    TEXT
);

-- Sync log (track what changed)
CREATE TABLE sync_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type   TEXT NOT NULL,  -- 'reader', 'sensor', 'container', 'coreswitch'
    entity_id     TEXT NOT NULL,
    action        TEXT NOT NULL,  -- 'create', 'update', 'delete'
    version       INTEGER,
    synced_at     TEXT DEFAULT (datetime('now'))
);

-- Pending outbound data (telemetry buffer if cloud is unreachable)
CREATE TABLE telemetry_buffer (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    payload_json  TEXT NOT NULL,   -- SenML array
    created_at    TEXT DEFAULT (datetime('now')),
    sent          INTEGER DEFAULT 0
);

-- Version tracking for incremental sync
CREATE TABLE sync_state (
    entity_type   TEXT PRIMARY KEY,  -- 'readers', 'sensors', 'containers', 'coreswitch'
    last_version  INTEGER DEFAULT 0,
    last_synced   TEXT
);
```

### How Readers Access SQLite

Each reader container mounts the shared SQLite volume:

```
volumes:
  - /opt/qube/data:/data:ro   # Read-only for readers
```

**Reader startup pattern (all protocols):**

```go
// Standard reader initialization
func loadConfig(db *sql.DB, readerID string) (*ReaderConfig, []SensorConfig, error) {
    // 1. Load reader config
    var configJSON string
    db.QueryRow("SELECT config_json FROM readers WHERE id=? AND status='active'", readerID).Scan(&configJSON)

    // 2. Load all sensors for this reader
    rows, _ := db.Query("SELECT id, name, config_json, tags_json FROM sensors WHERE reader_id=? AND status='active'", readerID)

    // 3. Parse and return
    // Each protocol interprets config_json differently
}
```

### SQLite Write Access

**Only conf-agent writes to SQLite.** All readers have read-only access. This prevents write conflicts.

```
conf-agent   → /opt/qube/data/qube.db  (read-write)
all readers  → /opt/qube/data/qube.db  (read-only)
influx-to-sql → /opt/qube/data/qube.db (read-only, for sensor_map)
core-switch  → /opt/qube/data/qube.db  (read-only, for settings)
```

### Config Change Detection by Readers

Instead of watching files, readers watch the `version` column:

```go
// Reader polls SQLite every 30s for version changes
func watchForChanges(db *sql.DB, readerID string, currentVersion int) bool {
    var newVersion int
    db.QueryRow("SELECT version FROM readers WHERE id=?", readerID).Scan(&newVersion)
    return newVersion > currentVersion
}
```

Or use SQLite's `update_hook` if using WAL mode with shared memory.

---

## 6. Cloud Architecture — Split Databases

### Database 1: Management DB (`qubedb`)

All operational state — orgs, users, qubes, readers, sensors, templates, commands.

```
postgres://qubeadmin:qubepass@localhost:5432/qubedb
```

### Database 2: Telemetry DB (`qubedata`)

All sensor readings — partitioned by qube and time.

```
postgres://qubeadmin:qubepass@localhost:5432/qubedata
```

### Telemetry DB Schema

```sql
-- =============================================
-- TELEMETRY DATABASE: qubedata
-- =============================================

-- Each Qube gets its own schema
-- Created dynamically when a Qube is claimed
CREATE SCHEMA IF NOT EXISTS qube_Q1001;

-- Readings table with monthly partitioning
CREATE TABLE qube_Q1001.readings (
    ts          TIMESTAMPTZ NOT NULL,
    sensor_id   UUID NOT NULL,
    field_key   TEXT NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    unit        TEXT,
    tags        JSONB
) PARTITION BY RANGE (ts);

-- Monthly partitions (auto-created by a maintenance function)
CREATE TABLE qube_Q1001.readings_2026_03
    PARTITION OF qube_Q1001.readings
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE qube_Q1001.readings_2026_04
    PARTITION OF qube_Q1001.readings
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

-- Index for common queries
CREATE INDEX ON qube_Q1001.readings (sensor_id, ts DESC);
CREATE INDEX ON qube_Q1001.readings (ts DESC);

-- Latest value materialized view (optional, for fast dashboards)
CREATE MATERIALIZED VIEW qube_Q1001.latest_readings AS
SELECT DISTINCT ON (sensor_id, field_key)
    sensor_id, field_key, value, unit, ts
FROM qube_Q1001.readings
ORDER BY sensor_id, field_key, ts DESC;
```

### Partition Management API

```
POST /api/v1/qubes/{id}/telemetry/settings
{
    "partition_interval": "monthly",     -- "daily", "weekly", "monthly"
    "retention_months": 12,              -- auto-drop partitions older than this
    "auto_create_ahead": 3               -- create partitions 3 intervals ahead
}
```

Cloud API runs a background goroutine that:
1. Creates future partitions ahead of time
2. Drops expired partitions based on retention policy
3. Refreshes materialized views periodically

### Why Split?

1. **Performance** — Telemetry queries don't compete with management queries
2. **Scaling** — Telemetry DB can be on separate hardware / larger disk
3. **Backup** — Different backup strategies (management: daily, telemetry: weekly)
4. **Multi-tenant isolation** — Per-qube schemas prevent cross-contamination
5. **Customer data separation** — Compliance-friendly

---

## 7. Webhook-Based Communication

### v1 Flow (Polling)
```
conf-agent → GET /v1/sync/state (every 30s)
           → GET /v1/sync/config (if hash changed)
```

### v2 Flow (Webhook Push)

```
Cloud API (user makes change)
  ↓ writes to Management DB
  ↓ computes diff
  ↓ POST webhook to Qube's TP-API

Qube TP-API (:8081) receives webhook
  ↓ validates HMAC signature
  ↓ writes changes to SQLite
  ↓ notifies conf-agent
  ↓ conf-agent redeploys if needed
```

### Webhook Payload Format

```json
{
    "event": "config.updated",
    "qube_id": "Q-1001",
    "timestamp": "2026-03-26T10:30:00Z",
    "version": 42,
    "changes": [
        {
            "entity": "reader",
            "action": "update",
            "id": "uuid-reader-1",
            "data": {
                "name": "modbus-panel-a",
                "protocol": "modbus_tcp",
                "config_json": { "host": "192.168.1.50", "port": 502, "poll_interval_sec": 20 },
                "status": "active"
            }
        },
        {
            "entity": "sensor",
            "action": "create",
            "id": "uuid-sensor-new",
            "data": {
                "reader_id": "uuid-reader-1",
                "name": "PM5100_Rack1",
                "config_json": { "registers": [...] },
                "tags_json": { "name": "PM5100_Rack1" }
            }
        }
    ],
    "signature": "hmac-sha256-of-payload"
}
```

### Webhook Events

| Event | Trigger | Payload |
|-------|---------|---------|
| `config.updated` | Reader/sensor/container CRUD | Changed entities only (incremental) |
| `config.full_sync` | Manual trigger or first sync | All entities (full snapshot) |
| `command.dispatch` | User sends command | Command details |
| `coreswitch.settings` | Control API change | New settings |
| `container.redeploy` | Image update or force redeploy | Container list |

### Fallback: Polling Still Available

If the Qube is behind NAT / unreachable by webhook:

1. Cloud marks webhook as "pending"
2. Conf-agent still polls `/v1/sync/state` on a longer interval (5 min default)
3. On poll, receives all pending changes since last version
4. Webhook delivery is "best effort" — polling is the safety net

### Webhook Registration

When a Qube self-registers, it reports its webhook endpoint:

```json
POST /v1/device/register
{
    "qube_id": "Q-1001",
    "register_key": "TEST-Q1001-REG",
    "webhook_url": "http://192.168.1.10:8081/v1/webhook",
    "capabilities": ["webhook", "sqlite"]
}
```

Cloud stores this URL and uses it for push notifications.

### Webhook Security

- **HMAC-SHA256 signature** in `X-Webhook-Signature` header
- **Timestamp** in `X-Webhook-Timestamp` to prevent replay attacks (reject if >5 min old)
- **Idempotency** via version numbers — applying same version twice is a no-op

---

## 8. Unified Template System

### Problem in v1

Template data is scattered:
- `sensor_templates.config_json` → register/OID/topic definitions
- `sensors.address_params` → device-specific addressing
- `service_csv_rows.row_data` → generated CSV data
- `services.image` → container image
- `protocols.connection_params_schema` → UI schema for reader config
- `protocols.addr_params_schema` → UI schema for sensor addressing

### v2: Unified Protocol Template

A **Protocol Template** is a single document that defines everything needed to:
1. Configure the reader container
2. Configure individual sensors
3. Generate the sensor map
4. Display correct UI forms

### Protocol Template Schema

```sql
CREATE TABLE protocol_templates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Identity
    name              TEXT NOT NULL,           -- "Schneider PM5100"
    protocol          TEXT NOT NULL,           -- "modbus_tcp"
    manufacturer      TEXT,                    -- "Schneider Electric"
    model             TEXT,                    -- "PM5100"
    description       TEXT,

    -- Scope
    org_id            UUID REFERENCES organisations(id),  -- NULL = global
    is_global         BOOLEAN DEFAULT false,

    -- Reader config schema (what the container needs)
    reader_config_schema  JSONB NOT NULL,
    -- JSON Schema defining: host, port, poll_interval, credentials, etc.
    -- UI renders form from this schema dynamically

    -- Sensor config definition (what each sensor provides)
    sensor_config       JSONB NOT NULL,
    -- Protocol-specific measurement definitions
    -- This IS the template's core value

    -- Sensor addressing schema (what user fills per-sensor)
    sensor_params_schema  JSONB NOT NULL,
    -- JSON Schema for per-sensor fields: unit_id, ip_address, topic, node_id, etc.

    -- Container specification
    container_spec      JSONB NOT NULL,
    -- {
    --   "image": "ghcr.io/.../modbus-reader",
    --   "env_defaults": {"LOG_LEVEL": "info"},
    --   "volumes": ["/data:/data:ro"],
    --   "ports": [],
    --   "multi_sensor": false  -- true = one container handles all sensors (SNMP)
    --                          -- false = one container per reader endpoint (Modbus, MQTT, OPC-UA)
    -- }

    -- Metadata
    tags              JSONB,                  -- searchable tags
    version           INTEGER DEFAULT 1,
    created_at        TIMESTAMPTZ DEFAULT now(),
    updated_at        TIMESTAMPTZ DEFAULT now()
);
```

### Two Reader Standards

Based on analysis, there are **two patterns** for how readers handle sensors:

#### Standard A: Endpoint-Based (Modbus TCP, OPC-UA, HTTP)
- **One reader container per endpoint** (IP:port)
- Multiple sensors connect through the same reader
- Each sensor has its own register addresses / node IDs
- Container reads from ONE endpoint, handles many measurements

```
[Reader: Modbus @ 192.168.1.50:502]
  ├── Sensor: PM5100_Rack1 (registers 3000-3020)
  ├── Sensor: PM5100_Rack2 (registers 3000-3020, unit_id=2)
  └── Sensor: Custom_Meter  (registers 100-110)
```

#### Standard B: Multi-Target (SNMP, MQTT)
- **One reader container handles multiple targets**
- SNMP: one container polls many IPs (each IP = one sensor/device)
- MQTT: one container subscribes to many topics (each topic = one sensor)
- Container manages connections to many endpoints internally

```
[Reader: SNMP]
  ├── Sensor: UPS_A @ 10.0.0.50 (OID map: apc-smart-ups)
  ├── Sensor: UPS_B @ 10.0.0.51 (OID map: apc-smart-ups)
  └── Sensor: PDU_1 @ 10.0.0.60 (OID map: raritan-pdu)

[Reader: MQTT @ broker:1883]
  ├── Sensor: TempSensor1 (topic: factory/floor1/temp)
  ├── Sensor: TempSensor2 (topic: factory/floor2/temp)
  └── Sensor: PowerMeter  (topic: factory/power/main)
```

### Template Examples

#### Modbus TCP Template (Standard A)

```json
{
    "name": "Schneider PM5100",
    "protocol": "modbus_tcp",
    "manufacturer": "Schneider Electric",
    "model": "PM5100",

    "reader_config_schema": {
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "port": {"type": "integer", "title": "Port", "default": 502},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (s)", "default": 20, "minimum": 1},
            "timeout_ms": {"type": "integer", "title": "Timeout (ms)", "default": 3000}
        },
        "required": ["host", "port"]
    },

    "sensor_config": {
        "registers": [
            {"field_key": "active_power_w",  "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W",   "table": "Measurements"},
            {"field_key": "voltage_ll_v",    "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0, "unit": "V",   "table": "Measurements"},
            {"field_key": "current_a",       "register_type": "Holding", "address": 3002, "data_type": "float32", "scale": 1.0, "unit": "A",   "table": "Measurements"},
            {"field_key": "energy_kwh",      "register_type": "Holding", "address": 3060, "data_type": "float32", "scale": 1.0, "unit": "kWh", "table": "Measurements"},
            {"field_key": "power_factor",    "register_type": "Holding", "address": 3004, "data_type": "float32", "scale": 1.0, "unit": "",    "table": "Measurements"},
            {"field_key": "frequency_hz",    "register_type": "Holding", "address": 3110, "data_type": "float32", "scale": 1.0, "unit": "Hz",  "table": "Measurements"}
        ]
    },

    "sensor_params_schema": {
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Offset", "default": 0}
        },
        "required": ["unit_id"]
    },

    "container_spec": {
        "image_suffix": "modbus-reader",
        "multi_sensor": false,
        "env_defaults": {"LOG_LEVEL": "info"},
        "volumes": ["/data:/data:ro"]
    }
}
```

#### SNMP Template (Standard B)

```json
{
    "name": "APC Smart-UPS",
    "protocol": "snmp",
    "manufacturer": "APC / Schneider Electric",
    "model": "Smart-UPS",

    "reader_config_schema": {
        "type": "object",
        "properties": {
            "fetch_interval_sec": {"type": "integer", "title": "Fetch Interval (s)", "default": 15},
            "timeout_sec": {"type": "integer", "title": "Timeout (s)", "default": 10},
            "worker_count": {"type": "integer", "title": "Worker Threads", "default": 2}
        }
    },

    "sensor_config": {
        "oids": [
            {"field_key": "battery_voltage",    "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.8.0",  "unit": "V",   "table": "Measurements"},
            {"field_key": "battery_capacity",   "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.1.0",  "unit": "%",   "table": "Measurements"},
            {"field_key": "output_load",        "oid": ".1.3.6.1.4.1.318.1.1.1.4.2.3.0",  "unit": "%",   "table": "Measurements"},
            {"field_key": "runtime_remaining",  "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.3.0",  "unit": "min", "table": "Measurements"}
        ]
    },

    "sensor_params_schema": {
        "type": "object",
        "properties": {
            "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "community": {"type": "string", "title": "SNMP Community", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["2c", "3"], "default": "2c"}
        },
        "required": ["ip_address"]
    },

    "container_spec": {
        "image_suffix": "snmp-reader",
        "multi_sensor": true,
        "env_defaults": {"LOG_LEVEL": "info"},
        "volumes": ["/data:/data:ro"]
    }
}
```

#### MQTT Template (Standard B)

```json
{
    "name": "Generic MQTT JSON Sensor",
    "protocol": "mqtt",

    "reader_config_schema": {
        "type": "object",
        "properties": {
            "broker_host": {"type": "string", "title": "MQTT Broker Host"},
            "broker_port": {"type": "integer", "title": "Broker Port", "default": 1883},
            "username": {"type": "string", "title": "Username"},
            "password": {"type": "string", "title": "Password", "format": "password"},
            "client_id": {"type": "string", "title": "Client ID"}
        },
        "required": ["broker_host"]
    },

    "sensor_config": {
        "json_paths": [
            {"field_key": "temperature", "json_path": "$.data.temperature", "unit": "°C", "table": "Measurements"},
            {"field_key": "humidity",    "json_path": "$.data.humidity",    "unit": "%",  "table": "Measurements"}
        ]
    },

    "sensor_params_schema": {
        "type": "object",
        "properties": {
            "topic": {"type": "string", "title": "MQTT Topic", "description": "e.g. factory/floor1/sensor_001"},
            "qos": {"type": "integer", "title": "QoS", "enum": [0, 1, 2], "default": 1},
            "payload_format": {"type": "string", "title": "Payload Format", "enum": ["json", "senml", "sparkplugb"], "default": "json"}
        },
        "required": ["topic"]
    },

    "container_spec": {
        "image_suffix": "mqtt-reader",
        "multi_sensor": true,
        "env_defaults": {"LOG_LEVEL": "info"},
        "volumes": ["/data:/data:ro"]
    }
}
```

#### HTTP Reader Template (NEW — Standard B)

```json
{
    "name": "Generic HTTP JSON Endpoint",
    "protocol": "http",

    "reader_config_schema": {
        "type": "object",
        "properties": {
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (s)", "default": 30}
        }
    },

    "sensor_config": {
        "json_paths": [
            {"field_key": "value", "json_path": "$.value", "unit": "", "table": "Measurements"}
        ]
    },

    "sensor_params_schema": {
        "type": "object",
        "properties": {
            "url": {"type": "string", "title": "Endpoint URL", "format": "uri"},
            "method": {"type": "string", "title": "HTTP Method", "enum": ["GET", "POST"], "default": "GET"},
            "headers_json": {"type": "string", "title": "Custom Headers (JSON)"},
            "body_template": {"type": "string", "title": "Request Body Template"},
            "auth_type": {"type": "string", "title": "Auth Type", "enum": ["none", "basic", "bearer", "api_key"], "default": "none"},
            "auth_credentials": {"type": "string", "title": "Credentials", "format": "password"}
        },
        "required": ["url"]
    },

    "container_spec": {
        "image_suffix": "http-reader",
        "multi_sensor": true,
        "env_defaults": {"LOG_LEVEL": "info"},
        "volumes": ["/data:/data:ro"]
    }
}
```

---

## 9. Reader (Gateway) Standardization

### Standard Reader Interface

Every reader container MUST implement:

```go
// Standard reader contract
type Reader interface {
    // Initialize from SQLite
    Init(db *sql.DB, readerID string) error

    // Start reading data
    Run(ctx context.Context) error

    // Handle config changes (version bump in SQLite)
    Reload() error

    // Graceful shutdown
    Stop() error
}
```

### Standard Reader Startup

```go
func main() {
    readerID := os.Getenv("READER_ID")     // Set by conf-agent in container env
    dbPath := os.Getenv("SQLITE_PATH")     // Default: /data/qube.db
    coreSwitchURL := os.Getenv("CORESWITCH_URL")  // Default: http://core-switch:8585

    db, _ := sql.Open("sqlite3", dbPath + "?mode=ro")

    // Load reader config from SQLite
    reader := NewModbusReader(db, readerID, coreSwitchURL)
    reader.Init()

    // Watch for config changes
    go reader.WatchConfigChanges()  // polls SQLite version column

    // Run
    reader.Run(ctx)
}
```

### Standard Output Format

All readers output to core-switch using the same JSON format:

```json
{
    "batch": [
        {
            "equipment": "PM5100_Rack1",
            "reading": "active_power_w",
            "value": 1250.5,
            "table": "Measurements",
            "tags": {"name": "PM5100_Rack1", "location": "rack1"},
            "timestamp": "2026-03-26T10:30:00Z"
        }
    ]
}
```

### Reader Container Environment Variables (Standard)

| Variable | Required | Description |
|----------|----------|-------------|
| `READER_ID` | Yes | UUID of this reader in SQLite |
| `SQLITE_PATH` | No | Path to SQLite DB (default: `/data/qube.db`) |
| `CORESWITCH_URL` | No | Core-switch endpoint (default: `http://core-switch:8585`) |
| `LOG_LEVEL` | No | `debug`, `info`, `warn`, `error` (default: `info`) |

### Shared Reader Library

Create a shared Go module `pkg/reader` that all readers import:

```
pkg/
└── reader/
    ├── config.go       # SQLite config loading
    ├── output.go       # Core-switch HTTP client (batch POST)
    ├── watcher.go      # SQLite version change watcher
    ├── senml.go        # SenML encoding/decoding
    └── logger.go       # Standard logging
```

---

## 10. Core-Switch v2

### Current Core-Switch

- Receives data from readers via HTTP POST
- Writes to InfluxDB
- Publishes to internal MQTT (for inter-service communication)

### v2 Changes

Add a **third output path**: live data via webhook.

```
Reader → POST /v3/batch → Core-Switch
                              │
                              ├── InfluxDB (always, for edge buffering)
                              ├── Internal MQTT / EMQX (configurable)
                              └── Live webhook (configurable, future)
```

### Core-Switch Settings (from SQLite)

```go
// Core-switch reads settings from SQLite on startup + on change
type CoreSwitchConfig struct {
    Outputs struct {
        InfluxDB struct {
            Enabled bool   `json:"enabled"`
            URL     string `json:"url"`
            DB      string `json:"db"`
        } `json:"influxdb"`
        MQTT struct {
            Enabled     bool   `json:"enabled"`
            Broker      string `json:"broker"`
            TopicPrefix string `json:"topic_prefix"`
        } `json:"mqtt"`
        Live struct {
            Enabled    bool   `json:"enabled"`
            WebhookURL string `json:"webhook_url"`  // Future: push to frontend
        } `json:"live"`
    } `json:"outputs"`
    BatchSize       int `json:"batch_size"`
    FlushIntervalMs int `json:"flush_interval_ms"`
}
```

### Control API (:8082)

New API for managing core-switch settings remotely:

```
GET  /api/v1/qubes/{id}/coreswitch/settings    # Get current settings
PUT  /api/v1/qubes/{id}/coreswitch/settings    # Update settings
POST /api/v1/qubes/{id}/coreswitch/outputs/{output}/toggle  # Enable/disable output
```

Settings changes are pushed to the Qube via webhook → written to SQLite → core-switch picks up.

---

## 11. Protocol Standards & Libraries

### Go Libraries (Per Protocol)

| Protocol | Library | Maturity | Notes |
|----------|---------|----------|-------|
| Modbus TCP | `github.com/grid-x/modbus` | Production | Modern, maintained |
| Modbus RTU | `github.com/grid-x/modbus` | Production | Serial support |
| OPC-UA | `github.com/gopcua/opcua` | Production | Full OPC-UA client |
| SNMP | `github.com/gosnmp/gosnmp` | Production | v1/v2c/v3, walk/bulk |
| MQTT | `github.com/eclipse/paho.mqtt.golang` | Production | v3.1.1 + v5.0 |
| HTTP | `net/http` (stdlib) | Production | No external dep needed |
| S7 (future) | `github.com/robinson/gos7` | Moderate | Siemens PLCs |
| BACnet (future) | TBD | Low | Building automation |
| LoRaWAN (future) | ChirpStack API client | Moderate | Via network server |

### Why Not Apache PLC4X for Go?

- Go implementation is **not production-ready** (low maturity)
- Auto-generated code produces non-idiomatic Go
- Java is the only mature PLC4X implementation
- Individual Go libraries are more stable and better maintained
- Our per-container architecture already provides protocol isolation

### Telemetry Standard: SenML (RFC 8428)

All telemetry payloads between influx-to-sql → TP-API use SenML:

```json
[
    {"bn": "urn:qube:Q-1001:PM5100_Rack1:", "bt": 1711446600, "bu": "W"},
    {"n": "active_power_w", "v": 1250.5},
    {"n": "voltage_ll_v", "v": 230.1, "u": "V"},
    {"n": "current_a", "v": 5.43, "u": "A"}
]
```

Benefits:
- IETF standard (interoperable)
- Compact (base name/time/unit reduce repetition)
- Well-defined unit registry
- Go library: `github.com/mainflux/senml`

---

## 12. Docker Swarm File Generation

### v2: Stored in Database

Instead of string concatenation in sync.go, the Swarm compose file is:
1. **Generated** when config changes
2. **Stored** in the `swarm_files` table in Management DB
3. **Pushed** to Qube via webhook
4. **Stored** in SQLite `swarm_state` table
5. **Applied** by conf-agent via `docker stack deploy`

### Generation Logic

```go
func generateSwarmFile(pool *pgxpool.Pool, qubeID string, registry string) (string, error) {
    // 1. Query all active containers for this qube
    // 2. For each container:
    //    - Resolve image from registry + container_spec.image_suffix + arch
    //    - Add environment: READER_ID, SQLITE_PATH, CORESWITCH_URL
    //    - Mount shared SQLite volume (read-only for readers)
    // 3. Always include: conf-agent, influx-to-sql, core-switch, influxdb, emqx
    // 4. Generate docker-compose.yml string
    // 5. Store in DB
}
```

### Generated Swarm File (Example)

```yaml
version: "3.8"

networks:
  qube-net:
    driver: overlay
    attachable: true

volumes:
  qube-data:
    driver: local
  influx-data:
    driver: local

services:
  # === Infrastructure ===

  enterprise-conf-agent:
    image: ${REGISTRY}/enterprise-conf-agent:${ARCH}.latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - qube-data:/opt/qube/data
    environment:
      QUBE_ID: Q-1001
      SQLITE_PATH: /opt/qube/data/qube.db
      TPAPI_URL: https://cloud.example.com:8081
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  core-switch:
    image: ${REGISTRY}/core-switch:${ARCH}.latest
    volumes:
      - qube-data:/data:ro
    environment:
      SQLITE_PATH: /data/qube.db
    ports:
      - "8585:8585"
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  influxdb:
    image: influxdb:1.8
    volumes:
      - influx-data:/var/lib/influxdb
    environment:
      INFLUXDB_DB: edgex
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  emqx:
    image: emqx/emqx:5-slim
    ports:
      - "1883:1883"
      - "18083:18083"
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  enterprise-influx-to-sql:
    image: ${REGISTRY}/enterprise-influx-to-sql:${ARCH}.latest
    volumes:
      - qube-data:/data:ro
    environment:
      SQLITE_PATH: /data/qube.db
      INFLUX_URL: http://influxdb:8086
      INFLUX_DB: edgex
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  # === Readers (dynamically generated) ===

  modbus-reader-panel-a:
    image: ${REGISTRY}/modbus-reader:${ARCH}.latest
    volumes:
      - qube-data:/data:ro
    environment:
      READER_ID: "uuid-reader-1"
      SQLITE_PATH: /data/qube.db
      CORESWITCH_URL: http://core-switch:8585
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  snmp-reader:
    image: ${REGISTRY}/snmp-reader:${ARCH}.latest
    volumes:
      - qube-data:/data:ro
    environment:
      READER_ID: "uuid-reader-2"
      SQLITE_PATH: /data/qube.db
      CORESWITCH_URL: http://core-switch:8585
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]
```

---

## 13. Auto-Discovery Flow

For situations where a customer brings a new device and no template exists.

### Step 1: Start Discovery Container

```
POST /api/v1/qubes/{qube_id}/discover
{
    "protocol": "modbus_tcp",
    "target": {"host": "192.168.1.100", "port": 502},
    "duration_sec": 30,
    "options": {
        "register_range": "1-10000",
        "register_types": ["Holding", "Input"]
    }
}
```

Cloud pushes a temporary discovery container to the Qube.

### Step 2: Discovery Container Scans

The discovery container:
- **Modbus**: Scans register ranges, identifies which registers return valid data
- **SNMP**: Performs SNMP walk, collects all available OIDs
- **MQTT**: Subscribes to wildcard topic, collects all received messages
- **OPC-UA**: Browses the node tree, lists all available nodes
- **HTTP**: GETs the endpoint, parses JSON structure

### Step 3: Results Returned

```json
{
    "discovery_id": "uuid",
    "protocol": "modbus_tcp",
    "status": "completed",
    "discovered": [
        {"address": 3000, "register_type": "Holding", "raw_value": 2301, "data_type": "uint16", "suggested_field": "register_3000"},
        {"address": 3002, "register_type": "Holding", "raw_value": 543,  "data_type": "uint16", "suggested_field": "register_3002"},
        {"address": 3004, "register_type": "Holding", "raw_value": 1250, "data_type": "uint16", "suggested_field": "register_3004"}
    ]
}
```

### Step 4: User Maps Fields

UI shows discovered data points. User maps each to a meaningful name:

```
register_3000 → "voltage_v" (unit: V)
register_3002 → "current_a" (unit: A)
register_3004 → "active_power_w" (unit: W)
```

### Step 5: Template Created

System generates a new template from the mapping:

```
POST /api/v1/templates/from-discovery
{
    "discovery_id": "uuid",
    "name": "Custom Power Meter",
    "mappings": [
        {"address": 3000, "field_key": "voltage_v", "unit": "V", "data_type": "float32", "scale": 0.1},
        {"address": 3002, "field_key": "current_a", "unit": "A", "data_type": "float32", "scale": 0.01},
        {"address": 3004, "field_key": "active_power_w", "unit": "W", "data_type": "float32", "scale": 1.0}
    ]
}
```

### Step 6: Template Available for Reuse

The created template becomes an org-level template. Can be promoted to global by superadmin.

---

## 14. Migration Strategy (SQL)

### v2 Migration Files

```
cloud/migrations/
├── 001_init.sql                  # All tables (management DB)
├── 002_global_templates.sql      # Global protocol templates + protocol definitions
├── 003_test_seeds.sql            # Test data (dev/staging only, NOT for production)
└── 004_telemetry_init.sql        # Telemetry DB schema + partition functions
```

### 001_init.sql — All Management Tables

```sql
-- =============================================
-- 001_init.sql — Management Database Schema
-- =============================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Organisations
CREATE TABLE organisations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    org_secret  TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    mqtt_namespace TEXT,
    created_at  TIMESTAMPTZ DEFAULT now()
);

-- Users
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organisations(id),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('superadmin','admin','editor','viewer')),
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Qubes
CREATE TABLE qubes (
    id              TEXT PRIMARY KEY,  -- Q-1001
    org_id          UUID REFERENCES organisations(id),
    register_key    TEXT NOT NULL,
    maintain_key    TEXT,
    auth_token_hash TEXT,
    device_type     TEXT DEFAULT 'arm64',
    webhook_url     TEXT,              -- NEW: where to push config changes
    capabilities    JSONB DEFAULT '[]', -- NEW: ["webhook", "sqlite"]
    last_seen       TIMESTAMPTZ,
    status          TEXT DEFAULT 'unclaimed'
                    CHECK (status IN ('unclaimed','online','offline')),
    claimed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_qubes_register_key ON qubes(register_key);
CREATE INDEX idx_qubes_org_id ON qubes(org_id);

-- Protocols
CREATE TABLE protocols (
    id                      TEXT PRIMARY KEY,  -- modbus_tcp, snmp, mqtt, opcua, http
    label                   TEXT NOT NULL,
    default_port            INTEGER,
    reader_standard         TEXT NOT NULL CHECK (reader_standard IN ('endpoint', 'multi_target')),
    -- endpoint = one container per host:port (Modbus, OPC-UA, HTTP)
    -- multi_target = one container handles many targets (SNMP, MQTT)
    created_at              TIMESTAMPTZ DEFAULT now()
);

-- Protocol Templates (unified)
CREATE TABLE protocol_templates (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL,
    protocol              TEXT NOT NULL REFERENCES protocols(id),
    manufacturer          TEXT,
    model                 TEXT,
    description           TEXT,
    org_id                UUID REFERENCES organisations(id),  -- NULL = global
    is_global             BOOLEAN DEFAULT false,
    reader_config_schema  JSONB NOT NULL,
    sensor_config         JSONB NOT NULL,
    sensor_params_schema  JSONB NOT NULL,
    container_spec        JSONB NOT NULL,
    tags                  JSONB,
    version               INTEGER DEFAULT 1,
    created_at            TIMESTAMPTZ DEFAULT now(),
    updated_at            TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_templates_protocol ON protocol_templates(protocol);
CREATE INDEX idx_templates_org ON protocol_templates(org_id);
CREATE INDEX idx_templates_global ON protocol_templates(is_global) WHERE is_global = true;

-- Readers (formerly gateways)
CREATE TABLE readers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    name        TEXT NOT NULL,
    protocol    TEXT NOT NULL REFERENCES protocols(id),
    config_json JSONB NOT NULL,      -- reader-specific config (host, port, etc.)
    template_id UUID REFERENCES protocol_templates(id),
    status      TEXT DEFAULT 'active' CHECK (status IN ('active','disabled')),
    version     INTEGER DEFAULT 1,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_readers_qube ON readers(qube_id);

-- Sensors
CREATE TABLE sensors (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reader_id     UUID NOT NULL REFERENCES readers(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    template_id   UUID REFERENCES protocol_templates(id),
    config_json   JSONB NOT NULL,    -- sensor-specific config (registers, OIDs, topics, etc.)
    params_json   JSONB,             -- user-provided params (unit_id, ip, topic, etc.)
    tags_json     JSONB,
    status        TEXT DEFAULT 'active' CHECK (status IN ('active','disabled')),
    version       INTEGER DEFAULT 1,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_sensors_reader ON sensors(reader_id);

-- Containers (Docker services per qube)
CREATE TABLE containers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    reader_id   UUID UNIQUE REFERENCES readers(id),  -- nullable for infra containers
    name        TEXT NOT NULL,       -- Docker service name
    image       TEXT NOT NULL,
    env_json    JSONB,
    volumes_json JSONB,
    status      TEXT DEFAULT 'active' CHECK (status IN ('active','disabled')),
    is_infrastructure BOOLEAN DEFAULT false,  -- conf-agent, core-switch, influxdb, emqx
    version     INTEGER DEFAULT 1,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_containers_qube ON containers(qube_id);

-- Config State (hash tracking)
CREATE TABLE config_state (
    qube_id         TEXT PRIMARY KEY REFERENCES qubes(id),
    config_hash     TEXT,
    config_version  INTEGER DEFAULT 0,
    compose_yml     TEXT,            -- Generated docker-compose.yml
    last_pushed_at  TIMESTAMPTZ,
    last_acked_at   TIMESTAMPTZ
);

-- Swarm file history
CREATE TABLE swarm_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    compose_yml TEXT NOT NULL,
    config_hash TEXT NOT NULL,
    version     INTEGER NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT now()
);

-- Commands
CREATE TABLE qube_commands (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    command     TEXT NOT NULL,
    payload     JSONB,
    status      TEXT DEFAULT 'pending'
                CHECK (status IN ('pending','dispatched','executed','failed','timeout')),
    result      JSONB,
    created_at  TIMESTAMPTZ DEFAULT now(),
    dispatched_at TIMESTAMPTZ,
    executed_at TIMESTAMPTZ
);
CREATE INDEX idx_commands_pending ON qube_commands(qube_id, status) WHERE status IN ('pending','dispatched');

-- Webhook delivery log
CREATE TABLE webhook_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    event       TEXT NOT NULL,
    payload     JSONB,
    status      TEXT DEFAULT 'pending'
                CHECK (status IN ('pending','delivered','failed','expired')),
    attempts    INTEGER DEFAULT 0,
    last_error  TEXT,
    created_at  TIMESTAMPTZ DEFAULT now(),
    delivered_at TIMESTAMPTZ
);
CREATE INDEX idx_webhook_pending ON webhook_log(qube_id, status) WHERE status = 'pending';

-- Discovery sessions
CREATE TABLE discovery_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id),
    protocol    TEXT NOT NULL REFERENCES protocols(id),
    target      JSONB NOT NULL,
    status      TEXT DEFAULT 'running'
                CHECK (status IN ('running','completed','failed','cancelled')),
    results     JSONB,
    created_at  TIMESTAMPTZ DEFAULT now(),
    completed_at TIMESTAMPTZ
);

-- Core-switch settings per qube
CREATE TABLE coreswitch_settings (
    qube_id     TEXT PRIMARY KEY REFERENCES qubes(id),
    settings    JSONB NOT NULL DEFAULT '{
        "outputs": {
            "influxdb": {"enabled": true},
            "mqtt": {"enabled": true},
            "live": {"enabled": false}
        },
        "batch_size": 100,
        "flush_interval_ms": 5000
    }',
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- Telemetry partition settings per qube
CREATE TABLE telemetry_settings (
    qube_id             TEXT PRIMARY KEY REFERENCES qubes(id),
    partition_interval  TEXT DEFAULT 'monthly'
                        CHECK (partition_interval IN ('daily','weekly','monthly')),
    retention_months    INTEGER DEFAULT 12,
    auto_create_ahead   INTEGER DEFAULT 3,
    updated_at          TIMESTAMPTZ DEFAULT now()
);
```

### 002_global_templates.sql — Protocol + Template Seed Data

```sql
-- Protocols
INSERT INTO protocols (id, label, default_port, reader_standard) VALUES
('modbus_tcp', 'Modbus TCP',  502,  'endpoint'),
('opcua',      'OPC-UA',      4840, 'endpoint'),
('http',       'HTTP/REST',   80,   'multi_target'),
('snmp',       'SNMP',        161,  'multi_target'),
('mqtt',       'MQTT',        1883, 'multi_target');

-- Global Templates (examples - full definitions with JSON schemas)
-- ... (Schneider PM5100, APC Smart-UPS, etc.)
```

### 003_test_seeds.sql — Development Only

```sql
-- Test org, users, qubes (Q-1001 through Q-1020)
-- Only loaded when SEED_TEST_DATA=true
```

### 004_telemetry_init.sql — Telemetry DB Functions

```sql
-- Run against qubedata database
-- Functions to create per-qube schemas and partition management

CREATE OR REPLACE FUNCTION create_qube_schema(p_qube_id TEXT)
RETURNS void AS $$
DECLARE
    schema_name TEXT := 'qube_' || replace(p_qube_id, '-', '_');
BEGIN
    EXECUTE format('CREATE SCHEMA IF NOT EXISTS %I', schema_name);
    EXECUTE format('
        CREATE TABLE IF NOT EXISTS %I.readings (
            ts          TIMESTAMPTZ NOT NULL,
            sensor_id   UUID NOT NULL,
            field_key   TEXT NOT NULL,
            value       DOUBLE PRECISION NOT NULL,
            unit        TEXT,
            tags        JSONB
        ) PARTITION BY RANGE (ts)', schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_%s_readings_sensor_ts ON %I.readings (sensor_id, ts DESC)',
        replace(p_qube_id, '-', '_'), schema_name);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION create_monthly_partition(p_qube_id TEXT, p_year INT, p_month INT)
RETURNS void AS $$
DECLARE
    schema_name TEXT := 'qube_' || replace(p_qube_id, '-', '_');
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    partition_name := format('readings_%s_%s', p_year, lpad(p_month::text, 2, '0'));
    start_date := make_date(p_year, p_month, 1);
    end_date := start_date + interval '1 month';

    EXECUTE format('
        CREATE TABLE IF NOT EXISTS %I.%I
        PARTITION OF %I.readings
        FOR VALUES FROM (%L) TO (%L)',
        schema_name, partition_name, schema_name,
        start_date, end_date);
END;
$$ LANGUAGE plpgsql;

-- Auto-partition maintenance (run via pg_cron or app-level scheduler)
CREATE OR REPLACE FUNCTION maintain_partitions()
RETURNS void AS $$
    -- For each qube with telemetry_settings:
    -- 1. Create partitions for next N months (auto_create_ahead)
    -- 2. Drop partitions older than retention_months
$$ LANGUAGE plpgsql;
```

---

## 15. API Surface v2

### Cloud API (:8080) — User-Facing

```
# Auth
POST   /api/v1/auth/register
POST   /api/v1/auth/login

# Org & Users
GET    /api/v1/users/me
GET    /api/v1/users
POST   /api/v1/users
PATCH  /api/v1/users/{id}
DELETE /api/v1/users/{id}

# Qubes
GET    /api/v1/qubes
GET    /api/v1/qubes/{id}
POST   /api/v1/qubes/claim
DELETE /api/v1/qubes/{id}/claim           # unclaim

# Protocols
GET    /api/v1/protocols

# Protocol Templates
GET    /api/v1/templates                  # list (filter by protocol, global/org)
GET    /api/v1/templates/{id}
POST   /api/v1/templates                  # create org template
PUT    /api/v1/templates/{id}
DELETE /api/v1/templates/{id}
POST   /api/v1/templates/{id}/clone       # clone global → org
POST   /api/v1/templates/from-discovery   # create from discovery results

# Readers (formerly gateways)
GET    /api/v1/qubes/{id}/readers
POST   /api/v1/qubes/{id}/readers
GET    /api/v1/readers/{id}
PUT    /api/v1/readers/{id}
DELETE /api/v1/readers/{id}

# Sensors
GET    /api/v1/readers/{id}/sensors
POST   /api/v1/readers/{id}/sensors
GET    /api/v1/sensors/{id}
PUT    /api/v1/sensors/{id}
DELETE /api/v1/sensors/{id}
POST   /api/v1/sensors/{id}/sync-template  # regenerate from latest template

# Containers
GET    /api/v1/qubes/{id}/containers
GET    /api/v1/containers/{id}

# Commands
POST   /api/v1/qubes/{id}/commands
GET    /api/v1/qubes/{id}/commands
GET    /api/v1/commands/{id}

# Discovery
POST   /api/v1/qubes/{id}/discover
GET    /api/v1/discovery/{id}
DELETE /api/v1/discovery/{id}             # cancel

# Telemetry & Data
GET    /api/v1/data/readings              # query with filters
GET    /api/v1/data/sensors/{id}/latest
GET    /api/v1/data/qubes/{id}/summary

# Telemetry Settings
GET    /api/v1/qubes/{id}/telemetry/settings
PUT    /api/v1/qubes/{id}/telemetry/settings

# Admin
GET    /api/v1/admin/registry
PUT    /api/v1/admin/registry
```

### TP-API (:8081) — Qube-Facing

```
# Public
POST   /v1/device/register                # self-registration with register_key

# HMAC Protected
GET    /v1/sync/state                     # returns config version + hash
GET    /v1/sync/config                    # full config snapshot (fallback)
GET    /v1/sync/changes?since_version=N   # incremental changes (NEW)
POST   /v1/heartbeat
POST   /v1/commands/poll
POST   /v1/commands/{id}/ack
POST   /v1/telemetry/ingest              # SenML format

# Webhook receiver (on Qube-side TP-API instance)
POST   /v1/webhook                        # receives push from cloud
```

### Control API (:8082) — Core-Switch Settings

```
GET    /api/v1/qubes/{id}/coreswitch/settings
PUT    /api/v1/qubes/{id}/coreswitch/settings
POST   /api/v1/qubes/{id}/coreswitch/outputs/{output}/toggle
```

> **Note:** The Control API can be merged into Cloud API (:8080) as additional routes rather than a separate port. Decision: keep it in the same binary, separate router group.

**Revised:** Control API routes live under Cloud API (:8080):

```
GET    /api/v1/qubes/{id}/coreswitch/settings
PUT    /api/v1/qubes/{id}/coreswitch/settings
```

---

## 16. Configuration Formats (JSON, not YAML)

### v1 → v2 Format Changes

| File | v1 Format | v2 Format |
|------|-----------|-----------|
| Reader config | `configs.yml` (YAML) | Stored in SQLite `readers.config_json` |
| Sensor config | CSV files | Stored in SQLite `sensors.config_json` |
| Sensor map | `sensor_map.json` | Stored in SQLite `sensor_map` table |
| Influx-to-SQL config | `configs.yml` (YAML) | Reads from SQLite |
| Docker compose | Generated string | Stored in DB + SQLite |
| Core-switch config | Hardcoded | Stored in SQLite `coreswitch_settings` |

### Reader Config (JSON, in SQLite)

**Modbus TCP:**
```json
{
    "host": "192.168.1.50",
    "port": 502,
    "poll_interval_sec": 20,
    "timeout_ms": 3000,
    "max_retries": 3
}
```

**SNMP:**
```json
{
    "fetch_interval_sec": 15,
    "timeout_sec": 10,
    "worker_count": 2
}
```

**MQTT:**
```json
{
    "broker_host": "tcp://192.168.1.10",
    "broker_port": 1883,
    "username": "mqttuser",
    "password": "password",
    "client_id": "qube-Q1001-mqtt-reader"
}
```

### Sensor Config (JSON, in SQLite)

**Modbus sensor:**
```json
{
    "registers": [
        {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0},
        {"field_key": "voltage_ll_v",   "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0}
    ],
    "unit_id": 1,
    "register_offset": 0
}
```

**SNMP sensor:**
```json
{
    "ip_address": "10.0.0.50",
    "community": "public",
    "snmp_version": "2c",
    "oids": [
        {"field_key": "battery_voltage", "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.8.0"},
        {"field_key": "output_load",     "oid": ".1.3.6.1.4.1.318.1.1.1.4.2.3.0"}
    ]
}
```

---

## 17. Implementation Phases

### Phase 0: Standards & Shared Infrastructure (Week 1)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 0.1 | Create `pkg/reader` shared library | `pkg/reader/` | Config loading, SQLite watcher, core-switch client, SenML encoder, standard logging |
| 0.2 | Create `pkg/sqlite` shared library | `pkg/sqlite/` | SQLite schema init, migration helper, read-only opener |
| 0.3 | Define standard Dockerfile template | `standards/` | Multi-stage Go build, shared base image, standard labels |
| 0.4 | Set up `EMQX` in dev compose | `docker-compose.dev.yml` | Replace mosquitto with EMQX |
| 0.5 | Document reader standards | `standards/READER_STANDARD.md` | Interface contract, env vars, lifecycle |
| 0.6 | Document template schema | `standards/TEMPLATE_STANDARD.md` | JSON Schema for all template fields |

### Phase 1: Cloud Foundation Rewrite (Weeks 2-3)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 1.1 | Write 001_init.sql | `cloud/migrations/` | All management tables (see Section 14) |
| 1.2 | Write 002_global_templates.sql | `cloud/migrations/` | Protocols + global templates |
| 1.3 | Write 003_test_seeds.sql | `cloud/migrations/` | Dev test data |
| 1.4 | Write 004_telemetry_init.sql | `cloud/migrations/` | Telemetry DB functions |
| 1.5 | Update Cloud API router | `cloud/internal/api/router.go` | New routes (/readers/, /templates/, etc.) |
| 1.6 | Rewrite reader handlers | `cloud/internal/api/readers.go` | CRUD for readers (was gateways.go) |
| 1.7 | Rewrite sensor handlers | `cloud/internal/api/sensors.go` | CRUD, remove CSV generation, add template-based config |
| 1.8 | Rewrite template handlers | `cloud/internal/api/templates.go` | Unified protocol_templates CRUD |
| 1.9 | Add container handlers | `cloud/internal/api/containers.go` | View containers per qube |
| 1.10 | Rewrite hash computation | `cloud/internal/api/hash.go` | Version-based, not CSV-based |
| 1.11 | Add webhook dispatch | `cloud/internal/api/webhook.go` | Push changes to qubes |
| 1.12 | Add discovery handlers | `cloud/internal/api/discovery.go` | Start/check/cancel discovery |
| 1.13 | Add telemetry settings | `cloud/internal/api/telemetry_settings.go` | Partition config API |
| 1.14 | Add coreswitch settings | `cloud/internal/api/coreswitch.go` | Core-switch config API |
| 1.15 | Update auth & middleware | `cloud/internal/api/` | Keep JWT, update role checks |
| 1.16 | Split DB connections | `cloud/cmd/server/main.go` | Connect to both qubedb + qubedata |
| 1.17 | Update telemetry ingest | `cloud/internal/tpapi/telemetry.go` | SenML parsing, per-qube schema writes |

### Phase 2: TP-API & Webhook System (Week 4)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 2.1 | Rewrite sync endpoints | `cloud/internal/tpapi/sync.go` | Remove CSV/YAML gen, add incremental sync |
| 2.2 | Add webhook receiver | `cloud/internal/tpapi/webhook.go` | Receive + validate + apply changes |
| 2.3 | Add incremental changes endpoint | `cloud/internal/tpapi/sync.go` | `GET /v1/sync/changes?since_version=N` |
| 2.4 | Update device registration | `cloud/internal/tpapi/router.go` | Accept webhook_url + capabilities |
| 2.5 | Implement webhook dispatch worker | `cloud/internal/webhook/` | Background goroutine, retry logic, delivery log |
| 2.6 | Swarm file generation | `cloud/internal/tpapi/compose.go` | Generate compose from DB, store in config_state |

### Phase 3: Edge Rewrite — Conf-Agent + SQLite (Weeks 5-6)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 3.1 | Add SQLite dependency | `conf-agent/` | go-sqlite3 driver |
| 3.2 | Implement SQLite schema init | `conf-agent/sqlite.go` | Create tables on first run |
| 3.3 | Implement webhook receiver | `conf-agent/webhook.go` | HTTP server on :8081, HMAC validation |
| 3.4 | Implement config applier | `conf-agent/apply.go` | Write webhook changes to SQLite |
| 3.5 | Implement deploy trigger | `conf-agent/deploy.go` | Docker stack deploy on compose change |
| 3.6 | Keep polling fallback | `conf-agent/poll.go` | 5-min interval, incremental sync |
| 3.7 | Implement heartbeat | `conf-agent/heartbeat.go` | POST /v1/heartbeat (keep) |
| 3.8 | Implement command executor | `conf-agent/commands.go` | Same as v1 but via webhook push |

### Phase 4: Reader Containers (Weeks 7-9)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 4.1 | Build modbus-reader | `modbus-reader/` | SQLite config, grid-x/modbus, standard output |
| 4.2 | Build snmp-reader | `snmp-reader/` | SQLite config, gosnmp, multi-target, standard output |
| 4.3 | Build mqtt-reader | `mqtt-reader/` | SQLite config, paho.mqtt, multi-topic, standard output |
| 4.4 | Build opcua-reader | `opcua-reader/` | SQLite config, gopcua, standard output |
| 4.5 | Build http-reader | `http-reader/` | SQLite config, net/http, multi-endpoint, standard output |
| 4.6 | Build discovery containers | `discovery/` | Per-protocol scan containers |

### Phase 5: Core-Switch & Influx-to-SQL (Week 10)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 5.1 | Update core-switch | `core-switch/` | Read config from SQLite, add live output path |
| 5.2 | Update influx-to-sql | `enterprise-influx-to-sql/` | Read sensor_map from SQLite, SenML output |
| 5.3 | Add EMQX integration | Docker compose | Replace mosquitto, configure EMQX |

### Phase 6: Testing & Documentation (Week 11)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 6.1 | Rewrite test_api.sh | `test/test_api.sh` | All new endpoints, reader/sensor/template flows |
| 6.2 | Update docker-compose.dev.yml | Root | EMQX, split DBs, SQLite volume, new reader containers |
| 6.3 | Update TESTING.md | Root | New test scenarios |
| 6.4 | Update ARCHITECTURE.md | Root | v2 architecture |
| 6.5 | Update ADDING-PROTOCOLS.md | Root | New protocol addition guide |
| 6.6 | Update DEPLOYMENT.md | Root | New deployment steps |
| 6.7 | Update UI-API-GUIDE.md | Root | New API surface |
| 6.8 | Update CLAUDE.md | Root | New codebase guide |
| 6.9 | Update README.md | Root | Quick start v2 |
| 6.10 | Update DEEP_DIVE.md | Root | Code walkthrough v2 |
| 6.11 | Create MIGRATION_GUIDE.md | Root | v1 → v2 migration |

### Phase 7: CI/CD & Registry (Week 12)

| # | Task | Component | Description |
|---|------|-----------|-------------|
| 7.1 | Update GitHub Actions | `.github/workflows/` | Build all reader containers + new services |
| 7.2 | Multi-arch builds | `.github/workflows/` | amd64 + arm64 for all containers |
| 7.3 | Docker image tagging | Dockerfiles | Standard tagging: `{service}-{arch}.{version}` |
| 7.4 | Registry abstraction | Cloud API | `QUBE_IMAGE_REGISTRY` env var (GitHub vs GitLab) |

---

## 18. Repository Structure v2

```
qube-enterprise/
├── cloud/                              # Cloud API + TP-API (single Go binary)
│   ├── cmd/server/main.go              # Entry point — starts :8080 and :8081
│   ├── internal/api/                   # Cloud API (JWT, port 8080)
│   │   ├── auth.go
│   │   ├── readers.go                  # (was gateways.go)
│   │   ├── sensors.go
│   │   ├── templates.go                # Unified protocol_templates
│   │   ├── containers.go
│   │   ├── discovery.go                # NEW
│   │   ├── coreswitch.go              # NEW
│   │   ├── telemetry.go
│   │   ├── telemetry_settings.go      # NEW
│   │   ├── hash.go
│   │   ├── webhook.go                 # NEW
│   │   ├── commands.go
│   │   ├── middleware.go
│   │   ├── qubes.go
│   │   ├── users.go
│   │   ├── protocols.go
│   │   ├── registry.go
│   │   └── router.go
│   ├── internal/tpapi/                 # TP-API (HMAC, port 8081)
│   │   ├── router.go
│   │   ├── sync.go                    # Rewritten: incremental sync
│   │   ├── webhook.go                 # NEW: webhook receiver
│   │   ├── compose.go                 # NEW: swarm file generation
│   │   ├── telemetry.go              # SenML ingest
│   │   └── commands.go
│   ├── internal/webhook/              # NEW: webhook dispatch worker
│   │   └── dispatcher.go
│   └── migrations/
│       ├── 001_init.sql
│       ├── 002_global_templates.sql
│       ├── 003_test_seeds.sql
│       └── 004_telemetry_init.sql
│
├── conf-agent/                         # Edge agent (webhook receiver + deployer)
│   ├── main.go
│   ├── sqlite.go                      # NEW: SQLite schema init + writes
│   ├── webhook.go                     # NEW: HTTP server for webhook
│   ├── apply.go                       # NEW: apply changes to SQLite
│   ├── deploy.go                      # Docker stack deploy
│   ├── poll.go                        # Fallback polling
│   ├── heartbeat.go
│   └── commands.go
│
├── pkg/                               # NEW: shared Go libraries
│   ├── reader/                        # Standard reader library
│   │   ├── config.go                  # SQLite config loading
│   │   ├── output.go                  # Core-switch HTTP client
│   │   ├── watcher.go                 # Config version watcher
│   │   ├── senml.go                   # SenML encoding
│   │   └── logger.go
│   └── sqlite/                        # SQLite helpers
│       ├── schema.go                  # Schema definition
│       └── open.go                    # Connection helpers
│
├── modbus-reader/                     # (was modbus-gateway)
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── snmp-reader/                       # (was snmp-gateway)
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── mqtt-reader/                       # (was mqtt-gateway)
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── opcua-reader/                      # NEW
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── http-reader/                       # NEW
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── core-switch/                       # Updated: reads SQLite, live output path
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── enterprise-influx-to-sql/          # Updated: reads SQLite sensor_map
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
│
├── discovery/                         # NEW: protocol discovery containers
│   ├── modbus-discover/main.go
│   ├── snmp-discover/main.go
│   ├── mqtt-discover/main.go
│   ├── opcua-discover/main.go
│   └── http-discover/main.go
│
├── standards/                         # NEW: development standards
│   ├── READER_STANDARD.md            # Reader container contract
│   ├── TEMPLATE_STANDARD.md          # Template JSON schema docs
│   ├── SENML_FORMAT.md               # Telemetry format guide
│   └── SQLITE_SCHEMA.md             # Edge SQLite schema reference
│
├── scripts/
│   ├── setup-cloud.sh
│   ├── setup-qube.sh
│   └── write-to-database.sh          # Updated for v2
│
├── test/
│   ├── test_api.sh                   # Rewritten for v2
│   ├── mit.txt
│   └── emqx/                         # NEW (replaces mosquitto/)
│       └── emqx.conf
│
├── test-ui/index.html                # Updated for v2 API
├── docker-compose.dev.yml            # Updated: EMQX, split DBs, SQLite, readers
├── CLAUDE.md                         # Updated
├── ARCHITECTURE.md                   # Updated
├── DEEP_DIVE.md                      # Updated
├── DEPLOYMENT.md                     # Updated
├── TESTING.md                        # Updated
├── ADDING-PROTOCOLS.md               # Updated
├── UI-API-GUIDE.md                   # Updated
├── README.md                         # Updated
├── MIGRATION_GUIDE.md                # NEW: v1 → v2
└── QUBE_ENTERPRISE_V2_ARCHITECTURE.md  # This document
```

---

## 19. Standards Reference

### SenML (RFC 8428) — Telemetry Format

All telemetry between influx-to-sql and TP-API:

```json
[
    {"bn": "urn:qube:Q-1001:", "bt": 1711446600},
    {"n": "PM5100_Rack1.active_power_w", "v": 1250.5, "u": "W"},
    {"n": "PM5100_Rack1.voltage_ll_v", "v": 230.1, "u": "V"}
]
```

### JSON Schema (Draft 2020-12) — Template Forms

All `reader_config_schema` and `sensor_params_schema` fields use JSON Schema. The UI renders forms dynamically from these schemas (using libraries like `react-jsonschema-form`).

### Reader Standard — Two Types

| Standard | Type | Protocols | Container Behavior |
|----------|------|-----------|-------------------|
| **A: Endpoint** | One reader ↔ One endpoint | Modbus TCP, OPC-UA | One container per IP:port |
| **B: Multi-Target** | One reader ↔ Many targets | SNMP, MQTT, HTTP | One container handles all sensors |

### EMQX Configuration

Replace Mosquitto with EMQX for:
- Better clustering support
- Built-in dashboard (port 18083)
- Rule engine for data routing
- WebSocket support (for future live data to frontend)
- Better performance under load

---

## 20. Testing & Documentation Updates

### Test Suite Rewrite (test_api.sh)

The test suite needs full rewrite covering:

1. **Auth tests** — register, login, token refresh
2. **Qube tests** — claim, unclaim, webhook URL registration
3. **Protocol tests** — list protocols
4. **Template tests** — CRUD, clone, from-discovery
5. **Reader tests** — CRUD (was gateway), auto-container creation
6. **Sensor tests** — CRUD, template-based config generation
7. **Container tests** — list per qube
8. **Webhook tests** — config push, delivery verification
9. **Sync tests** — state check, full sync, incremental sync
10. **Command tests** — dispatch, poll, ack
11. **Telemetry tests** — SenML ingest, per-qube schema, query
12. **Discovery tests** — start scan, get results, create template
13. **Coreswitch tests** — get/set settings
14. **Telemetry settings tests** — partition config
15. **Integration tests** — end-to-end: add reader+sensor → webhook push → SQLite updated → reader picks up config

### Documentation Files to Update

| File | Changes |
|------|---------|
| `CLAUDE.md` | Full rewrite — new structure, new patterns, new routes |
| `ARCHITECTURE.md` | v2 diagrams, SQLite, webhook, split DB |
| `DEEP_DIVE.md` | New code walkthrough |
| `DEPLOYMENT.md` | EMQX, split DBs, SQLite setup |
| `TESTING.md` | New test scenarios |
| `ADDING-PROTOCOLS.md` | New: create template + reader container |
| `UI-API-GUIDE.md` | All new API endpoints |
| `README.md` | v2 quick start |

### New Documentation

| File | Purpose |
|------|---------|
| `MIGRATION_GUIDE.md` | v1 → v2 migration steps |
| `standards/READER_STANDARD.md` | Reader container contract |
| `standards/TEMPLATE_STANDARD.md` | Template JSON schema guide |
| `standards/SENML_FORMAT.md` | Telemetry format reference |
| `standards/SQLITE_SCHEMA.md` | Edge SQLite schema reference |

---

## 21. CI/CD & Registry Considerations

### Image Naming Convention

```
{registry}/{service}:{arch}.{version}
```

Examples:
```
ghcr.io/sandun-s/qube-enterprise-home:modbus-reader-arm64.latest
ghcr.io/sandun-s/qube-enterprise-home:modbus-reader-amd64.v2.1.0
registry.gitlab.com/iot-team4/product:enterprise-cloud-api-amd64.latest
```

### Registry Abstraction

`QUBE_IMAGE_REGISTRY` env var controls the base registry path. This is the ONLY thing that changes between GitHub and GitLab:

```bash
# GitHub (dev/staging)
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home

# GitLab (production)
QUBE_IMAGE_REGISTRY=registry.gitlab.com/iot-team4/product
```

The cloud-api uses this when generating Swarm compose files.

### Containers to Build

| Container | Architectures | Base |
|-----------|---------------|------|
| enterprise-cloud-api | amd64 | Go multi-stage |
| enterprise-conf-agent | arm64, amd64 | Go multi-stage |
| enterprise-influx-to-sql | arm64, amd64 | Go multi-stage |
| core-switch | arm64, amd64 | Go multi-stage |
| modbus-reader | arm64, amd64 | Go multi-stage |
| snmp-reader | arm64, amd64 | Go multi-stage |
| mqtt-reader | arm64, amd64 | Go multi-stage |
| opcua-reader | arm64, amd64 | Go multi-stage |
| http-reader | arm64, amd64 | Go multi-stage |
| modbus-discover | arm64, amd64 | Go multi-stage |
| snmp-discover | arm64, amd64 | Go multi-stage |

---

## 22. Open Questions & Decisions

### Decided

| Question | Decision | Rationale |
|----------|----------|-----------|
| PLC4X for Go? | **No** — use per-protocol libraries | PLC4X Go is not production-ready |
| MQTT broker? | **EMQX** | Production-grade, dashboard, clustering |
| Telemetry format? | **SenML (RFC 8428)** | IETF standard, compact, Go library available |
| Config format? | **JSON everywhere** | Consistency, no YAML parsing needed |
| Reader standards? | **Two types** (endpoint + multi-target) | Matches protocol behavior naturally |
| Control API port? | **Merged into :8080** | Simpler — same binary, route group |

### Needs Discussion

| Question | Options | Recommendation |
|----------|---------|----------------|
| SQLite WAL mode? | WAL (concurrent reads) vs Journal | **WAL** — allows readers to read while conf-agent writes |
| SQLite on volume vs tmpfs? | Persistent volume vs tmpfs | **Volume** — survive container restarts |
| Webhook retry strategy? | Exponential backoff vs fixed interval | **Exponential** — 1s, 2s, 4s, 8s, max 5 min |
| Max webhook payload size? | Full entity vs diff | **Full entity** per change — simpler, idempotent |
| Reader config watch interval? | Fixed vs configurable | **30s fixed** — simple, good enough |
| Telemetry DB: TimescaleDB? | Native partitioning vs TimescaleDB | **Native partitioning** — no extra dependency |
| How to handle core-switch repo? | In this repo vs separate | **In this repo** — unified build |
| Go workspace vs go.work? | Single module vs multi-module | **go.work** — each reader has own go.mod, shared pkg/ |

---

## Summary: What to Build, In Order

```
Phase 0 (Week 1)   → Standards, shared libraries, EMQX setup
Phase 1 (Weeks 2-3) → Cloud API rewrite (new DB schema, readers, templates, webhook)
Phase 2 (Week 4)    → TP-API rewrite (incremental sync, webhook receiver)
Phase 3 (Weeks 5-6) → Conf-Agent rewrite (SQLite, webhook, deploy)
Phase 4 (Weeks 7-9) → All 5 reader containers (modbus, snmp, mqtt, opcua, http)
Phase 5 (Week 10)   → Core-switch v2, influx-to-sql v2
Phase 6 (Week 11)   → Full test suite, all documentation
Phase 7 (Week 12)   → CI/CD, multi-arch builds, registry config
```

**Total: ~12 weeks for the full v2 rewrite.**
