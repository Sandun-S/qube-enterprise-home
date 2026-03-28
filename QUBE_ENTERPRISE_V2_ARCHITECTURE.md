# Qube Enterprise v2 — Complete Architecture & Implementation Plan

> **Date**: 2026-03-28 (Updated)
> **Status**: Planning — Revised after feedback
> **Scope**: Full architectural rewrite of all Qube Enterprise services

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Key Changes Summary Table](#2-key-changes-summary-table)
3. [Naming Convention Changes](#3-naming-convention-changes)
4. [System Architecture v2](#4-system-architecture-v2)
5. [WebSocket Communication](#5-websocket-communication)
6. [Edge Architecture — SQLite](#6-edge-architecture--sqlite)
7. [How Readers Use SQLite (Config Reload via Docker API)](#7-how-readers-use-sqlite)
8. [Cloud Architecture — Split Databases + TimescaleDB](#8-cloud-architecture--split-databases--timescaledb)
9. [Template System — Device Templates + Reader Templates](#9-template-system--device-templates--reader-templates)
10. [Reader Standardization](#10-reader-standardization)
11. [Core-Switch v2](#11-core-switch-v2)
12. [Protocol Standards & Libraries](#12-protocol-standards--libraries)
13. [Docker Swarm File Generation](#13-docker-swarm-file-generation)
14. [Auto-Discovery Flow](#14-auto-discovery-flow)
15. [Migration Strategy (SQL)](#15-migration-strategy-sql)
16. [API Surface v2](#16-api-surface-v2)
17. [Implementation Phases](#17-implementation-phases)
18. [Repository Structure v2](#18-repository-structure-v2)
19. [Testing & Documentation Updates](#19-testing--documentation-updates)
20. [CI/CD & Registry Considerations](#20-cicd--registry-considerations)
21. [Existing Repos Reference](#21-existing-repos-reference)

---

## 1. Executive Summary

Qube Enterprise v2 is a ground-up architectural evolution:

- **CSV files → SQLite** on each Qube (shared Docker volume, conf-agent writes, readers read on startup)
- **Polling → WebSocket** (persistent bidirectional connection between cloud and Qube)
- **"gateway" → "reader"** (accurate naming — these read data, not route traffic)
- **Single Postgres → 2 databases** on same instance (management + telemetry with TimescaleDB)
- **Scattered templates → Split templates** (Device Template for sensors + Reader Template for containers)
- **YAML configs → JSON everywhere**
- **New HTTP reader** for REST API sensors
- **Remove internal MQTT broker from Qube** (no Grafana on Qube anymore = no need for EMQX/Mosquitto internally)
- **Core-switch outputs: `influxdb` or `live` (WebSocket)** — not MQTT
- **Reader config reload via Docker API** — conf-agent stops container, Swarm recreates it, fresh SQLite read on startup
- **Auto-discovery** — scan unknown devices, let user map fields, auto-create templates

### What Stays the Same
- Go 1.22+ (all services)
- PostgreSQL (cloud, same instance for both DBs)
- InfluxDB v1 (edge buffer — core-switch writes here)
- Docker Swarm (production) / Docker Compose (dev testing)
- JWT + HMAC (auth model)
- pgx driver (raw SQL, no ORM)
- Core-switch `/v3/batch` DataIn format (compatible with existing gateways)

---

## 2. Key Changes Summary Table

| Area | v1 (Current) | v2 (New) |
|------|-------------|----------|
| Edge config storage | CSV files + YAML configs | **SQLite database** (shared Docker volume) |
| Cloud → Qube sync | Polling (conf-agent polls every 30s) | **WebSocket** (persistent, bidirectional) |
| Fallback sync | N/A | **HTTP polling** (30s-1min, configurable via API) |
| Config format | YAML (configs.yml) | **JSON** (stored in SQLite) |
| Naming | "gateway" | **"reader"** |
| Cloud database | Single Postgres DB | **2 databases** on same Postgres (mgmt + telemetry) |
| Telemetry storage | Single `sensor_readings` table | **TimescaleDB hypertable** (auto-partitioned) |
| Internal MQTT on Qube | Mosquitto (for Grafana dashboards) | **Removed** (no Grafana on Qube) |
| MQTT broker | Mosquitto | **Only for MQTT reader** connecting to external brokers |
| Core-switch outputs | influxdb, mqtt | **influxdb, live** (live = WebSocket to cloud) |
| Sensor mapping | sensor_map.json file | **SQLite `sensor_map` table** |
| Template system | Single `sensor_templates` table | **Split: Device Template + Reader Template** |
| Config reload | Reader polls SQLite | **Docker API stop → Swarm recreates → fresh read** |
| Telemetry format | Custom JSON | **SenML (RFC 8428)** for TP-API ingest |
| Reader data → core-switch | `schema.DataIn` JSON | **Same format** (backward compatible) |

---

## 3. Naming Convention Changes

| v1 Term | v2 Term | Scope |
|---------|---------|-------|
| `gateway` | `reader` | DB tables, API routes, code, Docker images |
| `gateways` table | `readers` table | Postgres |
| `gateway_id` | `reader_id` | All FKs |
| `modbus-gateway` | `modbus-reader` | Container/image name |
| `mqtt-gateway` | `mqtt-reader` | Container/image name |
| `snmp-gateway` | `snmp-reader` | Container/image name |
| `opc-ua-gateway` | `opcua-reader` | Container/image name |
| `service_csv_rows` | *(removed)* | Replaced by SQLite |
| `services` table | `containers` table | More accurate |
| `config.csv` / `configs.yml` | *(removed)* | Data lives in SQLite |
| `sensor_map.json` | *(removed)* | SQLite `sensor_map` table |
| `conf-agent` | `enterprise-conf-agent` | Full enterprise features |

---

## 4. System Architecture v2

```
┌─────────────────────────────────────────────────────────────┐
│                        CLOUD (single server)                │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │            Cloud API Binary (:8080 + :8081)          │   │
│  │                                                      │   │
│  │  Cloud API (:8080, JWT)    TP-API (:8081, HMAC)     │   │
│  │  - User management         - Device register         │   │
│  │  - Reader/Sensor CRUD      - Sync endpoints          │   │
│  │  - Template management     - Telemetry ingest        │   │
│  │  - Commands                - Commands poll/ack       │   │
│  │  - Discovery               - Heartbeat               │   │
│  │  - Coreswitch settings                               │   │
│  │  - Telemetry queries                                 │   │
│  │                                                      │   │
│  │  WebSocket Server (:8080/ws)                         │   │
│  │  - Config push to Qubes                              │   │
│  │  - Command dispatch                                  │   │
│  │  - Live telemetry stream (from Qube → frontend)      │   │
│  │  - Heartbeat                                         │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────────┐  ┌─────────────────────────────┐  │
│  │  Management DB      │  │  Telemetry DB               │  │
│  │  (qubedb)           │  │  (qubedata)                 │  │
│  │  Postgres            │  │  Postgres + TimescaleDB    │  │
│  │                     │  │                             │  │
│  │  orgs, users, qubes │  │  readings (hypertable)     │  │
│  │  readers, sensors   │  │  auto-partitioned by time  │  │
│  │  templates, config  │  │  indexed by qube_id,       │  │
│  │  commands, etc.     │  │  sensor_id                 │  │
│  └─────────────────────┘  └─────────────────────────────┘  │
│         Same PostgreSQL instance, 2 databases               │
└─────────────────────────────┬───────────────────────────────┘
                              │
                    WebSocket (persistent, outbound from Qube)
                    + HTTP polling fallback
                              │
┌─────────────────────────────┴───────────────────────────────┐
│                     QUBE (Edge Device)                      │
│                     Docker Swarm (prod) / Compose (dev)     │
│                     Network: qube-net                       │
│                     Shared volume: qube-data                │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Enterprise Conf-Agent                               │   │
│  │  - WebSocket client → cloud (:8080/ws)               │   │
│  │  - Receives config pushes, commands                  │   │
│  │  - Writes to SQLite (ONLY writer)                    │   │
│  │  - Docker API: stop containers to trigger reload     │   │
│  │  - Fallback: HTTP polling to TP-API (:8081)          │   │
│  │  - Heartbeat sender                                  │   │
│  │  - All conf-agent lite features included             │   │
│  └──────────────────┬───────────────────────────────────┘   │
│                     │ writes                                │
│                     ▼                                       │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  SQLite DB (file on shared volume)                   │   │
│  │  /opt/qube/data/qube.db                              │   │
│  │                                                      │   │
│  │  conf-agent:      READ-WRITE                         │   │
│  │  all readers:     READ-ONLY (on startup only)        │   │
│  │  core-switch:     READ-ONLY (on startup only)        │   │
│  │  influx-to-sql:   READ-ONLY (on startup only)        │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │ Modbus   │ │ SNMP     │ │ MQTT     │ │ OPC-UA   │      │
│  │ Reader   │ │ Reader   │ │ Reader   │ │ Reader   │      │
│  │          │ │          │ │          │ │          │      │
│  │ Reads    │ │ Reads    │ │ Reads    │ │ Reads    │      │
│  │ SQLite   │ │ SQLite   │ │ SQLite   │ │ SQLite   │      │
│  │ on start │ │ on start │ │ on start │ │ on start │      │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘      │
│       │ HTTP POST   │            │             │            │
│       └──────┬──────┘────────────┘─────────────┘            │
│              ▼                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Core-Switch :8585                                   │   │
│  │  Reads settings from SQLite on startup               │   │
│  │                                                      │   │
│  │  POST /v3/batch → routes based on Output field:      │   │
│  │    "influxdb"      → InfluxDB v1                     │   │
│  │    "live"          → WebSocket to cloud (real-time)  │   │
│  │    "influxdb,live" → both                            │   │
│  │                                                      │   │
│  │  API to toggle outputs (set via cloud → SQLite):     │   │
│  │    PUT /settings {influxdb: true, live: false}       │   │
│  └──────────────────────┬───────────────────────────────┘   │
│                         │                                   │
│                         ▼                                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  InfluxDB v1 (edge buffer)                           │   │
│  └──────────────────────┬───────────────────────────────┘   │
│                         │                                   │
│                         ▼                                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Enterprise Influx-to-SQL                            │   │
│  │  Reads sensor_map from SQLite (on startup)           │   │
│  │  Queries InfluxDB → maps to sensor UUIDs             │   │
│  │  POST /v1/telemetry/ingest (SenML) to cloud TP-API  │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                             │
│  NO internal MQTT broker (removed — no Grafana on Qube)    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 5. WebSocket Communication

### Why WebSocket (not MQTT, not HTTP Webhook)

| Reason | Detail |
|--------|--------|
| Supervisor recommended | WebSocket was specifically requested |
| No internal MQTT needed | Grafana removed from Qube = no need for MQTT broker on edge |
| Bidirectional | Cloud pushes config + commands. Qube sends heartbeat + telemetry + ACKs. One connection. |
| Firewall-friendly | Outbound TCP from Qube (like HTTPS). No inbound ports needed. |
| No extra service | Cloud API serves WebSocket endpoint directly. No EMQX/broker to manage. |
| Browser native | Future: frontend connects via WebSocket for live dashboards |
| Simpler stack | Fewer containers on the Pi = less resource usage |

### WebSocket Connection Lifecycle

```
1. Qube boots → conf-agent starts
2. Conf-agent connects: ws://cloud-api:8080/ws?qube_id=Q-1001&token=HMAC_TOKEN
3. Cloud validates HMAC token (same as TP-API auth)
4. Connection established — bidirectional messaging

CLOUD → QUBE messages:
  { "type": "config.push",    "version": 42, "changes": [...] }
  { "type": "config.full",    "version": 42, "data": {...} }
  { "type": "command",        "id": "uuid", "command": "ping", "payload": {...} }
  { "type": "coreswitch.settings", "settings": {...} }

QUBE → CLOUD messages:
  { "type": "heartbeat",      "qube_id": "Q-1001", "uptime": 3600 }
  { "type": "config.ack",     "version": 42 }
  { "type": "command.result", "id": "uuid", "status": "executed", "result": {...} }
  { "type": "live.data",      "readings": [...] }  // real-time from core-switch
```

### Fallback: HTTP Polling

If WebSocket disconnects (network issue, cloud restart, etc.):

```
1. Conf-agent detects WS disconnect
2. Switches to HTTP polling mode automatically
3. Polls TP-API: GET /v1/sync/state (every 30s-60s, configurable)
4. If hash/version mismatch: GET /v1/sync/config (full snapshot)
5. Keeps trying to reconnect WebSocket in background
6. When WS reconnects → switches back to WS mode

Polling interval is configurable via API:
PUT /api/v1/qubes/{id}/settings { "poll_interval_sec": 30 }
```

### WebSocket for Live Data (Core-Switch → Cloud → Frontend)

```
Reader → POST /v3/batch (Output: "live") → Core-Switch
  ↓
Core-Switch opens WebSocket to conf-agent's live data channel
  ↓
Conf-agent forwards to cloud via existing WebSocket connection
  ↓
Cloud API forwards to connected frontend WebSocket clients
  ↓
Browser dashboard shows real-time data

(This is a FUTURE feature — architecture supports it, implement later)
```

### Internal Qube Communication (No MQTT Needed)

```
Readers → HTTP POST /v3/batch → Core-Switch     (same as current)
Core-Switch → HTTP POST → InfluxDB               (same as current)
Influx-to-SQL → reads InfluxDB → HTTP POST → cloud TP-API  (same as current)

No internal pub/sub needed. All communication is direct HTTP.
```

---

## 6. Edge Architecture — SQLite

### SQLite Database Location & Access

```
Shared Docker volume: qube-data
Mount path: /opt/qube/data/qube.db
Mode: WAL (Write-Ahead Logging) for concurrent read access

Access pattern:
  enterprise-conf-agent  → READ-WRITE (the ONLY writer)
  modbus-reader          → READ-ONLY (reads once on startup)
  snmp-reader            → READ-ONLY (reads once on startup)
  mqtt-reader            → READ-ONLY (reads once on startup)
  opcua-reader           → READ-ONLY (reads once on startup)
  http-reader            → READ-ONLY (reads once on startup)
  core-switch            → READ-ONLY (reads settings on startup)
  influx-to-sql          → READ-ONLY (reads sensor_map on startup)
```

Docker volume mount in Swarm file:
```yaml
volumes:
  qube-data:
    driver: local

services:
  enterprise-conf-agent:
    volumes:
      - qube-data:/opt/qube/data          # read-write (default)

  modbus-reader-panel-a:
    volumes:
      - qube-data:/opt/qube/data:ro       # read-only

  core-switch:
    volumes:
      - qube-data:/opt/qube/data:ro       # read-only
```

### SQLite Schema (on Qube)

```sql
-- =============================================
-- QUBE EDGE SQLite DATABASE
-- File: /opt/qube/data/qube.db
-- WAL mode enabled
-- Only conf-agent writes. All others read-only.
-- =============================================

PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;

-- Qube identity & connection info
CREATE TABLE qube_identity (
    qube_id       TEXT PRIMARY KEY,
    org_id        TEXT,
    qube_token    TEXT,
    cloud_url     TEXT,       -- "wss://cloud.example.com:8080/ws"
    tp_api_url    TEXT,       -- "https://cloud.example.com:8081" (fallback)
    poll_interval_sec INTEGER DEFAULT 30,
    config_version INTEGER DEFAULT 0,
    last_sync_at  TEXT        -- ISO 8601
);

-- Reader definitions (synced from cloud)
CREATE TABLE readers (
    id            TEXT PRIMARY KEY,   -- UUID from cloud
    name          TEXT NOT NULL,
    protocol      TEXT NOT NULL,      -- modbus_tcp, snmp, mqtt, opcua, http
    config_json   TEXT NOT NULL,      -- JSON: connection config (host, port, credentials, etc.)
    status        TEXT DEFAULT 'active',
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Sensor definitions (synced from cloud)
CREATE TABLE sensors (
    id            TEXT PRIMARY KEY,   -- UUID from cloud
    reader_id     TEXT NOT NULL REFERENCES readers(id),
    name          TEXT NOT NULL,
    config_json   TEXT NOT NULL,      -- JSON: what to read (registers, OIDs, topics, nodes)
    tags_json     TEXT,               -- JSON: user-defined tags
    output        TEXT DEFAULT 'influxdb',  -- "influxdb", "live", "influxdb,live"
    table_name    TEXT DEFAULT 'Measurements',
    status        TEXT DEFAULT 'active',
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Sensor map (replaces sensor_map.json)
-- Maps InfluxDB "Equipment.Reading" → cloud sensor UUID
CREATE TABLE sensor_map (
    measurement_key TEXT PRIMARY KEY,  -- "PM5100_Rack1.active_power_w"
    sensor_id       TEXT NOT NULL,
    field_key       TEXT NOT NULL,
    unit            TEXT
);

-- Container definitions (what Docker services to run)
CREATE TABLE containers (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,      -- Docker service name
    image         TEXT NOT NULL,      -- Full image path
    reader_id     TEXT REFERENCES readers(id),
    env_json      TEXT,               -- JSON: environment variables
    status        TEXT DEFAULT 'active',
    version       INTEGER DEFAULT 1,
    updated_at    TEXT
);

-- Core-switch settings
CREATE TABLE coreswitch_settings (
    key           TEXT PRIMARY KEY,
    value_json    TEXT NOT NULL,
    updated_at    TEXT
);
-- Default rows:
-- ('outputs', '{"influxdb": true, "live": false}')
-- ('batch_size', '100')
-- ('flush_interval_ms', '5000')

-- Swarm compose file (generated by cloud, stored here)
CREATE TABLE swarm_state (
    id            INTEGER PRIMARY KEY DEFAULT 1,
    compose_yml   TEXT,
    config_hash   TEXT,
    deployed_hash TEXT,
    updated_at    TEXT
);

-- Influx-to-SQL upload config (replaces uploads.csv)
CREATE TABLE influx_uploads (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    device        TEXT NOT NULL,       -- InfluxDB measurement name (Equipment)
    reading       TEXT NOT NULL,       -- Specific metric or "*" for all
    agg_time_min  INTEGER NOT NULL,    -- Aggregation interval minutes
    agg_func      TEXT NOT NULL,       -- SUM, AVG, MAX, MIN
    to_table      TEXT NOT NULL,       -- Target: just for mapping, actual goes to TimescaleDB
    tag_names     TEXT,                -- Pipe-separated tag dimensions
    sensor_id     TEXT,                -- Cloud sensor UUID for this mapping
    updated_at    TEXT
);
```

---

## 7. How Readers Use SQLite

### The Problem with Polling SQLite

Polling SQLite every 30 seconds from every reader is wasteful — if config changes once a month, that's 86,400 useless queries per month per reader.

### The Solution: Read Once on Startup + Docker API Restart

```
┌─────────────────────────────────────────────────────────────┐
│  Config Change Flow                                         │
│                                                             │
│  1. Cloud API: user adds a sensor                           │
│  2. Cloud sends via WebSocket: { type: "config.push", ... } │
│  3. Conf-agent receives → writes to SQLite                  │
│  4. Conf-agent checks: which readers are affected?          │
│  5. Conf-agent calls Docker API:                            │
│     docker stop modbus-reader-panel-a                       │
│  6. Swarm detects stopped container → recreates it          │
│  7. New container starts → reads fresh SQLite on startup    │
│  8. Reader now has new sensor config                        │
│  9. Conf-agent sends ACK to cloud: { type: "config.ack" }  │
└─────────────────────────────────────────────────────────────┘
```

### Reader Startup Code Pattern (All Protocols)

```go
// ─── Standard reader main.go ───
func main() {
    readerID := os.Getenv("READER_ID")
    dbPath   := os.Getenv("SQLITE_PATH")  // default: /opt/qube/data/qube.db
    csURL    := os.Getenv("CORESWITCH_URL") // default: http://core-switch:8585

    // 1. Open SQLite read-only
    db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")
    if err != nil { log.Fatal(err) }
    defer db.Close()

    // 2. Load reader config
    var configJSON string
    err = db.QueryRow(
        "SELECT config_json FROM readers WHERE id = ? AND status = 'active'",
        readerID,
    ).Scan(&configJSON)

    var readerConfig ReaderConfig
    json.Unmarshal([]byte(configJSON), &readerConfig)

    // 3. Load all sensors for this reader
    rows, _ := db.Query(`
        SELECT id, name, config_json, tags_json, output, table_name
        FROM sensors
        WHERE reader_id = ? AND status = 'active'
    `, readerID)
    defer rows.Close()

    var sensors []SensorConfig
    for rows.Next() {
        var s SensorConfig
        var configStr, tagsStr, output, tableName string
        rows.Scan(&s.ID, &s.Name, &configStr, &tagsStr, &output, &tableName)
        json.Unmarshal([]byte(configStr), &s.Config)
        json.Unmarshal([]byte(tagsStr), &s.Tags)
        s.Output = output
        s.Table = tableName
        sensors = append(sensors, s)
    }

    // 4. Close SQLite — no longer needed
    db.Close()

    // 5. Start protocol-specific reading loop
    reader := NewModbusReader(readerConfig, sensors, csURL)
    reader.Run(context.Background())
    // Runs forever until container is stopped
}
```

### Core-Switch Startup (Reads Settings from SQLite)

```go
func main() {
    dbPath := os.Getenv("SQLITE_PATH")
    db, _ := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")

    // Read output settings
    var outputsJSON string
    db.QueryRow("SELECT value_json FROM coreswitch_settings WHERE key = 'outputs'").Scan(&outputsJSON)

    var outputs OutputConfig
    json.Unmarshal([]byte(outputsJSON), &outputs)
    // outputs.InfluxDB = true, outputs.Live = false

    db.Close()

    // Start core-switch with these settings
    // If settings change: cloud → conf-agent → SQLite → docker stop core-switch → restart
}
```

### Influx-to-SQL Startup (Reads sensor_map from SQLite)

```go
func main() {
    dbPath := os.Getenv("SQLITE_PATH")
    db, _ := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")

    // Load sensor map (replaces sensor_map.json)
    rows, _ := db.Query("SELECT measurement_key, sensor_id, field_key, unit FROM sensor_map")
    sensorMap := make(map[string]SensorMapping)
    for rows.Next() {
        var key, sensorID, fieldKey, unit string
        rows.Scan(&key, &sensorID, &fieldKey, &unit)
        sensorMap[key] = SensorMapping{SensorID: sensorID, FieldKey: fieldKey, Unit: unit}
    }

    // Load upload configs (replaces uploads.csv)
    uploadRows, _ := db.Query("SELECT device, reading, agg_time_min, agg_func FROM influx_uploads")
    // ... parse upload configs

    db.Close()

    // Start transfer loop with these mappings
    // If mappings change: cloud → conf-agent → SQLite → docker stop influx-to-sql → restart
}
```

---

## 8. Cloud Architecture — Split Databases + TimescaleDB

### Same PostgreSQL Instance, Two Databases

```
PostgreSQL instance (localhost:5432)
├── qubedb      ← Management database (orgs, users, qubes, readers, sensors, templates, commands)
└── qubedata    ← Telemetry database (TimescaleDB hypertable, all sensor readings)
```

### Why TimescaleDB (Not Per-Schema Partitioning)

Per-qube schemas would create:
- 10,000 schemas at scale
- 120,000 partitions per year
- Bloated pg_catalog
- Impossible fleet-wide queries

**TimescaleDB gives us:**
- One `readings` hypertable, auto-partitioned by time (chunks)
- `qube_id` and `sensor_id` as regular indexed columns
- Fleet-wide queries work naturally: `WHERE org_id = X`
- Automatic chunk management (compression, retention)
- Same Postgres, just an extension: `CREATE EXTENSION timescaledb;`

### Telemetry DB Schema (qubedata)

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Single hypertable for ALL telemetry
CREATE TABLE readings (
    ts          TIMESTAMPTZ NOT NULL,
    qube_id     TEXT NOT NULL,
    sensor_id   UUID NOT NULL,
    field_key   TEXT NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    unit        TEXT,
    tags        JSONB
);

-- Convert to TimescaleDB hypertable (auto-partitions by time)
SELECT create_hypertable('readings', 'ts');

-- Indexes for common query patterns
CREATE INDEX idx_readings_qube_sensor ON readings (qube_id, sensor_id, ts DESC);
CREATE INDEX idx_readings_sensor_ts ON readings (sensor_id, ts DESC);

-- Retention policy: auto-drop data older than N months (configurable per qube)
-- Default: 12 months
SELECT add_retention_policy('readings', INTERVAL '12 months');

-- Compression policy: compress chunks older than 7 days
ALTER TABLE readings SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'qube_id, sensor_id',
    timescaledb.compress_orderby = 'ts DESC'
);
SELECT add_compression_policy('readings', INTERVAL '7 days');

-- Continuous aggregate for fast dashboard queries (optional)
CREATE MATERIALIZED VIEW readings_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', ts) AS bucket,
    qube_id,
    sensor_id,
    field_key,
    avg(value) AS avg_value,
    min(value) AS min_value,
    max(value) AS max_value,
    count(*) AS sample_count
FROM readings
GROUP BY bucket, qube_id, sensor_id, field_key;
```

### Telemetry Settings API

```
GET  /api/v1/qubes/{id}/telemetry/settings
PUT  /api/v1/qubes/{id}/telemetry/settings
{
    "retention_months": 12,
    "compression_after_days": 7,
    "continuous_aggregates": ["1h", "1d"]
}
```

These are stored in management DB and applied to TimescaleDB policies.

---

## 9. Template System — Device Templates + Reader Templates

### Why Split Templates?

A single template trying to hold everything (sensor config + container spec + UI schema + mappings) becomes unwieldy. Split into two focused templates:

**Device Template** = "What data to collect from this type of device"
- Register maps, OID maps, MQTT topic/JSON paths, OPC-UA nodes
- Field keys, units, data types
- This is what the user picks when adding a sensor

**Reader Template** = "How to run the container for this protocol"
- Docker image, environment variables, volumes
- Connection parameters schema (host, port, credentials)
- Protocol-specific behavior (multi-sensor? single-target?)
- This is what the system uses when creating containers

### Device Template (Sensor-Focused)

```sql
CREATE TABLE device_templates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL,            -- "Schneider PM5100"
    protocol          TEXT NOT NULL REFERENCES protocols(id),
    manufacturer      TEXT,
    model             TEXT,
    description       TEXT,
    org_id            UUID REFERENCES organisations(id),  -- NULL = global
    is_global         BOOLEAN DEFAULT false,

    -- What data this device provides
    sensor_config     JSONB NOT NULL,
    -- Modbus: { "registers": [{ "field_key": "voltage_v", "address": 3000, ... }] }
    -- SNMP:   { "oids": [{ "field_key": "battery_voltage", "oid": ".1.3.6...", ... }] }
    -- MQTT:   { "json_paths": [{ "field_key": "temperature", "json_path": "$.data.temp", ... }] }
    -- OPC-UA: { "nodes": [{ "field_key": "temperature", "node_id": "ns=2;i=1001", ... }] }
    -- HTTP:   { "json_paths": [{ "field_key": "value", "json_path": "$.value", ... }] }

    -- What user must fill per-sensor (JSON Schema for UI form generation)
    sensor_params_schema JSONB NOT NULL,
    -- Modbus: { "properties": { "unit_id": { "type": "integer", "default": 1 } } }
    -- SNMP:   { "properties": { "ip_address": { "type": "string" }, "community": { "type": "string" } } }
    -- MQTT:   { "properties": { "topic": { "type": "string" } } }

    -- Metadata for UI
    tags              JSONB,
    version           INTEGER DEFAULT 1,
    created_at        TIMESTAMPTZ DEFAULT now(),
    updated_at        TIMESTAMPTZ DEFAULT now()
);
```

### Reader Template (Container-Focused)

```sql
CREATE TABLE reader_templates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol          TEXT NOT NULL REFERENCES protocols(id),
    name              TEXT NOT NULL,            -- "Modbus TCP Reader"
    description       TEXT,

    -- Container specification
    image_suffix      TEXT NOT NULL,            -- "modbus-reader" (prepended with registry)
    env_defaults      JSONB DEFAULT '{}',       -- {"LOG_LEVEL": "info"}
    volumes           JSONB DEFAULT '[]',       -- ["/opt/qube/data:/opt/qube/data:ro"]

    -- Connection parameters schema (JSON Schema for UI form)
    connection_schema JSONB NOT NULL,
    -- Modbus: { "properties": { "host": {...}, "port": {...}, "poll_interval_sec": {...} } }
    -- SNMP:   { "properties": { "fetch_interval_sec": {...}, "worker_count": {...} } }
    -- MQTT:   { "properties": { "broker_host": {...}, "broker_port": {...}, "username": {...} } }

    -- Behavior
    reader_standard   TEXT NOT NULL CHECK (reader_standard IN ('endpoint', 'multi_target')),
    -- endpoint:     one container per host:port (Modbus, OPC-UA)
    -- multi_target:  one container handles many targets (SNMP) or one endpoint many sensors (MQTT)

    -- When adding new protocol: create this template + Dockerfile + device templates
    is_default        BOOLEAN DEFAULT true,     -- default template for this protocol
    version           INTEGER DEFAULT 1,
    created_at        TIMESTAMPTZ DEFAULT now()
);
```

### Protocols Table

```sql
CREATE TABLE protocols (
    id              TEXT PRIMARY KEY,    -- modbus_tcp, snmp, mqtt, opcua, http
    label           TEXT NOT NULL,       -- "Modbus TCP"
    default_port    INTEGER,
    reader_standard TEXT NOT NULL CHECK (reader_standard IN ('endpoint', 'multi_target')),
    created_at      TIMESTAMPTZ DEFAULT now()
);

INSERT INTO protocols VALUES
('modbus_tcp', 'Modbus TCP',  502,  'endpoint'),
('opcua',      'OPC-UA',      4840, 'endpoint'),
('snmp',       'SNMP',        161,  'multi_target'),
('mqtt',       'MQTT',        1883, 'endpoint'),      -- one container per broker, multi-sensor (topics)
('http',       'HTTP/REST',   80,   'multi_target');
```

**Note on MQTT**: MQTT is `endpoint` because you need one container per broker (IP). But it's multi-sensor — one container handles many topics from that broker. If you have 2 MQTT brokers, you run 2 mqtt-reader containers.

### How Templates Work Together

```
Adding a new protocol (e.g., BACnet):
1. INSERT INTO protocols ('bacnet', 'BACnet', 47808, 'multi_target')
2. Create reader_template: image_suffix='bacnet-reader', connection_schema={...}
3. Create device_template(s): "Johnson Controls VAV", sensor_config={bacnet_objects: [...]}
4. Build bacnet-reader Docker image (Go binary that reads SQLite)
5. Push to registry
6. Done — users can now add BACnet devices

Adding a new sensor (user flow):
1. User selects Qube → "Add Reader"
2. UI loads reader_templates for selected protocol
3. UI shows connection form (from reader_template.connection_schema)
4. User fills in host, port, etc.
5. Reader created in DB

6. User selects Reader → "Add Sensor"
7. UI loads device_templates for this protocol
8. User picks "Schneider PM5100"
9. UI shows sensor params form (from device_template.sensor_params_schema)
10. User fills in unit_id, tags, etc.
11. Sensor created with config_json from device_template.sensor_config + user params

12. Cloud pushes config via WebSocket → conf-agent → SQLite → Docker restart reader
```

### Custom Sensor (No Template — User Maps Manually)

For users adding sensors without a matching device template:

```
1. User selects "Custom Sensor" (or runs Auto-Discovery first)
2. UI shows generic form for the protocol:
   - Modbus: enter register address, type, field_key manually
   - SNMP: enter OID, field_key manually
   - MQTT: enter topic, JSON path, field_key manually
3. User can save this as a new device_template for reuse
```

---

## 10. Reader Standardization

### Shared Go Module (Build-Time, Not Runtime)

Each reader has its own `go.mod` and builds into its own Docker image. But they share common code via a Go module — same pattern as your existing gateways importing `gitlab.com/iot-team4/product/core-switch/v3`.

```
pkg/                              ← Shared Go module
├── go.mod                        ← module github.com/sandun-s/qube-enterprise-home/pkg
├── coreswitch/
│   └── client.go                 ← HTTP client for POST /v3/batch
├── sqliteconfig/
│   └── loader.go                 ← Load reader config + sensors from SQLite
└── logger/
    └── logger.go                 ← Standard logrus-based logging

modbus-reader/
├── go.mod                        ← requires github.com/sandun-s/qube-enterprise-home/pkg
├── main.go
└── Dockerfile

snmp-reader/
├── go.mod                        ← requires github.com/sandun-s/qube-enterprise-home/pkg
├── main.go
└── Dockerfile
```

At build time, Go downloads the `pkg` module and compiles it into the binary. Each Docker image is fully self-contained.

### Core-Switch DataIn Format (MUST Match Existing)

All readers send data to core-switch using the **exact same format** as current gateways:

```go
// From core-switch/schema/schema.go (existing, unchanged)
type DataIn struct {
    Table     string `json:"Table"`
    Equipment string `json:"Equipment"`
    Reading   string `json:"Reading"`
    Output    string `json:"Output"`     // "influxdb", "live", "influxdb,live"
    Sender    string `json:"Sender"`
    Tags      string `json:"Tags"`       // comma-separated: "name=PM5100,location=rack1"
    Time      int64  `json:"Time"`       // Unix MICROSECONDS
    Value     string `json:"Value"`      // Always string
}
```

**Change from v1**: The `Output` field now supports `"live"` (WebSocket to cloud) in addition to `"influxdb"`. The value `"mqtt"` is no longer used (no internal MQTT). Core-switch routes accordingly.

### Reader Environment Variables (Standard)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `READER_ID` | Yes | — | UUID of this reader in SQLite |
| `SQLITE_PATH` | No | `/opt/qube/data/qube.db` | Path to SQLite file |
| `CORESWITCH_URL` | No | `http://core-switch:8585` | Core-switch endpoint |
| `LOG_LEVEL` | No | `info` | debug, info, warn, error |

### Reader Protocol Classification

| Protocol | Standard | Container Behavior | Example |
|----------|----------|-------------------|---------|
| Modbus TCP | endpoint | One container per IP:port. Multiple sensors (registers) from that endpoint. | modbus-reader-panel-a → 192.168.1.50:502 |
| OPC-UA | endpoint | One container per OPC server. Multiple sensors (nodes) from that server. | opcua-reader-plc1 → opc.tcp://192.168.1.18:52520 |
| MQTT | endpoint | One container per MQTT broker. Multiple sensors (topics) from that broker. If 2 brokers → 2 containers. | mqtt-reader-factory → broker.internal:1883 |
| SNMP | multi_target | ONE container per Qube. Handles ALL SNMP devices (many IPs, many OID maps). | snmp-reader → polls 10.0.0.50, 10.0.0.51, ... |
| HTTP | multi_target | ONE container per Qube. Polls multiple HTTP endpoints. | http-reader → polls multiple URLs |

---

## 11. Core-Switch v2

### Changes from v1

| Feature | v1 | v2 |
|---------|----|----|
| Config source | Hardcoded / env vars | **SQLite** (reads on startup) |
| Outputs | influxdb, mqtt | **influxdb, live** |
| MQTT output | Publishes to internal Mosquitto | **Removed** (no internal MQTT) |
| Live output | Not supported | **WebSocket to conf-agent → cloud** |
| Settings control | None | **Via API → SQLite → restart** |

### Core-Switch Data Flow

```
Reader → POST /v3/batch
[
    {
        "Table": "Measurements",
        "Equipment": "PM5100_Rack1",
        "Reading": "active_power_w",
        "Output": "influxdb,live",      ← controls routing
        "Sender": "modbus-reader",
        "Tags": "name=PM5100_Rack1,location=rack1",
        "Time": 1711446600000000,        ← Unix microseconds
        "Value": "1250.5"                ← always string
    }
]

Core-Switch routes:
  "influxdb" in Output → write to InfluxDB v1 (edge buffer)
  "live" in Output     → forward to conf-agent WebSocket → cloud → frontend
```

### Core-Switch Settings API (via Cloud)

```
PUT /api/v1/qubes/{id}/coreswitch/settings
{
    "outputs": {
        "influxdb": true,     // write to InfluxDB (for influx-to-sql pipeline)
        "live": false          // stream to cloud in real-time (future)
    },
    "batch_size": 100,
    "flush_interval_ms": 5000
}
```

Cloud pushes this to Qube via WebSocket → conf-agent writes to SQLite `coreswitch_settings` table → `docker stop core-switch` → Swarm recreates → reads fresh settings.

---

## 12. Protocol Standards & Libraries

### Go Libraries (Per Protocol)

| Protocol | Library | Status |
|----------|---------|--------|
| Modbus TCP | `github.com/apache/plc4x/plc4go` | **Keep** (already in production) |
| OPC-UA | `github.com/gopcua/opcua` | Production |
| SNMP | `github.com/gosnmp/gosnmp` | Production (already used) |
| MQTT | `github.com/eclipse/paho.mqtt.golang` | Production |
| HTTP | `net/http` (stdlib) | No external dep |

### SenML (RFC 8428) — For TP-API Telemetry Ingest Only

SenML is used **only** for the influx-to-sql → TP-API telemetry ingest path. Readers use the existing `DataIn` format to core-switch.

```json
[
    {"bn": "urn:qube:Q-1001:", "bt": 1711446600},
    {"n": "PM5100_Rack1.active_power_w", "v": 1250.5, "u": "W"},
    {"n": "PM5100_Rack1.voltage_ll_v", "v": 230.1, "u": "V"}
]
```

### Standard JSON for Custom Sensors (Auto-Discovery)

When a user adds a custom HTTP/MQTT endpoint and we need to discover what data it provides, we support these standard JSON formats for incoming data:

- **SenML (RFC 8428)** — if the device sends SenML, we auto-parse it
- **Plain JSON** — user maps JSON paths to fields manually
- **Raw key-value** — `{"temperature": 25.5, "humidity": 60}` → auto-detect fields

The discovery flow (Section 14) handles showing the user what data came in and letting them map it.

---

## 13. Docker Swarm File Generation

### Dev: Docker Compose / Prod: Docker Swarm

```
Development:  docker compose -f docker-compose.dev.yml up -d
Production:   docker stack deploy -c /opt/qube/docker-compose.yml qube
```

The generated compose file is **compatible with both** (version: "3.8" works for both compose and swarm).

### Generation Logic

Swarm file is generated by cloud, pushed to Qube via WebSocket, stored in SQLite `swarm_state.compose_yml`.

Conf-agent writes it to `/opt/qube/docker-compose.yml` and runs `docker stack deploy`.

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
  # === Infrastructure (always present) ===

  enterprise-conf-agent:
    image: ${REGISTRY}/enterprise-conf-agent:${ARCH}.latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - qube-data:/opt/qube/data
    environment:
      QUBE_ID: Q-1001
      SQLITE_PATH: /opt/qube/data/qube.db
      CLOUD_WS_URL: wss://cloud.example.com:8080/ws
      TPAPI_URL: https://cloud.example.com:8081
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  core-switch:
    image: ${REGISTRY}/core-switch:${ARCH}.latest
    volumes:
      - qube-data:/opt/qube/data:ro
    environment:
      SQLITE_PATH: /opt/qube/data/qube.db
      INFLUX_URL: http://influxdb:8086
      INFLUX_DB: edgex
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

  enterprise-influx-to-sql:
    image: ${REGISTRY}/enterprise-influx-to-sql:${ARCH}.latest
    volumes:
      - qube-data:/opt/qube/data:ro
    environment:
      SQLITE_PATH: /opt/qube/data/qube.db
      INFLUX_URL: http://influxdb:8086
      INFLUX_DB: edgex
      TPAPI_URL: https://cloud.example.com:8081
      QUBE_ID: Q-1001
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  # === Readers (dynamically generated per qube config) ===

  modbus-reader-panel-a:
    image: ${REGISTRY}/modbus-reader:${ARCH}.latest
    volumes:
      - qube-data:/opt/qube/data:ro
    environment:
      READER_ID: "uuid-reader-1"
      SQLITE_PATH: /opt/qube/data/qube.db
      CORESWITCH_URL: http://core-switch:8585
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  snmp-reader:
    image: ${REGISTRY}/snmp-reader:${ARCH}.latest
    volumes:
      - qube-data:/opt/qube/data:ro
    environment:
      READER_ID: "uuid-reader-2"
      SQLITE_PATH: /opt/qube/data/qube.db
      CORESWITCH_URL: http://core-switch:8585
    deploy:
      restart_policy: {condition: any}
    networks: [qube-net]

  # NOTE: No MQTT broker service (removed — no internal MQTT needed)
```

---

## 14. Auto-Discovery Flow

### Purpose

When a customer brings an unknown device and no device template exists.

### Step 1: Start Discovery

```
POST /api/v1/qubes/{qube_id}/discover
{
    "protocol": "modbus_tcp",
    "target": { "host": "192.168.1.100", "port": 502 },
    "duration_sec": 30,
    "options": { "register_range": "1-10000", "register_types": ["Holding", "Input"] }
}
```

Cloud deploys a temporary discovery container to the Qube.

### Step 2: Discovery Scans

- **Modbus**: Scans register ranges, finds registers with valid data
- **SNMP**: SNMP walk, lists all available OIDs
- **MQTT**: Subscribes to wildcard, captures all messages
- **OPC-UA**: Browses node tree
- **HTTP**: GETs endpoint, parses JSON keys

### Step 3: Results Returned

```json
{
    "discovered": [
        { "address": 3000, "type": "Holding", "raw_value": 2301, "suggested_field": "register_3000" },
        { "address": 3002, "type": "Holding", "raw_value": 543,  "suggested_field": "register_3002" }
    ]
}
```

### Step 4: User Maps Fields in UI

User sees the raw data and maps each to a meaningful name:
```
register_3000 → "voltage_v" (unit: V, scale: 0.1)
register_3002 → "current_a" (unit: A, scale: 0.01)
```

### Step 5: Device Template Auto-Created

```
POST /api/v1/templates/device/from-discovery
{
    "discovery_id": "uuid",
    "name": "Custom Power Meter",
    "mappings": [...]
}
```

### Standard JSON Detection (HTTP/MQTT)

When discovering HTTP or MQTT endpoints, if the data arrives in a recognized format:

- **SenML**: Auto-parse fields, units, values
- **Plain JSON**: Show JSON tree, let user click to select paths
- **Raw values**: Show key-value pairs, user maps them

---

## 15. Migration Strategy (SQL)

### Management DB Migrations (qubedb)

```
cloud/migrations/
├── 001_init.sql                  # ALL management tables
├── 002_global_data.sql           # Protocols, reader templates, device templates (production data)
└── 003_test_seeds.sql            # Test orgs, users, qubes (dev/staging ONLY)
```

### Telemetry DB Migrations (qubedata)

```
cloud/migrations-telemetry/
└── 001_timescale_init.sql        # TimescaleDB extension + readings hypertable
```

### 001_init.sql — All Management Tables

```sql
-- See Section 8 for telemetry DB, Section 9 for template tables
-- Full schema includes:

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- organisations, users (same as v1)
-- qubes (add: ws_connected, poll_interval_sec, capabilities)
-- protocols
-- reader_templates
-- device_templates
-- readers (was gateways)
-- sensors (simplified, no csv_rows)
-- containers (was services)
-- config_state (add: config_version, ws_last_ack)
-- qube_commands
-- discovery_sessions
-- coreswitch_settings
-- webhook_log → renamed to ws_delivery_log
-- swarm_history
```

### 002_global_data.sql — Production Seed Data

```sql
-- Protocols
INSERT INTO protocols (id, label, default_port, reader_standard) VALUES
('modbus_tcp', 'Modbus TCP',  502,  'endpoint'),
('opcua',      'OPC-UA',      4840, 'endpoint'),
('mqtt',       'MQTT',        1883, 'endpoint'),
('snmp',       'SNMP',        161,  'multi_target'),
('http',       'HTTP/REST',   80,   'multi_target');

-- Reader Templates (one per protocol)
-- ... Modbus TCP Reader, SNMP Reader, MQTT Reader, OPC-UA Reader, HTTP Reader

-- Device Templates (global)
-- ... Schneider PM5100, Schneider PM2100, Eastron SDM630, APC Smart-UPS, etc.
```

### 003_test_seeds.sql — Dev Only

```sql
-- Superadmin org + user (iotteam@internal.local / iotteam2024)
-- Test Qubes Q-1001 through Q-1020
-- ONLY loaded when SEED_TEST_DATA=true or in docker-compose.dev.yml
```

---

## 16. API Surface v2

### Cloud API (:8080) — User-Facing (JWT)

```
# Auth
POST   /api/v1/auth/register
POST   /api/v1/auth/login

# Users
GET    /api/v1/users/me
GET    /api/v1/users
POST   /api/v1/users
PATCH  /api/v1/users/{id}
DELETE /api/v1/users/{id}

# Qubes
GET    /api/v1/qubes
GET    /api/v1/qubes/{id}
POST   /api/v1/qubes/claim
DELETE /api/v1/qubes/{id}/claim
PUT    /api/v1/qubes/{id}/settings           # poll_interval, etc.

# Protocols
GET    /api/v1/protocols

# Reader Templates
GET    /api/v1/templates/reader
GET    /api/v1/templates/reader/{id}
POST   /api/v1/templates/reader              # for new protocols
PUT    /api/v1/templates/reader/{id}

# Device Templates
GET    /api/v1/templates/device              # filter by protocol
GET    /api/v1/templates/device/{id}
POST   /api/v1/templates/device
PUT    /api/v1/templates/device/{id}
DELETE /api/v1/templates/device/{id}
POST   /api/v1/templates/device/{id}/clone
POST   /api/v1/templates/device/from-discovery

# Readers (was /gateways/)
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
POST   /api/v1/sensors/{id}/sync-template

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
DELETE /api/v1/discovery/{id}

# Telemetry & Data
GET    /api/v1/data/readings
GET    /api/v1/data/sensors/{id}/latest
GET    /api/v1/data/qubes/{id}/summary

# Telemetry Settings
GET    /api/v1/qubes/{id}/telemetry/settings
PUT    /api/v1/qubes/{id}/telemetry/settings

# Core-Switch Settings
GET    /api/v1/qubes/{id}/coreswitch/settings
PUT    /api/v1/qubes/{id}/coreswitch/settings

# Admin
GET    /api/v1/admin/registry
PUT    /api/v1/admin/registry

# WebSocket
GET    /ws                                   # Upgrade to WebSocket (Qube + Frontend)
```

### TP-API (:8081) — Qube-Facing (HMAC) — Polling Fallback

```
# Public
POST   /v1/device/register

# HMAC Protected
GET    /v1/sync/state                        # returns config_version + hash
GET    /v1/sync/config                       # full config snapshot
GET    /v1/sync/changes?since_version=N      # incremental changes
POST   /v1/heartbeat
POST   /v1/commands/poll
POST   /v1/commands/{id}/ack
POST   /v1/telemetry/ingest                  # SenML format
```

---

## 17. Implementation Phases

### Phase 0: Standards & Foundation (Week 1)

| # | Task | Files |
|---|------|-------|
| 0.1 | Create `pkg/` shared Go module (coreswitch client, SQLite loader, logger) | `pkg/` |
| 0.2 | Define SQLite edge schema | `standards/SQLITE_SCHEMA.md` |
| 0.3 | Define reader standard doc | `standards/READER_STANDARD.md` |
| 0.4 | Define template standard doc | `standards/TEMPLATE_STANDARD.md` |
| 0.5 | Set up docker-compose.dev.yml v2 (TimescaleDB, no MQTT broker) | `docker-compose.dev.yml` |
| 0.6 | Define core-switch DataIn format (add "live" output) | `standards/CORESWITCH_FORMAT.md` |

### Phase 1: Cloud Rewrite (Weeks 2-3)

| # | Task | Files |
|---|------|-------|
| 1.1 | Write 001_init.sql (all mgmt tables) | `cloud/migrations/001_init.sql` |
| 1.2 | Write 002_global_data.sql (protocols + templates) | `cloud/migrations/002_global_data.sql` |
| 1.3 | Write 003_test_seeds.sql | `cloud/migrations/003_test_seeds.sql` |
| 1.4 | Write 001_timescale_init.sql | `cloud/migrations-telemetry/001_timescale_init.sql` |
| 1.5 | Rewrite router (readers, templates, etc.) | `cloud/internal/api/router.go` |
| 1.6 | Reader CRUD handlers | `cloud/internal/api/readers.go` |
| 1.7 | Sensor CRUD handlers (template-based) | `cloud/internal/api/sensors.go` |
| 1.8 | Device template handlers | `cloud/internal/api/device_templates.go` |
| 1.9 | Reader template handlers | `cloud/internal/api/reader_templates.go` |
| 1.10 | Container handlers | `cloud/internal/api/containers.go` |
| 1.11 | Config hash + version tracking | `cloud/internal/api/hash.go` |
| 1.12 | WebSocket server | `cloud/internal/ws/server.go` |
| 1.13 | Config push via WebSocket | `cloud/internal/ws/config_push.go` |
| 1.14 | Discovery handlers | `cloud/internal/api/discovery.go` |
| 1.15 | Coreswitch settings handlers | `cloud/internal/api/coreswitch.go` |
| 1.16 | Telemetry settings handlers | `cloud/internal/api/telemetry_settings.go` |
| 1.17 | Split DB connections (qubedb + qubedata) | `cloud/cmd/server/main.go` |
| 1.18 | Telemetry ingest (SenML → TimescaleDB) | `cloud/internal/tpapi/telemetry.go` |

### Phase 2: TP-API & Sync Rewrite (Week 4)

| # | Task | Files |
|---|------|-------|
| 2.1 | Incremental sync endpoint | `cloud/internal/tpapi/sync.go` |
| 2.2 | Full sync endpoint (fallback) | `cloud/internal/tpapi/sync.go` |
| 2.3 | Swarm file generation | `cloud/internal/tpapi/compose.go` |
| 2.4 | Device registration (accept capabilities) | `cloud/internal/tpapi/router.go` |
| 2.5 | WebSocket message dispatcher | `cloud/internal/ws/dispatcher.go` |

### Phase 3: Enterprise Conf-Agent (Weeks 5-6)

| # | Task | Files |
|---|------|-------|
| 3.1 | SQLite schema init + writer | `conf-agent/sqlite.go` |
| 3.2 | WebSocket client to cloud | `conf-agent/websocket.go` |
| 3.3 | Config applier (WS message → SQLite) | `conf-agent/apply.go` |
| 3.4 | Docker API: stop/restart affected containers | `conf-agent/docker.go` |
| 3.5 | Swarm file writer + deployer | `conf-agent/deploy.go` |
| 3.6 | HTTP polling fallback | `conf-agent/poll.go` |
| 3.7 | Heartbeat sender | `conf-agent/heartbeat.go` |
| 3.8 | Command executor | `conf-agent/commands.go` |
| 3.9 | All conf-agent lite features | `conf-agent/` |
| 3.10 | Self-registration flow | `conf-agent/register.go` |

### Phase 4: Reader Containers (Weeks 7-9)

| # | Task | Component |
|---|------|-----------|
| 4.1 | modbus-reader (PLC4X, reads SQLite, standard output) | `modbus-reader/` |
| 4.2 | snmp-reader (gosnmp, multi-target, reads SQLite) | `snmp-reader/` |
| 4.3 | mqtt-reader (paho, multi-topic, reads SQLite) | `mqtt-reader/` |
| 4.4 | opcua-reader (gopcua, reads SQLite) | `opcua-reader/` |
| 4.5 | http-reader (stdlib, multi-endpoint, reads SQLite) | `http-reader/` |
| 4.6 | Discovery containers (per protocol) | `discovery/` |

### Phase 5: Core-Switch & Influx-to-SQL (Week 10)

| # | Task | Component |
|---|------|-----------|
| 5.1 | Core-switch v2 (SQLite settings, live output, no MQTT output) | `core-switch/` |
| 5.2 | Influx-to-SQL v2 (SQLite sensor_map + upload config) | `enterprise-influx-to-sql/` |

### Phase 6: Testing & Documentation (Week 11)

| # | Task | Files |
|---|------|-------|
| 6.1 | Rewrite test_api.sh (all new endpoints) | `test/test_api.sh` |
| 6.2 | Update docker-compose.dev.yml | Root |
| 6.3-6.10 | Update all .md docs | All docs |
| 6.11 | Create MIGRATION_GUIDE.md (v1 → v2) | Root |

### Phase 7: CI/CD (Week 12)

| # | Task | Files |
|---|------|-------|
| 7.1 | GitHub Actions: build all containers (amd64 + arm64) | `.github/workflows/` |
| 7.2 | Registry abstraction (QUBE_IMAGE_REGISTRY env var) | All Dockerfiles |
| 7.3 | Easy switch to GitLab later | Documentation |

---

## 18. Repository Structure v2

```
qube-enterprise/
├── cloud/                          # Cloud API + TP-API + WebSocket (single Go binary)
│   ├── cmd/server/main.go
│   ├── internal/api/               # Cloud API (:8080, JWT)
│   │   ├── router.go
│   │   ├── auth.go
│   │   ├── readers.go              # was gateways.go
│   │   ├── sensors.go
│   │   ├── device_templates.go
│   │   ├── reader_templates.go
│   │   ├── containers.go
│   │   ├── discovery.go
│   │   ├── coreswitch.go
│   │   ├── telemetry.go
│   │   ├── telemetry_settings.go
│   │   ├── hash.go
│   │   ├── commands.go
│   │   ├── qubes.go
│   │   ├── users.go
│   │   ├── protocols.go
│   │   ├── registry.go
│   │   └── middleware.go
│   ├── internal/tpapi/             # TP-API (:8081, HMAC) — polling fallback
│   │   ├── router.go
│   │   ├── sync.go
│   │   ├── compose.go
│   │   ├── telemetry.go
│   │   └── commands.go
│   ├── internal/ws/                # WebSocket server
│   │   ├── server.go
│   │   ├── config_push.go
│   │   └── dispatcher.go
│   ├── migrations/                 # Management DB
│   │   ├── 001_init.sql
│   │   ├── 002_global_data.sql
│   │   └── 003_test_seeds.sql
│   └── migrations-telemetry/       # Telemetry DB
│       └── 001_timescale_init.sql
│
├── conf-agent/                     # Enterprise Conf-Agent (all lite features + enterprise)
│   ├── main.go
│   ├── sqlite.go
│   ├── websocket.go
│   ├── apply.go
│   ├── docker.go
│   ├── deploy.go
│   ├── poll.go
│   ├── heartbeat.go
│   ├── commands.go
│   ├── register.go
│   └── Dockerfile
│
├── core-switch/                    # Core-Switch v2 (separate folder, separate build)
│   ├── main.go
│   ├── schema/schema.go           # DataIn struct (unchanged + "live" output)
│   ├── http/http.go               # Route data to influx or live
│   └── Dockerfile
│
├── pkg/                            # Shared Go module (imported at build time)
│   ├── go.mod
│   ├── coreswitch/client.go
│   ├── sqliteconfig/loader.go
│   └── logger/logger.go
│
├── modbus-reader/                  # Separate folder, separate build
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── snmp-reader/
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── mqtt-reader/
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── opcua-reader/
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── http-reader/                    # NEW
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── enterprise-influx-to-sql/       # Updated: reads SQLite
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
│
├── discovery/                      # Discovery containers
│   ├── modbus-discover/
│   ├── snmp-discover/
│   ├── mqtt-discover/
│   ├── opcua-discover/
│   └── http-discover/
│
├── standards/                      # Development standards docs
│   ├── READER_STANDARD.md
│   ├── TEMPLATE_STANDARD.md
│   ├── SQLITE_SCHEMA.md
│   ├── CORESWITCH_FORMAT.md
│   └── SENML_FORMAT.md
│
├── scripts/
│   ├── setup-cloud.sh
│   ├── setup-qube.sh
│   └── write-to-database.sh
│
├── test/
│   └── test_api.sh
│
├── test-ui/index.html
├── docker-compose.dev.yml          # Dev (compose, TimescaleDB, no MQTT broker)
├── CLAUDE.md
├── ARCHITECTURE.md
├── DEEP_DIVE.md
├── DEPLOYMENT.md
├── TESTING.md
├── ADDING-PROTOCOLS.md
├── UI-API-GUIDE.md
├── README.md
└── MIGRATION_GUIDE.md              # NEW
```

---

## 19. Testing & Documentation Updates

### Test Suite Rewrite

test_api.sh must cover:
1. Auth (register, login)
2. Qube claim/unclaim
3. Protocols list
4. Reader templates CRUD
5. Device templates CRUD + clone + from-discovery
6. Reader CRUD (was gateway)
7. Sensor CRUD (template-based)
8. Container list
9. WebSocket connection + config push (may need separate WS test)
10. Sync state + full config (polling fallback)
11. Incremental changes
12. Commands dispatch + poll + ack
13. Telemetry ingest (SenML) + query
14. Discovery start + results
15. Coreswitch settings
16. Telemetry settings

### Documentation to Update

All .md files need full rewrite for v2:
- CLAUDE.md, ARCHITECTURE.md, DEEP_DIVE.md, DEPLOYMENT.md
- TESTING.md, ADDING-PROTOCOLS.md, UI-API-GUIDE.md, README.md
- NEW: MIGRATION_GUIDE.md, standards/*.md

---

## 20. CI/CD & Registry Considerations

### Image Naming

```
{QUBE_IMAGE_REGISTRY}:{service}-{arch}.{version}

# GitHub (dev)
ghcr.io/sandun-s/qube-enterprise-home:modbus-reader-arm64.latest

# GitLab (production)
registry.gitlab.com/iot-team4/product:modbus-reader-arm64.latest
```

Only `QUBE_IMAGE_REGISTRY` changes between GitHub and GitLab.

### Containers to Build (12 total)

| Container | Architectures |
|-----------|---------------|
| enterprise-cloud-api | amd64 |
| enterprise-conf-agent | arm64, amd64 |
| core-switch | arm64, amd64 |
| enterprise-influx-to-sql | arm64, amd64 |
| modbus-reader | arm64, amd64 |
| snmp-reader | arm64, amd64 |
| mqtt-reader | arm64, amd64 |
| opcua-reader | arm64, amd64 |
| http-reader | arm64, amd64 |
| modbus-discover | arm64, amd64 |
| snmp-discover | arm64, amd64 |
| mqtt-discover | arm64, amd64 |

---

## 21. Existing Repos Reference

Existing working repos at `D:\mitesp\Projects\09_Qube_Enterprice\`:

| Service | Key Info | What Changes for v2 |
|---------|----------|---------------------|
| `core-switch/` | DataIn schema, /v3/batch endpoint, influx+mqtt routing | Add "live" output, read settings from SQLite, remove MQTT output |
| `modbus-gateway/` | Uses PLC4X, reads registers.csv, configs.yml | Rename to modbus-reader, read from SQLite instead of CSV/YAML |
| `snmp-gateway/` | gosnmp, devices.csv + maps/*.csv | Rename to snmp-reader, read from SQLite |
| `opc-ua-gateway/` | gopcua, nodes.csv | Rename to opcua-reader, read from SQLite |
| `influx-to-sql/` | uploads.csv, sensor aggregation, cron-based | Read config from SQLite, SenML output |
| `conf-agent/` | Lite version, MySQL backend | Full rewrite as enterprise-conf-agent |
| `conf-api/` | MySQL backend, v2 endpoints | **Not used in enterprise** (replaced by Cloud API) |
| `tp-api/` | MySQL backend | **Replaced** by enterprise TP-API |
| `con-checker/` | connections.csv | Keep as-is or integrate into conf-agent |
| `influxdb-relay/` | Python relay | **Not used in enterprise** |

**DO NOT modify these repos** — they are working in production. Reference only.
