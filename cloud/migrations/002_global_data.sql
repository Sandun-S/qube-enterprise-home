-- 002_global_data.sql — Qube Enterprise v2 Global Seed Data
-- Protocols, reader templates, device templates, registry config.
-- No org-specific or test data here (see 003_test_seeds.sql).

-- ===================== PROTOCOLS =====================

INSERT INTO protocols (id, label, description, reader_standard) VALUES
    ('modbus_tcp', 'Modbus TCP',  'Modbus TCP/IP — industrial PLCs, energy meters, drives',   'endpoint'),
    ('snmp',       'SNMP',        'Simple Network Management Protocol — UPS, switches, network devices', 'multi_target'),
    ('mqtt',       'MQTT',        'MQTT publish/subscribe — IoT sensors, environmental monitoring', 'endpoint'),
    ('opcua',      'OPC-UA',      'OPC Unified Architecture — industrial automation, SCADA',   'endpoint'),
    ('http',       'HTTP/REST',   'HTTP REST API — cloud sensors, weather stations, custom APIs', 'multi_target');

-- ===================== READER TEMPLATES =====================

-- Modbus TCP Reader
INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults) VALUES
(
    'modbus_tcp',
    'Modbus TCP Reader',
    'Reads Modbus TCP registers from industrial devices',
    'modbus-reader',
    '{
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Device IP Address", "format": "ipv4"},
            "port": {"type": "integer", "title": "Port", "default": 502, "minimum": 1, "maximum": 65535},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 20, "minimum": 1, "maximum": 3600},
            "timeout_ms": {"type": "integer", "title": "Timeout (ms)", "default": 3000}
        },
        "required": ["host", "port"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- SNMP Reader
(
    'snmp',
    'SNMP Reader',
    'Polls SNMP devices — one container handles all SNMP targets on the Qube',
    'snmp-reader',
    '{
        "type": "object",
        "properties": {
            "fetch_interval_sec": {"type": "integer", "title": "Fetch Interval (seconds)", "default": 15, "minimum": 5, "maximum": 3600},
            "timeout_sec": {"type": "integer", "title": "Timeout (seconds)", "default": 10},
            "worker_count": {"type": "integer", "title": "Worker Threads", "default": 2, "minimum": 1, "maximum": 10}
        }
    }',
    '{"LOG_LEVEL": "info"}'
),

