# SQLite Edge Schema Reference

> This is the schema for the SQLite database running on each Qube at `/opt/qube/data/qube.db`.

---

## Access Rules

| Service | Access | When |
|---------|--------|------|
| enterprise-conf-agent | **READ-WRITE** | Always (only writer) |
| modbus-reader | READ-ONLY | Once on startup |
| snmp-reader | READ-ONLY | Once on startup |
| mqtt-reader | READ-ONLY | Once on startup |
| opcua-reader | READ-ONLY | Once on startup |
| http-reader | READ-ONLY | Once on startup |
| core-switch | READ-ONLY | Once on startup |
| enterprise-influx-to-sql | READ-ONLY | Once on startup |

## Pragmas

```sql
PRAGMA journal_mode=WAL;     -- Allow concurrent readers while conf-agent writes
PRAGMA busy_timeout=5000;    -- Wait up to 5s if locked
```

## Full Schema

```sql
-- =============================================
-- Qube Identity & Connection
-- =============================================

CREATE TABLE IF NOT EXISTS qube_identity (
    qube_id           TEXT PRIMARY KEY,
    org_id            TEXT,
    qube_token        TEXT,           -- HMAC token for TP-API auth
    cloud_ws_url      TEXT,           -- "wss://cloud.example.com:8080/ws"
    tp_api_url        TEXT,           -- "https://cloud.example.com:8081" (polling fallback)
    poll_interval_sec INTEGER DEFAULT 30,
    config_version    INTEGER DEFAULT 0,
    last_sync_at      TEXT            -- ISO 8601
);

-- =============================================
-- Readers (synced from cloud Postgres readers table)
-- =============================================

CREATE TABLE IF NOT EXISTS readers (
    id          TEXT PRIMARY KEY,       -- UUID from cloud
    name        TEXT NOT NULL,          -- "modbus-panel-a"
    protocol    TEXT NOT NULL,          -- "modbus_tcp", "snmp", "mqtt", "opcua", "http"
    config_json TEXT NOT NULL,          -- JSON: protocol-specific connection config
    status      TEXT DEFAULT 'active',  -- "active" or "disabled"
    version     INTEGER DEFAULT 1,
    updated_at  TEXT                    -- ISO 8601
);

-- Reader config_json examples:
--
-- Modbus TCP:
--   {"host": "192.168.1.50", "port": 502, "poll_interval_sec": 20, "timeout_ms": 3000}
--
-- SNMP:
--   {"fetch_interval_sec": 15, "timeout_sec": 10, "worker_count": 2}
--
-- MQTT:
--   {"broker_host": "tcp://192.168.1.10", "broker_port": 1883, "username": "user", "password": "pass"}
--
-- OPC-UA:
--   {"endpoint": "opc.tcp://192.168.1.18:52520", "security_mode": "None"}
--
-- HTTP:
--   {"poll_interval_sec": 30}

-- =============================================
-- Sensors (synced from cloud Postgres sensors table)
-- =============================================

CREATE TABLE IF NOT EXISTS sensors (
    id          TEXT PRIMARY KEY,       -- UUID from cloud
    reader_id   TEXT NOT NULL,          -- FK to readers.id
    name        TEXT NOT NULL,          -- "PM5100_Rack1" (used as Equipment in DataIn)
    config_json TEXT NOT NULL,          -- JSON: what to read (registers, OIDs, topics, nodes)
    tags_json   TEXT,                   -- JSON: user-defined tags
    output      TEXT DEFAULT 'influxdb', -- "influxdb", "live", "influxdb,live"
    table_name  TEXT DEFAULT 'Measurements',
    status      TEXT DEFAULT 'active',
    version     INTEGER DEFAULT 1,
    updated_at  TEXT
);

-- Sensor config_json examples:
--
-- Modbus:
--   {
--     "unit_id": 1,
--     "register_offset": 0,
--     "registers": [
--       {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0},
--       {"field_key": "voltage_ll_v",   "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0}
--     ]
--   }
--
-- SNMP:
--   {
--     "ip_address": "10.0.0.50",
--     "community": "public",
--     "snmp_version": "2c",
--     "oids": [
--       {"field_key": "battery_voltage", "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.8.0"},
--       {"field_key": "output_load",     "oid": ".1.3.6.1.4.1.318.1.1.1.4.2.3.0"}
--     ]
--   }
--
-- MQTT:
--   {
--     "topic": "factory/floor1/sensor_001",
--     "qos": 1,
--     "json_paths": [
--       {"field_key": "temperature", "json_path": "$.data.temperature"},
--       {"field_key": "humidity",    "json_path": "$.data.humidity"}
--     ]
--   }
--
-- OPC-UA:
--   {
--     "nodes": [
--       {"field_key": "temperature", "node_id": "ns=2;i=1001", "type": "float"},
--       {"field_key": "pressure",    "node_id": "ns=2;i=1002", "type": "float"}
--     ]
--   }
--
-- HTTP:
--   {
--     "url": "https://api.example.com/data",
--     "method": "GET",
--     "headers_json": "{}",
--     "auth_type": "bearer",
--     "auth_credentials": "token123",
--     "json_paths": [
--       {"field_key": "temperature", "json_path": "$.readings.temp"}
--     ]
--   }

-- =============================================
-- Sensor Map (replaces sensor_map.json)
-- Used by influx-to-sql to map InfluxDB → cloud sensor UUIDs
-- =============================================

CREATE TABLE IF NOT EXISTS sensor_map (
    measurement_key TEXT PRIMARY KEY,   -- "PM5100_Rack1.active_power_w"
    sensor_id       TEXT NOT NULL,      -- Cloud sensor UUID
    field_key       TEXT NOT NULL,      -- "active_power_w"
    unit            TEXT                -- "W", "V", "A", etc.
);

-- =============================================
-- Containers (Docker services synced from cloud)
-- =============================================

CREATE TABLE IF NOT EXISTS containers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,          -- Docker service name
    image       TEXT NOT NULL,          -- Full image path
    reader_id   TEXT,                   -- FK to readers.id (null for infra containers)
    env_json    TEXT,                   -- JSON: environment variables
    status      TEXT DEFAULT 'active',
    version     INTEGER DEFAULT 1,
    updated_at  TEXT
);

-- =============================================
-- Core-Switch Settings
-- =============================================

CREATE TABLE IF NOT EXISTS coreswitch_settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT
);

-- Default settings inserted by conf-agent on first sync:
-- ('outputs',            '{"influxdb": true, "live": false}')
-- ('batch_size',         '100')
-- ('flush_interval_ms',  '5000')

-- =============================================
-- Swarm State (generated docker-compose.yml)
-- =============================================

CREATE TABLE IF NOT EXISTS swarm_state (
    id            INTEGER PRIMARY KEY DEFAULT 1,
    compose_yml   TEXT,            -- The generated docker-compose.yml
    config_hash   TEXT,            -- SHA-256 of current config
    deployed_hash TEXT,            -- Hash that was last successfully deployed
    updated_at    TEXT
);

-- =============================================
-- Influx-to-SQL Upload Config (replaces uploads.csv)
-- =============================================

CREATE TABLE IF NOT EXISTS influx_uploads (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    device       TEXT NOT NULL,        -- InfluxDB measurement name (Equipment)
    reading      TEXT NOT NULL,        -- Specific metric or "*" for all
    agg_time_min INTEGER NOT NULL,     -- Aggregation interval in minutes
    agg_func     TEXT NOT NULL,        -- SUM, AVG, MAX, MIN
    to_table     TEXT NOT NULL,        -- For reference (actual target is TimescaleDB)
    tag_names    TEXT,                 -- Pipe-separated tag dimensions
    sensor_id    TEXT,                 -- Cloud sensor UUID
    updated_at   TEXT
);
```

## How Config Reload Works

```
1. User changes config in Cloud API
2. Cloud sends change via WebSocket to conf-agent
3. Conf-agent writes update to SQLite (INSERT/UPDATE/DELETE)
4. Conf-agent identifies affected containers
5. Conf-agent calls Docker API: docker stop <container-name>
6. Docker Swarm detects stopped container → recreates it
7. New container starts → opens SQLite read-only → loads fresh config
8. Container runs with new config
```

No polling. No file watching. Clean restart with fresh data.
