-- 004_new_protocols.sql — Add BACnet, LoRaWAN, and DNP3 support
-- Registers new protocols and their default reader templates.

-- ===================== NEW PROTOCOLS =====================

INSERT INTO protocols (id, label, description, reader_standard) VALUES
    ('bacnet',  'BACnet/IP', 'Building Automation and Control networks — HVAC, lighting, elevators', 'multi_target'),
    ('lorawan', 'LoRaWAN',   'Long Range Wide Area Network — low-power sensors via Network Server (Chirpstack/TTN)', 'endpoint'),
    ('dnp3',    'DNP3',      'Distributed Network Protocol — utilities, substations, water/gas SCADA', 'endpoint');

-- ===================== NEW READER TEMPLATES =====================

-- BACnet/IP Reader
INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults) VALUES
(
    'bacnet',
    'BACnet/IP Reader',
    'Polls BACnet objects via UDP/IP',
    'bacnet-reader',
    '{
        "type": "object",
        "properties": {
            "local_port": {"type": "integer", "title": "Local UDP Port", "default": 47808, "minimum": 1, "maximum": 65535},
            "poll_interval_sec": {"type": "integer", "title": "Poll Interval (seconds)", "default": 30, "minimum": 5, "maximum": 3600},
            "broadcast_addr": {"type": "string", "title": "Broadcast Address (for Discovery)", "default": "255.255.255.255"}
        },
        "required": ["local_port"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- LoRaWAN Reader
(
    'lorawan',
    'LoRaWAN NS Reader',
    'Subscribes to uplinks from a LoRaWAN Network Server (MQTT interface)',
    'lorawan-reader',
    '{
        "type": "object",
        "properties": {
            "ns_host": {"type": "string", "title": "Network Server Host"},
            "ns_port": {"type": "integer", "title": "Port", "default": 1700},
            "app_id":  {"type": "string", "title": "Application ID"},
            "api_key": {"type": "string", "title": "API Key", "format": "password"}
        },
        "required": ["ns_host", "app_id"]
    }',
    '{"LOG_LEVEL": "info"}'
),

-- DNP3 Reader
(
    'dnp3',
    'DNP3 Master Reader',
    'Connects to DNP3 Outstations (RTUs/PLCs)',
    'dnp3-reader',
    '{
        "type": "object",
        "properties": {
            "host": {"type": "string", "title": "Outstation IP", "format": "ipv4"},
            "port": {"type": "integer", "title": "Port", "default": 20000},
            "outstation_address": {"type": "integer", "title": "Outstation Address", "default": 10},
            "master_address": {"type": "integer", "title": "Master Address", "default": 1}
        },
        "required": ["host", "outstation_address"]
    }',
    '{"LOG_LEVEL": "info"}'
);

-- ===================== SAMPLE DEVICE TEMPLATES =====================

INSERT INTO device_templates (protocol, name, manufacturer, model, description, is_global, sensor_config, sensor_params_schema) VALUES
(
    'bacnet',
    'Generic BACnet HVAC',
    'Generic',
    'HVAC-01',
    'Standard BACnet HVAC controller — temperature and setpoint',
    TRUE,
    '{
        "objects": [
            {"field_key": "room_temp",     "object_type": "analogInput",  "object_instance": 1, "unit": "C"},
            {"field_key": "temp_setpoint", "object_type": "analogValue",  "object_instance": 1, "unit": "C"},
            {"field_key": "fan_status",    "object_type": "binaryValue",  "object_instance": 1, "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "device_instance": {"type": "integer", "title": "BACnet Device Instance"}
        },
        "required": ["device_instance"]
    }'
),
(
    'lorawan',
    'Dragino LHT65',
    'Dragino',
    'LHT65',
    'LoRaWAN Temperature & Humidity Sensor',
    TRUE,
    '{
        "readings": [
            {"field_key": "temperature_c", "field": "TempC_SHT", "unit": "C"},
            {"field_key": "humidity_pct",  "field": "Hum_SHT",   "unit": "%"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "dev_eui": {"type": "string", "title": "Device EUI (16 hex chars)"}
        },
        "required": ["dev_eui"]
    }'
);
