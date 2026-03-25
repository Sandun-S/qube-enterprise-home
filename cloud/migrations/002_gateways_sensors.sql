
-- 002_gateways_sensors.sql — Qube Enterprise Phase 2 Schema
-- Adds: protocols, sensor_templates, gateways, sensors, services, service_csv_rows,
--       sensor_readings, registry_config
-- Global device templates are seeded in 003_device_catalog.sql

-- ===================== PROTOCOLS REGISTRY =====================
-- Add new protocols here when a new gateway container is built.
-- image_name is the Docker image suffix used in the compose generator.
CREATE TABLE protocols (
    id                     TEXT PRIMARY KEY,
    label                  TEXT NOT NULL,
    image_name             TEXT NOT NULL,   -- container image suffix (registry added at runtime via QUBE_IMAGE_REGISTRY)
    default_port           INT NOT NULL DEFAULT 0,
    description            TEXT NOT NULL DEFAULT '',
    -- Schema of what to ask the user when adding a gateway of this protocol.
    -- Each entry: {key, label, type (text|number|select), default, options[], required, hint}
    -- UI renders this dynamically — no hardcoding needed when adding new protocols.
    connection_params_schema JSONB NOT NULL DEFAULT '[]',
    -- Schema of address_params to ask when adding a sensor of this protocol.
    addr_params_schema     JSONB NOT NULL DEFAULT '[]',
    is_active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO protocols (id, label, image_name, default_port, description,
                       connection_params_schema, addr_params_schema) VALUES
(
  'modbus_tcp', 'Modbus TCP', 'modbus-gateway', 502,
  'Modbus TCP/IP — industrial PLCs, energy meters, drives',
  '[
    {"key":"host","label":"Device IP address","type":"text","required":true,"placeholder":"192.168.1.100","hint":"IP of the Modbus device"},
    {"key":"port","label":"Modbus port","type":"number","default":502,"required":true}
  ]'::jsonb,
  '[
    {"key":"unit_id","label":"Unit ID (slave address)","type":"number","default":1,"required":true,"hint":"1-247, set on the device"},
    {"key":"register_offset","label":"Register offset","type":"number","default":0,"hint":"Shift all addresses. Usually 0."},
    {"key":"section","label":"Table (measurement name)","type":"text","default":"Measurements","hint":"InfluxDB measurement table name — becomes the Table column in config.csv"}
  ]'::jsonb
),
(
  'opcua', 'OPC-UA', 'opc-ua-gateway', 4840,
  'OPC Unified Architecture — industrial automation, SCADA systems',
  '[
    {"key":"host","label":"OPC-UA endpoint URL","type":"text","required":true,"placeholder":"opc.tcp://192.168.1.18:4840","hint":"Full endpoint URL including protocol"},
    {"key":"port","label":"Port","type":"number","default":4840,"required":true}
  ]'::jsonb,
  '[
    {"key":"freq_sec","label":"Poll frequency (seconds)","type":"number","default":10,"hint":"How often to read node values"}
  ]'::jsonb
),
(
  'snmp', 'SNMP', 'snmp-gateway', 161,
  'Simple Network Management Protocol — UPS, switches, network devices',
  '[
    {"key":"host","label":"Network interface / management host","type":"text","required":false,"placeholder":"0.0.0.0","hint":"Leave blank — SNMP gateway walks each device IP defined per sensor. Only set if your gateway needs to bind to a specific network interface."},
    {"key":"port","label":"SNMP port","type":"number","default":161,"required":true}
  ]'::jsonb,
  '[
    {"key":"device_ip",  "label":"Device IP address","type":"text","required":true,"placeholder":"192.168.1.200","hint":"IP of this specific SNMP device — each sensor can be a different device"},
    {"key":"community",  "label":"Community string", "type":"text","default":"public","required":true,"hint":"Usually public for read-only"},
    {"key":"version",    "label":"SNMP version",     "type":"select","options":["2c","1","3"],"default":"2c"}
  ]'::jsonb
),
(
  'mqtt', 'MQTT', 'mqtt-gateway', 1883,
  'MQTT publish/subscribe — IoT sensors, environmental monitoring',
  '[
    {"key":"host","label":"Broker URL","type":"text","required":true,"placeholder":"tcp://192.168.1.10:1883","hint":"Full broker URL including tcp:// or ssl:// prefix"},
    {"key":"port","label":"Port","type":"number","default":1883,"required":true},
    {"key":"base_topic","label":"Base topic","type":"text","placeholder":"factory/floor2","hint":"Sensors subscribe to base_topic/topic_suffix"},
    {"key":"username","label":"Username","type":"text","placeholder":"mqttuser","hint":"Leave blank if no authentication required"},
    {"key":"password","label":"Password","type":"text","placeholder":"","hint":"MQTT broker password"},
    {"key":"client_id","label":"Client ID","type":"text","placeholder":"qube-gw-01","hint":"Leave blank to auto-generate from Qube ID"}
  ]'::jsonb,
  '[
    {"key":"topic_suffix","label":"Topic suffix","type":"text","placeholder":"sensor_01","hint":"Full topic = base_topic/topic_suffix — one sensor per topic"}
  ]'::jsonb
);