-- MQTT Reader
(
    'mqtt',
    'MQTT Reader',
    'Subscribes to MQTT topics on a broker — one container per broker',
    'mqtt-reader',
    '{
        "type": "object",
        "properties": {
            "broker_host": {"type": "string", "title": "Broker URL", "description": "Include protocol prefix, e.g. tcp://192.168.1.10"},
            "broker_port": {"type": "integer", "title": "Broker Port", "default": 1883, "minimum": 1, "maximum": 65535},
            "username": {"type": "string", "title": "Username"},
            "password": {"type": "string", "title": "Password", "format": "password"},
            "client_id": {"type": "string", "title": "Client ID", "description": "Leave blank to auto-generate"}
        },
        "required": ["broker_host", "broker_port"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- OPC-UA Reader
(
    'opcua',
    'OPC-UA Reader',
    'Reads OPC-UA nodes from an endpoint — one container per server',
    'opcua-reader',
    '{
        "type": "object",
        "properties": {
            "endpoint": {"type": "string", "title": "OPC-UA Endpoint", "description": "e.g. opc.tcp://192.168.1.18:4840"},
            "security_mode": {"type": "string", "title": "Security Mode", "enum": ["None", "Sign", "SignAndEncrypt"], "default": "None"},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 10, "minimum": 1, "maximum": 3600}
        },
        "required": ["endpoint"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- HTTP Reader
(
    'http',
    'HTTP REST Reader',
    'Polls HTTP/REST endpoints — one container handles all HTTP targets',
    'http-reader',
    '{
        "type": "object",
        "properties": {
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 30, "minimum": 5, "maximum": 3600},
            "timeout_sec": {"type": "integer", "title": "Request Timeout (seconds)", "default": 10},
            "worker_count": {"type": "integer", "title": "Worker Threads", "default": 2, "minimum": 1, "maximum": 10}
        }
    }',
    '{"LOG_LEVEL": "info"}'
);

-- ===================== DEVICE TEMPLATES =====================

-- ── Modbus TCP Devices ──────────────────────────────────────

INSERT INTO device_templates (protocol, name, manufacturer, model, description, is_global, sensor_config, sensor_params_schema) VALUES
(
    'modbus_tcp',
    'Schneider PM5100',
    'Schneider Electric',
    'PM5100',
    '3-phase power meter — active power, voltage L-L, current, energy, PF, frequency',
    TRUE,
    '{
        "registers": [
            {"field_key": "active_power_w",  "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
            {"field_key": "voltage_ll_v",    "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0, "unit": "V"},
            {"field_key": "current_a",       "register_type": "Holding", "address": 3054, "data_type": "float32", "scale": 1.0, "unit": "A"},
            {"field_key": "energy_kwh",      "register_type": "Holding", "address": 3204, "data_type": "float32", "scale": 1.0, "unit": "kWh"},
            {"field_key": "power_factor",    "register_type": "Holding", "address": 3110, "data_type": "float32", "scale": 1.0, "unit": ""},
            {"field_key": "frequency_hz",    "register_type": "Holding", "address": 3060, "data_type": "float32", "scale": 1.0, "unit": "Hz"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
),
(
    'modbus_tcp',
    'Schneider PM2100',
    'Schneider Electric',
    'PM2100',
    'Basic power meter — active power, voltage, energy',
    TRUE,
    '{
        "registers": [
            {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "uint16", "scale": 0.1, "unit": "W"},
            {"field_key": "voltage_v",      "register_type": "Holding", "address": 3020, "data_type": "uint16", "scale": 0.1, "unit": "V"},
            {"field_key": "energy_kwh",     "register_type": "Holding", "address": 3204, "data_type": "uint16", "scale": 0.1, "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
),
(
    'modbus_tcp',
    'Eastron SDM630',
    'Eastron',
    'SDM630',
    '3-phase energy meter — per-phase voltage, current, total power and energy',
    TRUE,
    '{
        "registers": [
            {"field_key": "voltage_l1_v",   "register_type": "Input", "address": 0,   "data_type": "float32", "scale": 1.0, "unit": "V"},
            {"field_key": "voltage_l2_v",   "register_type": "Input", "address": 2,   "data_type": "float32", "scale": 1.0, "unit": "V"},
            {"field_key": "voltage_l3_v",   "register_type": "Input", "address": 4,   "data_type": "float32", "scale": 1.0, "unit": "V"},
            {"field_key": "current_l1_a",   "register_type": "Input", "address": 6,   "data_type": "float32", "scale": 1.0, "unit": "A"},
            {"field_key": "current_l2_a",   "register_type": "Input", "address": 8,   "data_type": "float32", "scale": 1.0, "unit": "A"},
            {"field_key": "current_l3_a",   "register_type": "Input", "address": 10,  "data_type": "float32", "scale": 1.0, "unit": "A"},
            {"field_key": "active_power_w", "register_type": "Input", "address": 52,  "data_type": "float32", "scale": 1.0, "unit": "W"},
            {"field_key": "energy_kwh",     "register_type": "Input", "address": 342, "data_type": "float32", "scale": 1.0, "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
),
(
    'modbus_tcp',
    'Generic Modbus Register',
    '',
    '',
    'Generic Modbus TCP holding register — single value. Customize after adding.',
    TRUE,
    '{
        "registers": [
            {"field_key": "value", "register_type": "Holding", "address": 0, "data_type": "uint16", "scale": 1.0, "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
),

-- ── SNMP Devices ────────────────────────────────────────────

(
    'snmp',
    'APC Smart-UPS',
    'APC',
    'Smart-UPS',
    'APC Smart-UPS — battery capacity, runtime, input/output voltage, load',
    TRUE,
    '{
        "oids": [
            {"field_key": "battery_capacity_pct", "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.1.0",  "unit": "%"},
            {"field_key": "battery_runtime_min",  "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.3.0",  "unit": "min"},
            {"field_key": "input_voltage_v",      "oid": ".1.3.6.1.4.1.318.1.1.1.3.2.1.0",  "unit": "V"},
            {"field_key": "output_voltage_v",     "oid": ".1.3.6.1.4.1.318.1.1.1.4.2.1.0",  "unit": "V"},
            {"field_key": "load_pct",             "oid": ".1.3.6.1.4.1.318.1.1.1.4.2.3.0",  "unit": "%"},
            {"field_key": "battery_temp_c",       "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.4.0",  "unit": "C"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["ip_address"]
    }'
),
(
    'snmp',
    'Liebert GXT RT UPS',
    'Liebert',
    'GXT RT',
    'Liebert GXT RT UPS — battery, runtime, voltages, load',
    TRUE,
    '{
        "oids": [
            {"field_key": "battery_capacity_pct", "oid": ".1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.1", "unit": "%"},
            {"field_key": "battery_runtime_min",  "oid": ".1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.2", "unit": "min"},
            {"field_key": "input_voltage_v",      "oid": ".1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.3", "unit": "V"},
            {"field_key": "output_voltage_v",     "oid": ".1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.4", "unit": "V"},
            {"field_key": "load_pct",             "oid": ".1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.5", "unit": "%"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["ip_address"]
    }'
),
(
    'snmp',
    'Vertiv ITA2 UPS',
    'Vertiv',
    'ITA2',
    'Vertiv ITA2 3-phase UPS — input/output voltages, currents, load, battery',
    TRUE,
    '{
        "oids": [
            {"field_key": "systemStatus",                       "oid": ".1.3.6.1.4.1.13400.2.54.2.1.1.0",  "unit": ""},
            {"field_key": "upsOutputSource",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.1.2.0",  "unit": ""},
            {"field_key": "inputPhaseVoltageA",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.2.1.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageB",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.2.2.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageC",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.2.3.0",  "unit": "V"},
            {"field_key": "inputFrequency",                     "oid": ".1.3.6.1.4.1.13400.2.54.2.2.4.0",  "unit": "Hz"},
            {"field_key": "outputPhaseVoltageA",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.1.0",  "unit": "V"},
            {"field_key": "outputPhaseVoltageB",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.2.0",  "unit": "V"},
            {"field_key": "outputPhaseVoltageC",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.3.0",  "unit": "V"},
            {"field_key": "outputCurrentA",                     "oid": ".1.3.6.1.4.1.13400.2.54.2.3.4.0",  "unit": "A"},
            {"field_key": "outputCurrentB",                     "oid": ".1.3.6.1.4.1.13400.2.54.2.3.5.0",  "unit": "A"},
            {"field_key": "outputCurrentC",                     "oid": ".1.3.6.1.4.1.13400.2.54.2.3.6.0",  "unit": "A"},
            {"field_key": "outputFrequency",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.3.7.0",  "unit": "Hz"},
            {"field_key": "outputActivePowerA",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.3.8.0",  "unit": "W"},
            {"field_key": "outputActivePowerB",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.3.9.0",  "unit": "W"},
            {"field_key": "outputActivePowerC",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.3.10.0", "unit": "W"},
            {"field_key": "outputLoadA",                        "oid": ".1.3.6.1.4.1.13400.2.54.2.3.14.0", "unit": "%"},
            {"field_key": "outputLoadB",                        "oid": ".1.3.6.1.4.1.13400.2.54.2.3.15.0", "unit": "%"},
            {"field_key": "outputLoadC",                        "oid": ".1.3.6.1.4.1.13400.2.54.2.3.16.0", "unit": "%"},
            {"field_key": "batteryRemainsTime",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.5.7.0",  "unit": "min"},
            {"field_key": "batteryTemperature",                 "oid": ".1.3.6.1.4.1.13400.2.54.2.5.8.0",  "unit": "C"},
            {"field_key": "batteryCapacity",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.5.10.0", "unit": "%"},
            {"field_key": "positiveBatteryVoltage",             "oid": ".1.3.6.1.4.1.13400.2.54.2.5.1.0",  "unit": "V"},
            {"field_key": "positiveBatteryChargingCurrent",     "oid": ".1.3.6.1.4.1.13400.2.54.2.5.3.0",  "unit": "A"},
            {"field_key": "positiveBatteryDischargingCurrent",  "oid": ".1.3.6.1.4.1.13400.2.54.2.5.4.0",  "unit": "A"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["ip_address"]
    }'
),

-- ── OPC-UA Devices ──────────────────────────────────────────

(
    'opcua',
    'Generic OPC-UA Power Meter',
    '',
    '',
    'Generic OPC-UA power meter — update node IDs to match your server namespace',
    TRUE,
    '{
        "nodes": [
            {"field_key": "active_power_w", "node_id": "ns=2;i=1001", "type": "float", "unit": "W"},
            {"field_key": "voltage_v",      "node_id": "ns=2;i=1002", "type": "float", "unit": "V"},
            {"field_key": "current_a",      "node_id": "ns=2;i=1003", "type": "float", "unit": "A"},
            {"field_key": "energy_kwh",     "node_id": "ns=2;i=1004", "type": "float", "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "namespace_index": {"type": "integer", "title": "Namespace Index", "default": 2}
        }
    }'
),
(
    'opcua',
    'Generic OPC-UA Temperature',
    '',
    '',
    'Generic OPC-UA temperature/humidity sensor',
    TRUE,
    '{
        "nodes": [
            {"field_key": "temperature_c", "node_id": "ns=2;i=2001", "type": "float", "unit": "C"},
            {"field_key": "humidity_pct",  "node_id": "ns=2;i=2002", "type": "float", "unit": "%"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "namespace_index": {"type": "integer", "title": "Namespace Index", "default": 2}
        }
    }'
),

-- ── MQTT Devices ────────────────────────────────────────────

(
    'mqtt',
    'Generic MQTT JSON Sensor',
    '',
    '',
    'MQTT device publishing JSON payloads — configure topic when adding',
    TRUE,
    '{
        "json_paths": [
            {"field_key": "value",       "json_path": "$.value",       "unit": ""},
            {"field_key": "temperature", "json_path": "$.temperature", "unit": "C"},
            {"field_key": "humidity",    "json_path": "$.humidity",    "unit": "%"},
            {"field_key": "pressure",    "json_path": "$.pressure",    "unit": "hPa"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "topic": {"type": "string", "title": "MQTT Topic"},
            "qos": {"type": "integer", "title": "QoS Level", "enum": [0, 1, 2], "default": 1},
            "payload_format": {"type": "string", "title": "Payload Format", "enum": ["json", "senml"], "default": "json"}
        },
        "required": ["topic"]
    }'
),
(
    'mqtt',
    'MQTT Energy Monitor (Shelly EM)',
    'Shelly',
    'EM',
    'Shelly EM MQTT energy monitor — power and energy consumption',
    TRUE,
    '{
        "json_paths": [
            {"field_key": "active_power_w", "json_path": "$.power",   "unit": "W"},
            {"field_key": "energy_kwh",     "json_path": "$.total",   "unit": "kWh"},
            {"field_key": "voltage_v",      "json_path": "$.voltage", "unit": "V"},
            {"field_key": "current_a",      "json_path": "$.current", "unit": "A"},
            {"field_key": "power_factor",   "json_path": "$.pf",     "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "topic": {"type": "string", "title": "MQTT Topic"},
            "qos": {"type": "integer", "title": "QoS Level", "enum": [0, 1, 2], "default": 1}
        },
        "required": ["topic"]
    }'
),

-- ── HTTP Devices ────────────────────────────────────────────

(
    'http',
    'Generic HTTP JSON Endpoint',
    '',
    '',
    'HTTP REST API sensor — polls a JSON endpoint and extracts values',
    TRUE,
    '{
        "json_paths": [
            {"field_key": "value", "json_path": "$.readings.value", "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "url": {"type": "string", "title": "Endpoint URL", "format": "uri"},
            "method": {"type": "string", "title": "HTTP Method", "enum": ["GET", "POST"], "default": "GET"},
            "auth_type": {"type": "string", "title": "Auth Type", "enum": ["none", "basic", "bearer", "api_key"], "default": "none"},
            "auth_credentials": {"type": "string", "title": "Credentials", "format": "password"},
            "headers_json": {"type": "string", "title": "Custom Headers (JSON)"}
        },
        "required": ["url"]
    }'
);

-- ===================== REGISTRY CONFIG =====================

INSERT INTO registry_config (key, value, description) VALUES
    ('mode',        'github',  'Registry mode: github | gitlab | custom'),
    ('github_base', 'ghcr.io/sandun-s/qube-enterprise-home',
                               'GitHub GHCR base URL (single-repo mode)'),
    ('gitlab_base', 'registry.gitlab.com/iot-team4/product',
                               'GitLab registry base URL (separate-repo mode)'),
    ('img_conf_agent',   'registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest',
                         'Full image for enterprise-conf-agent'),
    ('img_influx_sql',   'registry.gitlab.com/iot-team4/product/enterprise-influx-to-sql:arm64.latest',
                         'Full image for enterprise-influx-to-sql'),
    ('img_modbus_reader','registry.gitlab.com/iot-team4/product/modbus-reader:arm64.latest',
                         'Full image for modbus-reader'),
    ('img_snmp_reader',  'registry.gitlab.com/iot-team4/product/snmp-reader:arm64.latest',
                         'Full image for snmp-reader'),
    ('img_mqtt_reader',  'registry.gitlab.com/iot-team4/product/mqtt-reader:arm64.latest',
                         'Full image for mqtt-reader'),
    ('img_opcua_reader', 'registry.gitlab.com/iot-team4/product/opcua-reader:arm64.latest',
                         'Full image for opcua-reader'),
    ('img_http_reader',  'registry.gitlab.com/iot-team4/product/http-reader:arm64.latest',
                         'Full image for http-reader');
