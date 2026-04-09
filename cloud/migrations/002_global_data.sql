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

-- Modbus TCP Reader (endpoint: one container per device/gateway)
-- slave_id = Modbus unit ID of the target device (1–247)
-- single_read_count = max registers per request (reduce if device is slow)
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
            "port": {"type": "integer", "title": "Modbus TCP Port", "default": 502, "minimum": 1, "maximum": 65535},
            "slave_id": {"type": "integer", "title": "Slave / Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 10, "minimum": 1, "maximum": 3600},
            "single_read_count": {"type": "integer", "title": "Max Registers Per Request", "default": 100, "minimum": 1, "maximum": 125}
        },
        "required": ["host", "port"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- SNMP Reader (multi-target: one container per Qube, each sensor = one SNMP device)
-- Reader-level config: poll timing and retry behaviour only.
-- Per-device config (host, community, OIDs) lives in each sensor's config_json.
(
    'snmp',
    'SNMP Reader',
    'Polls SNMP devices — one container handles all SNMP targets on the Qube',
    'snmp-reader',
    '{
        "type": "object",
        "properties": {
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 30, "minimum": 5, "maximum": 3600},
            "timeout_ms": {"type": "integer", "title": "Request Timeout (ms)", "default": 5000},
            "retries": {"type": "integer", "title": "Retries per Device", "default": 2, "minimum": 0, "maximum": 10}
        }
    }',
    '{"LOG_LEVEL": "info"}'
),