-- ===================== SENSOR TEMPLATES =====================
-- Global (org_id IS NULL) or org-specific templates.
-- config_json encodes the register map / OID map / topic variable paths.
CREATE TABLE sensor_templates (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id              UUID REFERENCES organisations(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    protocol            TEXT NOT NULL REFERENCES protocols(id),  -- FK ensures only valid protocols
    description         TEXT NOT NULL DEFAULT '',
    config_json         JSONB NOT NULL DEFAULT '{}',
    influx_fields_json  JSONB NOT NULL DEFAULT '{}',
    ui_mapping_json     JSONB NOT NULL DEFAULT '{}',
    is_global           BOOLEAN NOT NULL DEFAULT FALSE,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_templates_org ON sensor_templates(org_id);
CREATE INDEX idx_templates_global ON sensor_templates(is_global) WHERE is_global = TRUE;

-- ===================== GATEWAYS =====================
CREATE TABLE gateways (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id         TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    protocol        TEXT NOT NULL REFERENCES protocols(id),      -- FK ensures only valid protocols
    host            TEXT NOT NULL DEFAULT '',
    port            INT NOT NULL DEFAULT 0,
    config_json     JSONB NOT NULL DEFAULT '{}',
    service_image   TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_gateways_qube ON gateways(qube_id);

-- ===================== SENSORS =====================
CREATE TABLE sensors (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    gateway_id      UUID NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    template_id     UUID NOT NULL REFERENCES sensor_templates(id),
    address_params  JSONB NOT NULL DEFAULT '{}',
    tags_json       JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sensors_gateway ON sensors(gateway_id);

-- ===================== SERVICES =====================
-- One Docker service entry per active gateway. Auto-managed.
CREATE TABLE services (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    gateway_id  UUID UNIQUE REFERENCES gateways(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    image       TEXT NOT NULL,
    port        INT NOT NULL DEFAULT 0,
    env_json    JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_services_qube ON services(qube_id);

-- ===================== SERVICE CSV ROWS =====================
-- Each sensor generates N rows (one per reading field).
-- These rows are assembled into the CSV files injected by Conf-Agent.
CREATE TABLE service_csv_rows (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    service_id  UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    sensor_id   UUID REFERENCES sensors(id) ON DELETE CASCADE,
    csv_type    TEXT NOT NULL CHECK (csv_type IN ('registers', 'devices', 'topics', 'oids', 'nodes')),
    row_data    JSONB NOT NULL DEFAULT '{}',
    row_order   INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_csv_rows_service ON service_csv_rows(service_id);

-- ===================== SENSOR READINGS =====================
-- Append-only telemetry table. Written by influx-to-sql via TP-API.
CREATE TABLE sensor_readings (
    time        TIMESTAMPTZ NOT NULL,
    qube_id     TEXT NOT NULL,
    sensor_id   UUID NOT NULL,
    field_key   TEXT NOT NULL,
    value       FLOAT8 NOT NULL,
    unit        TEXT NOT NULL DEFAULT ''
);
-- Hypertable hint for TimescaleDB (no-op on plain Postgres)
-- SELECT create_hypertable('sensor_readings', 'time', if_not_exists => TRUE);
CREATE INDEX idx_readings_sensor_time ON sensor_readings(sensor_id, time DESC);
CREATE INDEX idx_readings_qube_time   ON sensor_readings(qube_id, time DESC);

-- ===================== REGISTRY CONFIGURATION =====================
-- Stores Docker image registry settings per installation.
-- Managed via API: GET/PUT /api/v1/admin/registry
-- Superadmin only. Controls which registry Qubes pull images from.
--
-- Modes:
--   github  — single repo (ghcr.io/sandun-s/qube-enterprise-home)
--   gitlab  — separate repos (registry.gitlab.com/iot-team4/product)
--   custom  — full control, one entry per image

CREATE TABLE registry_config (
    key         TEXT PRIMARY KEY,   -- e.g. "mode", "base_url", "conf_agent", "influx_sql"
    value       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Default: GitHub single-repo mode (works immediately after first push)
INSERT INTO registry_config (key, value, description) VALUES
    ('mode',        'github',  'Registry mode: github | gitlab | custom'),
    ('github_base', 'ghcr.io/sandun-s/qube-enterprise-home',
                               'GitHub GHCR base URL (single-repo mode)'),
    ('gitlab_base', 'registry.gitlab.com/iot-team4/product',
                               'GitLab registry base URL (separate-repo mode)'),
    -- Per-image overrides (used in gitlab/custom mode)
    -- In github mode these are ignored — image = github_base + "/" + short_name
    -- In gitlab mode: use these full image paths (gitlab has enterprise- prefix)
    ('img_conf_agent',  'registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest',
                        'Full image for enterprise-conf-agent'),
    ('img_influx_sql',  'registry.gitlab.com/iot-team4/product/enterprise-influx-to-sql:arm64.latest',
                        'Full image for enterprise-influx-to-sql'),
    ('img_mqtt_gw',     'registry.gitlab.com/iot-team4/product/mqtt-gateway:arm64.latest',
                        'Full image for mqtt-gateway'),
    ('img_modbus',      'registry.gitlab.com/iot-team4/product/modbus-gateway:arm64.latest',
                        'Full image for modbus-gateway (existing Qube Lite image)'),
    ('img_opcua',       'registry.gitlab.com/iot-team4/product/opc-ua-gateway:arm64.latest',
                        'Full image for opc-ua-gateway (existing Qube Lite image)'),
    ('img_snmp',        'registry.gitlab.com/iot-team4/product/snmp-gateway:arm64.latest',
                        'Full image for snmp-gateway (existing Qube Lite image)');
