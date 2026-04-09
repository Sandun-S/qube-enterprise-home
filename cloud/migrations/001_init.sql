-- 001_init.sql — Qube Enterprise v2 Management Database (qubedb)
-- Creates the full schema. No seed data here.
-- Run order: 001_init.sql → 002_global_data.sql → 003_test_seeds.sql

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ===================== ORGANISATIONS =====================
CREATE TABLE IF NOT EXISTS organisations (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name       TEXT NOT NULL,
    org_secret TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== USERS =====================
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id        UUID NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('superadmin', 'admin', 'editor', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== QUBES =====================
CREATE TABLE IF NOT EXISTS qubes (
    id               TEXT PRIMARY KEY,          -- Q-1001, Q-1002, ...
    org_id           UUID REFERENCES organisations(id) ON DELETE SET NULL,
    auth_token_hash  TEXT,                      -- HMAC token (bcrypt hash)
    last_seen        TIMESTAMPTZ,
    status           TEXT NOT NULL DEFAULT 'unclaimed'
                     CHECK (status IN ('online', 'offline', 'unclaimed')),
    location_label   TEXT NOT NULL DEFAULT '',
    claimed_at       TIMESTAMPTZ,
    config_version   INT NOT NULL DEFAULT 0,
    -- Device identity (written by write-to-database.sh at flash time)
    register_key     TEXT UNIQUE,
    maintain_key     TEXT,
    device_type      TEXT NOT NULL DEFAULT 'arm64',
    -- v2: WebSocket + polling
    ws_connected     BOOLEAN NOT NULL DEFAULT FALSE,
    poll_interval_sec INT NOT NULL DEFAULT 30,
    capabilities     JSONB NOT NULL DEFAULT '[]',    -- ["modbus","snmp","opcua","mqtt","http"]
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_qubes_register_key ON qubes(register_key);
CREATE INDEX IF NOT EXISTS idx_qubes_org ON qubes(org_id);

-- ===================== PROTOCOLS =====================
-- Defines available protocols. UI renders dynamic forms from schemas stored here.
CREATE TABLE IF NOT EXISTS protocols (
    id                       TEXT PRIMARY KEY,    -- "modbus_tcp", "snmp", "mqtt", "opcua", "http"
    label                    TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    reader_standard          TEXT NOT NULL DEFAULT 'endpoint'
                             CHECK (reader_standard IN ('endpoint', 'multi_target')),
    is_active                BOOLEAN NOT NULL DEFAULT TRUE,
    -- UI rendering metadata (used by test-ui to build dynamic forms)
    icon                     TEXT NOT NULL DEFAULT '🔧',
    sensor_config_key        TEXT NOT NULL DEFAULT 'entries',      -- array key in sensor_config JSON
    measurement_fields_schema JSONB NOT NULL DEFAULT '[]',         -- column defs for measurement editor
    default_params_schema    JSONB NOT NULL DEFAULT '{"type":"object","properties":{},"required":[]}',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Add UI metadata columns for existing deployments that predate this schema
ALTER TABLE protocols ADD COLUMN IF NOT EXISTS icon                      TEXT  NOT NULL DEFAULT '🔧';
ALTER TABLE protocols ADD COLUMN IF NOT EXISTS sensor_config_key         TEXT  NOT NULL DEFAULT 'entries';
ALTER TABLE protocols ADD COLUMN IF NOT EXISTS measurement_fields_schema JSONB NOT NULL DEFAULT '[]';
ALTER TABLE protocols ADD COLUMN IF NOT EXISTS default_params_schema     JSONB NOT NULL DEFAULT '{"type":"object","properties":{},"required":[]}';

-- ===================== READER TEMPLATES =====================
-- One per protocol (usually). Defines the Docker container + connection schema.
CREATE TABLE IF NOT EXISTS reader_templates (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    protocol            TEXT NOT NULL REFERENCES protocols(id),
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    image_suffix        TEXT NOT NULL,           -- "modbus-reader", "snmp-reader", etc.
    connection_schema   JSONB NOT NULL DEFAULT '{}',  -- JSON Schema for connection params
    env_defaults        JSONB NOT NULL DEFAULT '{}',  -- Default env vars for the container
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reader_templates_protocol_image_suffix_key UNIQUE (protocol, image_suffix)
);
CREATE INDEX IF NOT EXISTS idx_reader_templates_protocol ON reader_templates(protocol);

-- ===================== DEVICE TEMPLATES =====================
-- Many per protocol. Defines what data to collect (registers, OIDs, etc.)
CREATE TABLE IF NOT EXISTS device_templates (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id                UUID REFERENCES organisations(id) ON DELETE CASCADE,  -- NULL = global
    protocol              TEXT NOT NULL REFERENCES protocols(id),
    name                  TEXT NOT NULL,
    manufacturer          TEXT NOT NULL DEFAULT '',
    model                 TEXT NOT NULL DEFAULT '',
    description           TEXT NOT NULL DEFAULT '',
    sensor_config         JSONB NOT NULL DEFAULT '{}',  -- registers/oids/nodes/json_paths
    sensor_params_schema  JSONB NOT NULL DEFAULT '{}',  -- JSON Schema for per-sensor params
    reader_template_id    UUID REFERENCES reader_templates(id),
    is_global             BOOLEAN NOT NULL DEFAULT FALSE,
    version               INT NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_templates_global_protocol_name_key
        UNIQUE (protocol, name, is_global) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX IF NOT EXISTS idx_device_templates_protocol ON device_templates(protocol);
CREATE INDEX IF NOT EXISTS idx_device_templates_org ON device_templates(org_id);
CREATE INDEX IF NOT EXISTS idx_device_templates_global ON device_templates(is_global) WHERE is_global = TRUE;

-- ===================== READERS =====================
-- One Docker container per reader. Created when user adds a reader to a Qube.
CREATE TABLE IF NOT EXISTS readers (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id         TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    protocol        TEXT NOT NULL REFERENCES protocols(id),
    template_id     UUID REFERENCES reader_templates(id),
    config_json     JSONB NOT NULL DEFAULT '{}',    -- Connection config (host, port, etc.)
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'disabled')),
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_readers_qube ON readers(qube_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_readers_qube_protocol_name ON readers(qube_id, protocol, name);

-- ===================== SENSORS =====================
-- Linked to a reader + device template.
CREATE TABLE IF NOT EXISTS sensors (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reader_id       UUID NOT NULL REFERENCES readers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,                 -- Equipment name in DataIn
    template_id     UUID REFERENCES device_templates(id),
    config_json     JSONB NOT NULL DEFAULT '{}',   -- Merged: template.sensor_config + user params
    tags_json       JSONB NOT NULL DEFAULT '{}',   -- User-defined tags
    output          TEXT NOT NULL DEFAULT 'influxdb'
                    CHECK (output IN ('influxdb', 'live', 'influxdb,live')),
    table_name      TEXT NOT NULL DEFAULT 'Measurements',
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'disabled')),
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sensors_reader ON sensors(reader_id);

-- ===================== CONTAINERS =====================
-- Docker containers managed by conf-agent. One per reader + infra containers.
CREATE TABLE IF NOT EXISTS containers (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    reader_id   UUID UNIQUE REFERENCES readers(id) ON DELETE CASCADE,  -- NULL for infra containers
    name        TEXT NOT NULL,           -- Docker service name
    image       TEXT NOT NULL,           -- Full image path
    env_json    JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'disabled')),
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_containers_qube ON containers(qube_id);

-- ===================== CONFIG STATE =====================
-- Tracks config hash per Qube for sync detection.
CREATE TABLE IF NOT EXISTS config_state (
    qube_id          TEXT PRIMARY KEY REFERENCES qubes(id) ON DELETE CASCADE,
    hash             TEXT NOT NULL DEFAULT '',
    config_version   INT NOT NULL DEFAULT 0,
    config_snapshot  JSONB NOT NULL DEFAULT '{}',
    generated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== SWARM HISTORY =====================
-- Audit trail of docker-compose deployments.
CREATE TABLE IF NOT EXISTS swarm_history (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    compose_yml TEXT NOT NULL,
    config_hash TEXT NOT NULL,
    deployed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_swarm_history_qube ON swarm_history(qube_id);

-- ===================== QUBE COMMANDS =====================
-- Remote commands dispatched via WebSocket or polled via TP-API.
CREATE TABLE IF NOT EXISTS qube_commands (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    command     TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'sent', 'executed', 'failed', 'timeout')),
    result      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at     TIMESTAMPTZ,
    executed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_qube_commands_pending ON qube_commands(qube_id, status)
    WHERE status IN ('pending', 'sent');

-- ===================== DISCOVERY SESSIONS =====================
-- Auto-discovery of unknown devices on a Qube's network.
CREATE TABLE IF NOT EXISTS discovery_sessions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id      TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    protocol     TEXT NOT NULL REFERENCES protocols(id),
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    params_json  JSONB NOT NULL DEFAULT '{}',    -- Scan parameters (IP range, etc.)
    results      JSONB NOT NULL DEFAULT '[]',    -- Discovered devices
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_discovery_qube ON discovery_sessions(qube_id);

-- ===================== CORESWITCH SETTINGS =====================
-- Per-Qube core-switch configuration.
CREATE TABLE IF NOT EXISTS coreswitch_settings (
    qube_id    TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value_json TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (qube_id, key)
);

-- ===================== TELEMETRY SETTINGS =====================
-- Per-Qube influx-to-sql upload configuration.
CREATE TABLE IF NOT EXISTS telemetry_settings (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id      TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    device       TEXT NOT NULL,
    reading      TEXT NOT NULL DEFAULT '*',
    agg_time_min INT NOT NULL DEFAULT 1,
    agg_func     TEXT NOT NULL DEFAULT 'AVG'
                 CHECK (agg_func IN ('SUM', 'AVG', 'MAX', 'MIN', 'LAST')),
    sensor_id    UUID REFERENCES sensors(id) ON DELETE SET NULL,
    tag_names    TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_telemetry_settings_qube ON telemetry_settings(qube_id);

-- ===================== WEBSOCKET DELIVERY LOG =====================
CREATE TABLE IF NOT EXISTS ws_delivery_log (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id      TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    message_type TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}',
    delivered    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ws_delivery_pending ON ws_delivery_log(qube_id, delivered)
    WHERE delivered = FALSE;

-- ===================== REGISTRY CONFIG =====================
-- Docker image registry settings.
CREATE TABLE IF NOT EXISTS registry_config (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
