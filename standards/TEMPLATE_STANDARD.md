# Template System Standard

> Templates define how to connect to devices and what data to collect.
> Split into two types: Reader Templates (container/protocol) and Device Templates (sensor/measurement).

---

## Two Template Types

### Reader Template — "How to run the container"

- One per protocol (usually)
- Defines: Docker image, env vars, connection parameters schema
- Used when creating a reader (container)
- Created by developers when adding new protocol support

### Device Template — "What data to collect"

- Many per protocol (one per device model)
- Defines: registers, OIDs, topics, nodes, field mappings
- Used when adding a sensor to a reader
- Created by developers (global) or users (org-specific)

## Reader Template Schema

```json
{
    "id": "uuid",
    "protocol": "modbus_tcp",
    "name": "Modbus TCP Reader",
    "description": "Reads Modbus TCP registers from devices",

    "image_suffix": "modbus-reader",

    "connection_schema": {
        "type": "object",
        "properties": {
            "host": {
                "type": "string",
                "title": "Device IP Address",
                "format": "ipv4"
            },
            "port": {
                "type": "integer",
                "title": "Port",
                "default": 502,
                "minimum": 1,
                "maximum": 65535
            },
            "poll_interval_sec": {
                "type": "integer",
                "title": "Poll Interval (seconds)",
                "default": 20,
                "minimum": 1,
                "maximum": 3600
            },
            "timeout_ms": {
                "type": "integer",
                "title": "Timeout (ms)",
                "default": 3000
            }
        },
        "required": ["host", "port"]
    },

    "env_defaults": {
        "LOG_LEVEL": "info"
    },

    "reader_standard": "endpoint"
}
```

The `connection_schema` uses **JSON Schema** format. The UI renders a dynamic form from this schema.

## Device Template Schema

```json
{
    "id": "uuid",
    "protocol": "modbus_tcp",
    "name": "Schneider PM5100",
    "manufacturer": "Schneider Electric",
    "model": "PM5100",
    "description": "3-phase power meter",
    "is_global": true,

    "sensor_config": {
        "registers": [
            {
                "field_key": "active_power_w",
                "register_type": "Holding",
                "address": 3000,
                "data_type": "float32",
                "scale": 1.0,
                "unit": "W"
            },
            {
                "field_key": "voltage_ll_v",
                "register_type": "Holding",
                "address": 3020,
                "data_type": "float32",
                "scale": 1.0,
                "unit": "V"
            }
        ]
    },

    "sensor_params_schema": {
        "type": "object",
        "properties": {
            "unit_id": {
                "type": "integer",
                "title": "Modbus Unit ID",
                "default": 1,
                "minimum": 1,
                "maximum": 247
            },
            "register_offset": {
                "type": "integer",
                "title": "Register Address Offset",
                "default": 0
            }
        },
        "required": ["unit_id"]
    }
}
```

## Protocol-Specific sensor_config Formats

### Modbus TCP

```json
{
    "registers": [
        {
            "field_key": "active_power_w",
            "register_type": "Holding",
            "address": 3000,
            "data_type": "float32",
            "scale": 1.0,
            "unit": "W"
        }
    ]
}
```

### SNMP

```json
{
    "oids": [
        {
            "field_key": "battery_voltage",
            "oid": ".1.3.6.1.4.1.318.1.1.1.2.2.8.0",
            "unit": "V"
        }
    ]
}
```

### MQTT

```json
{
    "json_paths": [
        {
            "field_key": "temperature",
            "json_path": "$.data.temperature",
            "unit": "°C"
        }
    ]
}
```

### OPC-UA

```json
{
    "nodes": [
        {
            "field_key": "temperature",
            "node_id": "ns=2;i=1001",
            "type": "float",
            "unit": "°C"
        }
    ]
}
```

### HTTP

```json
{
    "json_paths": [
        {
            "field_key": "value",
            "json_path": "$.readings.value",
            "unit": ""
        }
    ]
}
```

## Protocol-Specific sensor_params_schema

### Modbus TCP

```json
{
    "properties": {
        "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1},
        "register_offset": {"type": "integer", "title": "Register Offset", "default": 0}
    },
    "required": ["unit_id"]
}
```

### SNMP

```json
{
    "properties": {
        "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
        "community": {"type": "string", "title": "Community String", "default": "public"},
        "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
    },
    "required": ["ip_address"]
}
```

### MQTT

```json
{
    "properties": {
        "topic": {"type": "string", "title": "MQTT Topic"},
        "qos": {"type": "integer", "title": "QoS Level", "enum": [0, 1, 2], "default": 1},
        "payload_format": {"type": "string", "title": "Payload Format", "enum": ["json", "senml"], "default": "json"}
    },
    "required": ["topic"]
}
```

### OPC-UA

```json
{
    "properties": {
        "namespace_index": {"type": "integer", "title": "Namespace Index", "default": 2}
    }
}
```

### HTTP

```json
{
    "properties": {
        "url": {"type": "string", "title": "Endpoint URL", "format": "uri"},
        "method": {"type": "string", "title": "HTTP Method", "enum": ["GET", "POST"], "default": "GET"},
        "auth_type": {"type": "string", "title": "Auth Type", "enum": ["none", "basic", "bearer", "api_key"], "default": "none"},
        "auth_credentials": {"type": "string", "title": "Credentials", "format": "password"},
        "headers_json": {"type": "string", "title": "Custom Headers (JSON)"}
    },
    "required": ["url"]
}
```

## How Sensor Config Gets Built

When a user adds a sensor:

```
1. User picks a Device Template (e.g., "Schneider PM5100")
2. UI shows sensor_params_schema form (e.g., unit_id, register_offset)
3. User fills in params
4. Cloud merges: device_template.sensor_config + user params
   → Result stored in sensors.config_json
5. Cloud generates sensor_map entries (Equipment.Reading → sensor UUID)
6. Config pushed to Qube SQLite via WebSocket
```

## Adding a New Protocol

1. Add row to `protocols` table
2. Create a `reader_templates` entry (connection_schema, image_suffix)
3. Create device_templates for known devices
4. Build the reader Go binary (reads SQLite, sends to core-switch)
5. Build Docker image, push to registry
6. Done — users can now add readers and sensors for this protocol

## Adding a New Device (Existing Protocol)

1. Create a `device_templates` entry with sensor_config and sensor_params_schema
2. Done — users can now select this template when adding sensors
