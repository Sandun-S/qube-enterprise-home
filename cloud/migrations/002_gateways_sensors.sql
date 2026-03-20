-- 002_gateways_sensors.sql — Qube Enterprise Phase 2 Schema
-- Adds: sensor_templates, gateways, sensors, services, service_csv_rows, sensor_readings

-- ===================== SENSOR TEMPLATES =====================
-- Global (org_id IS NULL) or org-specific templates.
-- config_json encodes the register map / OID map / topic variable paths.
CREATE TABLE sensor_templates (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id              UUID REFERENCES organisations(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    protocol            TEXT NOT NULL CHECK (protocol IN ('modbus_tcp', 'mqtt', 'opcua', 'snmp')),
    description         TEXT NOT NULL DEFAULT '',
    config_json         JSONB NOT NULL DEFAULT '{}',
    influx_fields_json  JSONB NOT NULL DEFAULT '{}',
    ui_mapping_json     JSONB NOT NULL DEFAULT '{}',
    is_global           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_templates_org ON sensor_templates(org_id);
CREATE INDEX idx_templates_global ON sensor_templates(is_global) WHERE is_global = TRUE;

-- ===================== GATEWAYS =====================
CREATE TABLE gateways (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id         TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    protocol        TEXT NOT NULL CHECK (protocol IN ('modbus_tcp', 'mqtt', 'opcua', 'snmp')),
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

-- ===================== SEED: GLOBAL TEMPLATES =====================

-- Modbus: Schneider PM5100 Power Meter
INSERT INTO sensor_templates (name, protocol, description, is_global, config_json, influx_fields_json) VALUES
(
  'Schneider PM5100',
  'modbus_tcp',
  'Schneider Electric PM5100 power meter — active power, voltage, current',
  TRUE,
  '{
    "registers": [
      {"address": 3000, "register_type": "holding", "data_type": "float32", "count": 2, "scale": 1.0,   "field_key": "active_power_w",    "unit": "W"},
      {"address": 3028, "register_type": "holding", "data_type": "float32", "count": 2, "scale": 1.0,   "field_key": "voltage_ll_v",      "unit": "V"},
      {"address": 3054, "register_type": "holding", "data_type": "float32", "count": 2, "scale": 1.0,   "field_key": "current_a",         "unit": "A"},
      {"address": 3204, "register_type": "holding", "data_type": "float32", "count": 2, "scale": 0.001, "field_key": "energy_kwh",        "unit": "kWh"}
    ]
  }',
  '{
    "active_power_w":  {"display_label": "Active Power",  "unit": "W"},
    "voltage_ll_v":    {"display_label": "Voltage (L-L)", "unit": "V"},
    "current_a":       {"display_label": "Current",       "unit": "A"},
    "energy_kwh":      {"display_label": "Energy",        "unit": "kWh"}
  }'
),
-- MQTT: Generic JSON sensor
(
  'Generic MQTT Sensor',
  'mqtt',
  'Generic MQTT sensor — reads value and status from a JSON payload',
  TRUE,
  '{
    "topic_pattern": "{base_topic}/{topic_suffix}",
    "readings": [
      {"json_path": "$.value",  "field_key": "value",  "unit": ""},
      {"json_path": "$.status", "field_key": "status", "unit": ""}
    ]
  }',
  '{
    "value":  {"display_label": "Value",  "unit": ""},
    "status": {"display_label": "Status", "unit": ""}
  }'
),
-- SNMP: UPS Battery
(
  'APC UPS Battery',
  'snmp',
  'APC Smart-UPS — battery capacity, runtime, input/output voltage',
  TRUE,
  '{
    "oids": [
      {"oid": "1.3.6.1.4.1.318.1.1.1.2.2.1.0",  "field_key": "battery_capacity_pct", "unit": "%"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.2.2.3.0",  "field_key": "battery_runtime_min",  "unit": "min"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.3.2.1.0",  "field_key": "input_voltage_v",      "unit": "V"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.4.2.1.0",  "field_key": "output_voltage_v",     "unit": "V"}
    ]
  }',
  '{
    "battery_capacity_pct": {"display_label": "Battery Capacity", "unit": "%"},
    "battery_runtime_min":  {"display_label": "Battery Runtime",  "unit": "min"},
    "input_voltage_v":      {"display_label": "Input Voltage",    "unit": "V"},
    "output_voltage_v":     {"display_label": "Output Voltage",   "unit": "V"}
  }'
);

-- ===================== ADDITIONAL GLOBAL TEMPLATES =====================

-- OPC-UA: Generic power meter nodes
INSERT INTO sensor_templates (name, protocol, description, is_global, config_json, influx_fields_json) VALUES
(
  'Generic OPC-UA Power Meter',
  'opcua',
  'Generic OPC-UA power meter — active power, voltage, current, energy',
  TRUE,
  '{
    "nodes": [
      {"node_id": "ns=2;points/ActivePower",  "field_key": "active_power_w",  "data_type": "float", "table": "Measurements"},
      {"node_id": "ns=2;points/Voltage",       "field_key": "voltage_v",        "data_type": "float", "table": "Measurements"},
      {"node_id": "ns=2;points/Current",       "field_key": "current_a",        "data_type": "float", "table": "Measurements"},
      {"node_id": "ns=2;points/Energy",        "field_key": "energy_kwh",       "data_type": "float", "table": "Measurements"}
    ]
  }',
  '{
    "active_power_w": {"display_label": "Active Power", "unit": "W"},
    "voltage_v":      {"display_label": "Voltage",      "unit": "V"},
    "current_a":      {"display_label": "Current",      "unit": "A"},
    "energy_kwh":     {"display_label": "Energy",       "unit": "kWh"}
  }'
),
-- SNMP: GXT RT UPS (matching the real snmp example from Qube Lite)
(
  'GXT RT UPS (SNMP)',
  'snmp',
  'Liebert GXT RT UPS — battery status, runtime, load, input/output voltage',
  TRUE,
  '{
    "oids": [
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.1",  "field_key": "battery_capacity_pct", "type": "gauge"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.2",  "field_key": "battery_runtime_min",  "type": "gauge"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.3",  "field_key": "input_voltage_v",      "type": "gauge"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.4",  "field_key": "output_voltage_v",     "type": "gauge"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.5",  "field_key": "load_pct",             "type": "gauge"}
    ]
  }',
  '{
    "battery_capacity_pct": {"display_label": "Battery Capacity", "unit": "%"},
    "battery_runtime_min":  {"display_label": "Battery Runtime",  "unit": "min"},
    "input_voltage_v":      {"display_label": "Input Voltage",    "unit": "V"},
    "output_voltage_v":     {"display_label": "Output Voltage",   "unit": "V"},
    "load_pct":             {"display_label": "Load",             "unit": "%"}
  }'
),
-- MQTT: Generic JSON sensor (topic + JSON path mapping)
(
  'Generic MQTT JSON Sensor',
  'mqtt',
  'Generic MQTT sensor that reads value fields from JSON payload',
  TRUE,
  '{
    "topic_pattern": "{base_topic}/{topic_suffix}",
    "readings": [
      {"json_path": "$.value",       "field_key": "value",       "unit": ""},
      {"json_path": "$.temperature", "field_key": "temperature", "unit": "C"},
      {"json_path": "$.humidity",    "field_key": "humidity",    "unit": "%"}
    ]
  }',
  '{
    "value":       {"display_label": "Value",       "unit": ""},
    "temperature": {"display_label": "Temperature", "unit": "C"},
    "humidity":    {"display_label": "Humidity",    "unit": "%"}
  }'
);