-- MQTT Reader (endpoint: one container per broker)
-- Reader-level config: broker connection. Per-device: topics defined in each sensor's measurements.
(
    'mqtt',
    'MQTT Reader',
    'Subscribes to MQTT topics on a broker — one container per broker',
    'mqtt-reader',
    '{
        "type": "object",
        "properties": {
            "broker_host": {"type": "string", "title": "Broker Host (IP or hostname)", "description": "e.g. 192.168.1.10 or broker.example.com"},
            "broker_port": {"type": "integer", "title": "Broker Port", "default": 1883, "minimum": 1, "maximum": 65535},
            "username": {"type": "string", "title": "Username"},
            "password": {"type": "string", "title": "Password", "format": "password"},
            "client_id": {"type": "string", "title": "Client ID", "description": "Leave blank to auto-generate"},
            "qos": {"type": "integer", "title": "QoS Level (0=at most once, 1=at least once, 2=exactly once)", "default": 1, "minimum": 0, "maximum": 2}
        },
        "required": ["broker_host", "broker_port"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- OPC-UA Reader (endpoint: one container per OPC-UA server)
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
            "security_policy": {"type": "string", "title": "Security Policy", "enum": ["None", "Basic128Rsa15", "Basic256", "Basic256Sha256"], "default": "None"},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 10, "minimum": 1, "maximum": 3600}
        },
        "required": ["endpoint"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- HTTP Reader (multi-target: one container per Qube, each sensor polls its own URL)
-- Reader-level config: timing/concurrency only. Per-device: URL + auth in sensor config.
(
    'http',
    'HTTP REST Reader',
    'Polls HTTP/REST endpoints — one container handles all HTTP targets on the Qube',
    'http-reader',
    '{
        "type": "object",
        "properties": {
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 30, "minimum": 5, "maximum": 3600},
            "timeout_ms": {"type": "integer", "title": "Request Timeout (ms)", "default": 10000}
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
            "host": {"type": "string", "title": "Device IP Address", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["host"]
    }'
),
(
    'snmp',
    'Liebert GXT RT UPS',
    'Liebert',
    'GXT RT',
    'Liebert GXT RT UPS — RFC 1628 MIB — battery status, runtime, input/output voltage, current, power, load',
    TRUE,
    '{
        "oids": [
            {"field_key": "upsBatteryStatus",             "oid": ".1.3.6.1.2.1.33.1.2.1.0",     "unit": ""},
            {"field_key": "upsSecondsOnBattery",          "oid": ".1.3.6.1.2.1.33.1.2.2.0",     "unit": "s"},
            {"field_key": "upsEstimatedMinutesRemaining", "oid": ".1.3.6.1.2.1.33.1.2.3.0",     "unit": "min"},
            {"field_key": "upsEstimatedChargeRemaining",  "oid": ".1.3.6.1.2.1.33.1.2.4.0",     "unit": "%"},
            {"field_key": "upsBatteryVoltage",            "oid": ".1.3.6.1.2.1.33.1.2.5.0",     "unit": "0.1V"},
            {"field_key": "upsBatteryCurrent",            "oid": ".1.3.6.1.2.1.33.1.2.6.0",     "unit": "0.1A"},
            {"field_key": "upsBatteryTemperature",        "oid": ".1.3.6.1.2.1.33.1.2.7.0",     "unit": "C"},
            {"field_key": "upsInputFrequency",            "oid": ".1.3.6.1.2.1.33.1.3.3.1.2.1", "unit": "0.1Hz"},
            {"field_key": "upsInputVoltage",              "oid": ".1.3.6.1.2.1.33.1.3.3.1.3.1", "unit": "V"},
            {"field_key": "upsInputCurrent",              "oid": ".1.3.6.1.2.1.33.1.3.3.1.4.1", "unit": "0.1A"},
            {"field_key": "upsInputTruePower",            "oid": ".1.3.6.1.2.1.33.1.3.3.1.5.1", "unit": "W"},
            {"field_key": "upsOutputSource",              "oid": ".1.3.6.1.2.1.33.1.4.1.0",     "unit": ""},
            {"field_key": "upsOutputFrequency",           "oid": ".1.3.6.1.2.1.33.1.4.2.0",     "unit": "0.1Hz"},
            {"field_key": "upsOutputVoltage",             "oid": ".1.3.6.1.2.1.33.1.4.4.1.2.1", "unit": "V"},
            {"field_key": "upsOutputCurrent",             "oid": ".1.3.6.1.2.1.33.1.4.4.1.3.1", "unit": "0.1A"},
            {"field_key": "upsOutputPower",               "oid": ".1.3.6.1.2.1.33.1.4.4.1.4.1", "unit": "W"},
            {"field_key": "upsOutputPercentLoad",         "oid": ".1.3.6.1.2.1.33.1.4.4.1.5.1", "unit": "%"},
            {"field_key": "upsBypassFrequency",           "oid": ".1.3.6.1.2.1.33.1.5.1.0",     "unit": "0.1Hz"},
            {"field_key": "upsBypassVoltage",             "oid": ".1.3.6.1.2.1.33.1.5.3.1.2.1", "unit": "V"},
            {"field_key": "upsAlarmsPresent",             "oid": ".1.3.6.1.2.1.33.1.6.1.0",     "unit": "count"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Device IP Address", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["host"]
    }'
),
(
    'snmp',
    'Vertiv ITA2 UPS',
    'Vertiv',
    'ITA2',
    'Vertiv ITA2 3-phase UPS — full telemetry: input/output voltages, currents, power, load, bypass, battery',
    TRUE,
    '{
        "oids": [
            {"field_key": "systemStatus",                      "oid": ".1.3.6.1.4.1.13400.2.54.2.1.1.0",  "unit": ""},
            {"field_key": "upsOutputSource",                   "oid": ".1.3.6.1.4.1.13400.2.54.2.1.2.0",  "unit": ""},
            {"field_key": "inputPhaseVoltageA",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.1.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageB",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.2.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageC",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.3.0",  "unit": "V"},
            {"field_key": "inputFrequency",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.2.4.0",  "unit": "Hz"},
            {"field_key": "inputPhaseCurrentA",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.5.0",  "unit": "A"},
            {"field_key": "inputPhaseCurrentB",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.6.0",  "unit": "A"},
            {"field_key": "inputPhaseCurrentC",                "oid": ".1.3.6.1.4.1.13400.2.54.2.2.7.0",  "unit": "A"},
            {"field_key": "outputPhaseVoltageA",               "oid": ".1.3.6.1.4.1.13400.2.54.2.3.1.0",  "unit": "V"},
            {"field_key": "outputPhaseVoltageB",               "oid": ".1.3.6.1.4.1.13400.2.54.2.3.2.0",  "unit": "V"},
            {"field_key": "outputPhaseVoltageC",               "oid": ".1.3.6.1.4.1.13400.2.54.2.3.3.0",  "unit": "V"},
            {"field_key": "outputCurrentA",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.3.4.0",  "unit": "A"},
            {"field_key": "outputCurrentB",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.3.5.0",  "unit": "A"},
            {"field_key": "outputCurrentC",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.3.6.0",  "unit": "A"},
            {"field_key": "outputFrequency",                   "oid": ".1.3.6.1.4.1.13400.2.54.2.3.7.0",  "unit": "Hz"},
            {"field_key": "outputActivePowerA",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.8.0",  "unit": "W"},
            {"field_key": "outputActivePowerB",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.9.0",  "unit": "W"},
            {"field_key": "outputActivePowerC",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.10.0", "unit": "W"},
            {"field_key": "outputApparentPowerA",              "oid": ".1.3.6.1.4.1.13400.2.54.2.3.11.0", "unit": "VA"},
            {"field_key": "outputApparentPowerB",              "oid": ".1.3.6.1.4.1.13400.2.54.2.3.12.0", "unit": "VA"},
            {"field_key": "outputApparentPowerC",              "oid": ".1.3.6.1.4.1.13400.2.54.2.3.13.0", "unit": "VA"},
            {"field_key": "outputLoadA",                       "oid": ".1.3.6.1.4.1.13400.2.54.2.3.14.0", "unit": "%"},
            {"field_key": "outputLoadB",                       "oid": ".1.3.6.1.4.1.13400.2.54.2.3.15.0", "unit": "%"},
            {"field_key": "outputLoadC",                       "oid": ".1.3.6.1.4.1.13400.2.54.2.3.16.0", "unit": "%"},
            {"field_key": "outputPowerFactorA",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.17.0", "unit": ""},
            {"field_key": "outputPowerFactorB",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.18.0", "unit": ""},
            {"field_key": "outputPowerFactorC",                "oid": ".1.3.6.1.4.1.13400.2.54.2.3.19.0", "unit": ""},
            {"field_key": "bypassVoltageA",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.4.1.0",  "unit": "V"},
            {"field_key": "bypassVoltageB",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.4.2.0",  "unit": "V"},
            {"field_key": "bypassVoltageC",                    "oid": ".1.3.6.1.4.1.13400.2.54.2.4.3.0",  "unit": "V"},
            {"field_key": "bypassFrequency",                   "oid": ".1.3.6.1.4.1.13400.2.54.2.4.4.0",  "unit": "Hz"},
            {"field_key": "positiveBatteryVoltage",            "oid": ".1.3.6.1.4.1.13400.2.54.2.5.1.0",  "unit": "V"},
            {"field_key": "negativeBatteryVoltage",            "oid": ".1.3.6.1.4.1.13400.2.54.2.5.2.0",  "unit": "V"},
            {"field_key": "positiveBatteryChargingCurrent",    "oid": ".1.3.6.1.4.1.13400.2.54.2.5.3.0",  "unit": "A"},
            {"field_key": "positiveBatteryDischargingCurrent", "oid": ".1.3.6.1.4.1.13400.2.54.2.5.4.0",  "unit": "A"},
            {"field_key": "negativeBatteryChargingCurrent",    "oid": ".1.3.6.1.4.1.13400.2.54.2.5.5.0",  "unit": "A"},
            {"field_key": "negativeBatteryDischargingCurrent", "oid": ".1.3.6.1.4.1.13400.2.54.2.5.6.0",  "unit": "A"},
            {"field_key": "batteryRemainsTime",                "oid": ".1.3.6.1.4.1.13400.2.54.2.5.7.0",  "unit": "min"},
            {"field_key": "batteryTemperature",                "oid": ".1.3.6.1.4.1.13400.2.54.2.5.8.0",  "unit": "C"},
            {"field_key": "batteryEnvironmentTemperature",     "oid": ".1.3.6.1.4.1.13400.2.54.2.5.9.0",  "unit": "C"},
            {"field_key": "batteryCapacity",                   "oid": ".1.3.6.1.4.1.13400.2.54.2.5.10.0", "unit": "%"},
            {"field_key": "batteryDischargeTimes",             "oid": ".1.3.6.1.4.1.13400.2.54.2.5.11.0", "unit": "count"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Device IP Address", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["host"]
    }'
),

-- Vertiv APM150 UPS (Synergi/Vertiv enterprise UPS — MIB 1.3.6.1.4.1.13400.2.20.*)
(
    'snmp',
    'Vertiv APM150 UPS',
    'Vertiv',
    'APM150',
    'Vertiv APM150 3-phase UPS — full telemetry: input/output voltages, currents, power, load, bypass, battery',
    TRUE,
    '{
        "oids": [
            {"field_key": "systemStatus",         "oid": ".1.3.6.1.4.1.13400.2.20.2.1.1.0",  "unit": ""},
            {"field_key": "inputPhaseVoltageA",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.1.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageB",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.2.0",  "unit": "V"},
            {"field_key": "inputPhaseVoltageC",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.3.0",  "unit": "V"},
            {"field_key": "inputPhaseCurrentA",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.7.0",  "unit": "A"},
            {"field_key": "inputPhaseCurrentB",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.8.0",  "unit": "A"},
            {"field_key": "inputPhaseCurrentC",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.9.0",  "unit": "A"},
            {"field_key": "inputFrequency",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.10.0", "unit": "Hz"},
            {"field_key": "outputPhaseVoltageA",  "oid": ".1.3.6.1.4.1.13400.2.20.2.4.16.0", "unit": "V"},
            {"field_key": "outputPhaseVoltageB",  "oid": ".1.3.6.1.4.1.13400.2.20.2.4.17.0", "unit": "V"},
            {"field_key": "outputPhaseVoltageC",  "oid": ".1.3.6.1.4.1.13400.2.20.2.4.18.0", "unit": "V"},
            {"field_key": "outputCurrentA",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.19.0", "unit": "A"},
            {"field_key": "outputCurrentB",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.20.0", "unit": "A"},
            {"field_key": "outputCurrentC",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.21.0", "unit": "A"},
            {"field_key": "outputFrequency",      "oid": ".1.3.6.1.4.1.13400.2.20.2.4.22.0", "unit": "Hz"},
            {"field_key": "outputPowerFactorA",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.23.0", "unit": ""},
            {"field_key": "outputPowerFactorB",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.24.0", "unit": ""},
            {"field_key": "outputPowerFactorC",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.25.0", "unit": ""},
            {"field_key": "outputActivePowerA",   "oid": ".1.3.6.1.4.1.13400.2.20.2.2.1.0",  "unit": "W"},
            {"field_key": "outputActivePowerB",   "oid": ".1.3.6.1.4.1.13400.2.20.2.2.2.0",  "unit": "W"},
            {"field_key": "outputActivePowerC",   "oid": ".1.3.6.1.4.1.13400.2.20.2.2.3.0",  "unit": "W"},
            {"field_key": "outputApparentPowerA", "oid": ".1.3.6.1.4.1.13400.2.20.2.2.4.0",  "unit": "VA"},
            {"field_key": "outputApparentPowerB", "oid": ".1.3.6.1.4.1.13400.2.20.2.2.5.0",  "unit": "VA"},
            {"field_key": "outputApparentPowerC", "oid": ".1.3.6.1.4.1.13400.2.20.2.2.6.0",  "unit": "VA"},
            {"field_key": "outputLoadA",          "oid": ".1.3.6.1.4.1.13400.2.20.2.2.7.0",  "unit": "%"},
            {"field_key": "outputLoadB",          "oid": ".1.3.6.1.4.1.13400.2.20.2.2.8.0",  "unit": "%"},
            {"field_key": "outputLoadC",          "oid": ".1.3.6.1.4.1.13400.2.20.2.2.9.0",  "unit": "%"},
            {"field_key": "outputCrestFactorA",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.38.0", "unit": ""},
            {"field_key": "outputCrestFactorB",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.39.0", "unit": ""},
            {"field_key": "outputCrestFactorC",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.40.0", "unit": ""},
            {"field_key": "bypassVoltageA",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.41.0", "unit": "V"},
            {"field_key": "bypassVoltageB",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.42.0", "unit": "V"},
            {"field_key": "bypassVoltageC",       "oid": ".1.3.6.1.4.1.13400.2.20.2.4.43.0", "unit": "V"},
            {"field_key": "bypassFrequency",      "oid": ".1.3.6.1.4.1.13400.2.20.2.4.44.0", "unit": "Hz"},
            {"field_key": "batteryTemperature",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.46.0", "unit": "C"},
            {"field_key": "batteryDischargeTimes","oid": ".1.3.6.1.4.1.13400.2.20.2.4.48.0", "unit": "count"},
            {"field_key": "batteryCapacity",      "oid": ".1.3.6.1.4.1.13400.2.20.2.4.49.0", "unit": "%"},
            {"field_key": "batteryRemainsTime",   "oid": ".1.3.6.1.4.1.13400.2.20.2.4.50.0", "unit": "min"},
            {"field_key": "positiveBatteryVoltage","oid": ".1.3.6.1.4.1.13400.2.20.2.4.14.0", "unit": "V"},
            {"field_key": "negativeBatteryVoltage","oid": ".1.3.6.1.4.1.13400.2.20.2.4.15.0", "unit": "V"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Device IP Address", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["host"]
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

-- ── MQTT Production Devices ─────────────────────────────────

-- CCS 3-Phase Power Analyzer Panel
-- Payload: JSON array published to 'ccs_data' topic.
-- One sensor per panel (PM1/PM4/PM5 type). Use $[N].data.* with panel index N (0=PM1,1=PM4,2=PM5).
-- Advanced mode: adjust json_path indices for each panel when adding sensors.
(
    'mqtt',
    'CCS 3-Phase Power Analyzer Panel',
    'CCS',
    'ICF-3P',
    'CCS ICF power room 3-phase analyzer (PM1/PM4/PM5 type) — V L-L per phase, current, active/apparent power, energy. JSON array payload on ccs_data topic.',
    TRUE,
    '{
        "json_paths": [
            {"field_key": "voltage_ll1",   "json_path": "$[0].data.V_LL1",   "unit": "V"},
            {"field_key": "voltage_ll2",   "json_path": "$[0].data.V_LL2",   "unit": "V"},
            {"field_key": "voltage_ll3",   "json_path": "$[0].data.V_LL3",   "unit": "V"},
            {"field_key": "voltage_avg",   "json_path": "$[0].data.V_AVG",   "unit": "V"},
            {"field_key": "current_l1",    "json_path": "$[0].data.C_IL1",   "unit": "A"},
            {"field_key": "current_l2",    "json_path": "$[0].data.C_IL2",   "unit": "A"},
            {"field_key": "current_l3",    "json_path": "$[0].data.C_IL3",   "unit": "A"},
            {"field_key": "current_avg",   "json_path": "$[0].data.C_AVG",   "unit": "A"},
            {"field_key": "active_power",  "json_path": "$[0].data.ACT_POW", "unit": "kW"},
            {"field_key": "apparent_power","json_path": "$[0].data.APP_POW", "unit": "kVA"},
            {"field_key": "energy",        "json_path": "$[0].data.ENERGY",  "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "topic":       {"type": "string",  "title": "MQTT Topic",   "default": "ccs_data"},
            "qos":         {"type": "integer", "title": "QoS Level",    "enum": [0, 1, 2], "default": 0},
            "panel_index": {"type": "integer", "title": "Panel Array Index (0=PM1, 1=PM4, 2=PM5)", "default": 0, "minimum": 0, "maximum": 4},
            "panel_id":    {"type": "string",  "title": "Panel Analyser ID", "description": "e.g. CCS_ICF_PowerRoom_A_Panel"}
        },
        "required": ["topic"]
    }'
),

-- CCS HT Power Summary Panel
-- For PM2 (HT_Indoor) and PM3 (HT_Outdoor) type panels — average only, no per-phase breakdown.
(
    'mqtt',
    'CCS HT Power Summary Panel',
    'CCS',
    'ICF-HT',
    'CCS ICF HT power room summary analyzer (PM2/PM3 type) — average voltage, current, active/apparent power, energy. JSON array payload on ccs_data topic.',
    TRUE,
    '{
        "json_paths": [
            {"field_key": "voltage_avg",   "json_path": "$[3].data.V_AVG",   "unit": "V"},
            {"field_key": "current_avg",   "json_path": "$[3].data.C_AVG",   "unit": "A"},
            {"field_key": "active_power",  "json_path": "$[3].data.ACT_POW", "unit": "kW"},
            {"field_key": "apparent_power","json_path": "$[3].data.APP_POW", "unit": "kVA"},
            {"field_key": "energy",        "json_path": "$[3].data.ENERGY",  "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "topic":       {"type": "string",  "title": "MQTT Topic",   "default": "ccs_data"},
            "qos":         {"type": "integer", "title": "QoS Level",    "enum": [0, 1, 2], "default": 0},
            "panel_index": {"type": "integer", "title": "Panel Array Index (3=PM2/HT_Indoor, 4=PM3/HT_Outdoor)", "default": 3, "minimum": 0, "maximum": 4},
            "panel_id":    {"type": "string",  "title": "Panel Analyser ID", "description": "e.g. CCS_ICF_PowerRoom_HT_Indoor"}
        },
        "required": ["topic"]
    }'
),

-- ── Modbus Production Devices ─────────────────────────────────

-- Factory production line breakdown counter (from Qube-1302 registers.csv)
(
    'modbus_tcp',
    'Production Line Breakdown Counter',
    '',
    '',
    'Factory production line major/minor breakdown event counters — 3 lines (Conebakery, Flexline, Versaline). Holding registers, uint16.',
    TRUE,
    '{
        "registers": [
            {"field_key": "conebakery_major_breakdown", "register_type": "Holding", "address": 272, "data_type": "uint16", "scale": 1.0, "unit": "count"},
            {"field_key": "conebakery_minor_breakdown", "register_type": "Holding", "address": 271, "data_type": "uint16", "scale": 1.0, "unit": "count"},
            {"field_key": "flexline_major_breakdown",   "register_type": "Holding", "address": 72,  "data_type": "uint16", "scale": 1.0, "unit": "count"},
            {"field_key": "flexline_minor_breakdown",   "register_type": "Holding", "address": 71,  "data_type": "uint16", "scale": 1.0, "unit": "count"},
            {"field_key": "versaline_major_breakdown",  "register_type": "Holding", "address": 182, "data_type": "uint16", "scale": 1.0, "unit": "count"},
            {"field_key": "versaline_minor_breakdown",  "register_type": "Holding", "address": 181, "data_type": "uint16", "scale": 1.0, "unit": "count"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id":          {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset":  {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
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
