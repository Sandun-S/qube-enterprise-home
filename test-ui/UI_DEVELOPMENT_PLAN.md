# Qube Enterprise v2 — Full UI Development Plan

> **Scope:** Complete rewrite/upgrade of `test-ui/index.html` into a production-ready
> enterprise management console covering every Cloud API endpoint, every TP-API endpoint
> (via proxy), all five existing protocols, three new protocols, new global device
> templates, standards updates, and full admin + operator + viewer workflows.

---

## Table of Contents

1. [New Protocols to Add](#1-new-protocols-to-add)
2. [New Global Device Templates](#2-new-global-device-templates)
3. [Standards Updates Required](#3-standards-updates-required)
4. [UI Architecture Overview](#4-ui-architecture-overview)
5. [Navigation Structure](#5-navigation-structure)
6. [Page-by-Page Feature Plan](#6-page-by-page-feature-plan)
   - 6.1 Login / Register
   - 6.2 Dashboard (Home)
   - 6.3 Fleet — Qube Management
   - 6.4 Device Onboarding Wizard
   - 6.5 Readers Management
   - 6.6 Sensors Management
   - 6.7 Telemetry & Data Explorer
   - 6.8 Commands & Remote Actions
   - 6.9 Templates — Device Templates
   - 6.10 Templates — Reader Templates
   - 6.11 Protocols
   - 6.12 Users & Team Management
   - 6.13 Registry Settings (Superadmin)
   - 6.14 My Profile
7. [API-to-UI Full Mapping](#7-api-to-ui-full-mapping)
8. [Sensor Add Flow (Template-Driven)](#8-sensor-add-flow-template-driven)
9. [Protocol-Specific UI Forms](#9-protocol-specific-ui-forms)
10. [Role-Based Access Control in UI](#10-role-based-access-control-in-ui)
11. [Real-Time WebSocket Integration](#11-real-time-websocket-integration)
12. [Implementation Phases](#12-implementation-phases)
13. [File Structure for New UI](#13-file-structure-for-new-ui)

---

## 1. New Protocols to Add

These protocols should be added to the database via migrations and the UI should support them
automatically (connection_schema drives the form). Each entry follows the exact format from
`ADDING-PROTOCOLS.md`.

### 1.1 BACnet/IP

**Use case:** Building automation — HVAC, chillers, VAV boxes, lighting controllers.

**Step 1 — Register protocol (SQL):**
```sql
INSERT INTO protocols (id, label, description, reader_standard) VALUES
(
    'bacnet',
    'BACnet/IP',
    'BACnet over IP — building automation systems, HVAC, chillers, lighting controllers',
    'multi_target'   -- one container polls all BACnet devices on the Qube subnet
);
```

**Step 2 — Reader template (SQL):**
```sql
INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults) VALUES
(
    'bacnet',
    'BACnet/IP Reader',
    'Polls BACnet/IP devices — one container handles all BACnet targets on the local subnet',
    'bacnet-reader',
    '{
        "type": "object",
        "properties": {
            "local_port": {
                "type": "integer",
                "title": "Local UDP Port",
                "default": 47808,
                "minimum": 1024,
                "maximum": 65535
            },
            "poll_interval_sec": {
                "type": "integer",
                "title": "Poll Interval (seconds)",
                "default": 30,
                "minimum": 5,
                "maximum": 3600
            },
            "timeout_ms": {
                "type": "integer",
                "title": "Request Timeout (ms)",
                "default": 3000
            },
            "broadcast_addr": {
                "type": "string",
                "title": "Broadcast Address",
                "description": "e.g. 192.168.1.255 — used for device discovery",
                "format": "ipv4"
            }
        }
    }',
    '{"LOG_LEVEL": "info"}'
);
```

**reader_standard:** `multi_target` — each sensor has its own `device_instance` and `object_type`.

**sensor_params_schema (used in device templates):**
```json
{
    "type": "object",
    "properties": {
        "ip_address":       {"type": "string",  "title": "Device IP",         "format": "ipv4"},
        "device_instance":  {"type": "integer", "title": "BACnet Device Instance"},
        "object_type":      {"type": "string",  "title": "Object Type",       "enum": ["analogInput","analogOutput","analogValue","binaryInput","binaryOutput","multiStateInput"]},
        "object_instance":  {"type": "integer", "title": "Object Instance"},
        "property_id":      {"type": "string",  "title": "Property",          "default": "presentValue"}
    },
    "required": ["ip_address", "device_instance", "object_instance"]
}
```

**`protocolArrayKey` code change** in `cloud/internal/api/templates.go`:
```go
case "bacnet": return "objects"
```

---

### 1.2 LoRaWAN

**Use case:** Long-range low-power sensors — temperature, humidity, soil moisture, leak detection.

**Step 1 — Register protocol (SQL):**
```sql
INSERT INTO protocols (id, label, description, reader_standard) VALUES
(
    'lorawan',
    'LoRaWAN',
    'Long-range, low-power IoT devices via LoRaWAN network server (Chirpstack, TTN)',
    'endpoint'   -- one container per network server application
);
```

**Step 2 — Reader template (SQL):**
```sql
INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults) VALUES
(
    'lorawan',
    'LoRaWAN NS Reader',
    'Connects to a LoRaWAN Network Server (Chirpstack, TTN) and subscribes to device uplinks',
    'lorawan-reader',
    '{
        "type": "object",
        "properties": {
            "ns_host": {
                "type": "string",
                "title": "Network Server Host",
                "description": "Hostname or IP of Chirpstack/TTN server"
            },
            "ns_port": {
                "type": "integer",
                "title": "Port",
                "default": 1700,
                "minimum": 1,
                "maximum": 65535
            },
            "app_id": {
                "type": "string",
                "title": "Application ID"
            },
            "api_key": {
                "type": "string",
                "title": "API Key",
                "format": "password"
            },
            "mqtt_broker": {
                "type": "string",
                "title": "MQTT Broker (optional)",
                "description": "For Chirpstack MQTT integration, e.g. tcp://localhost:1883"
            }
        },
        "required": ["ns_host", "app_id"]
    }',
    '{"LOG_LEVEL": "info"}'
);
```

**`protocolArrayKey` code change:**
```go
case "lorawan": return "readings"
```

---

### 1.3 DNP3

**Use case:** Utility SCADA — power substations, RTUs, water treatment.

**Step 1 — Register protocol (SQL):**
```sql
INSERT INTO protocols (id, label, description, reader_standard) VALUES
(
    'dnp3',
    'DNP3',
    'Distributed Network Protocol 3 — utility SCADA, substations, RTUs, water treatment',
    'endpoint'   -- one container per DNP3 outstation
);
```

**Step 2 — Reader template (SQL):**
```sql
INSERT INTO reader_templates (protocol, name, description, image_suffix, connection_schema, env_defaults) VALUES
(
    'dnp3',
    'DNP3 Reader',
    'Polls DNP3 outstations over TCP — one container per outstation',
    'dnp3-reader',
    '{
        "type": "object",
        "properties": {
            "host": {
                "type": "string",
                "title": "Outstation IP",
                "format": "ipv4"
            },
            "port": {
                "type": "integer",
                "title": "Port",
                "default": 20000,
                "minimum": 1,
                "maximum": 65535
            },
            "master_address": {
                "type": "integer",
                "title": "Master DNP3 Address",
                "default": 1,
                "minimum": 0,
                "maximum": 65519
            },
            "outstation_address": {
                "type": "integer",
                "title": "Outstation DNP3 Address",
                "default": 10,
                "minimum": 0,
                "maximum": 65519
            },
            "poll_interval_sec": {
                "type": "integer",
                "title": "Poll Interval (seconds)",
                "default": 10,
                "minimum": 1,
                "maximum": 3600
            }
        },
        "required": ["host", "outstation_address"]
    }',
    '{"LOG_LEVEL": "info"}'
);
```

**`protocolArrayKey` code change:**
```go
case "dnp3": return "points"
```

---

### Summary of `protocolArrayKey` Changes

After adding the three new protocols, update `cloud/internal/api/templates.go`:

```go
func protocolArrayKey(protocol string) string {
    switch protocol {
    case "modbus_tcp": return "registers"
    case "opcua":      return "nodes"
    case "snmp":       return "oids"
    case "mqtt":       return "json_paths"
    case "http":       return "json_paths"
    case "lorawan":    return "readings"   // NEW
    case "bacnet":     return "objects"    // NEW
    case "dnp3":       return "points"     // NEW
    default:           return "entries"
    }
}
```

---

## 2. New Global Device Templates

Add these to `cloud/migrations/002_global_data.sql` (or a new `004_new_protocols.sql`
migration file) after the new protocols and reader templates are inserted.

### 2.1 BACnet Device Templates

```sql
INSERT INTO device_templates (protocol, name, manufacturer, model, description,
    is_global, sensor_config, sensor_params_schema) VALUES
(
    'bacnet',
    'Generic BACnet HVAC Controller',
    '',
    '',
    'Generic BACnet/IP HVAC controller — zone temp, setpoint, damper position, fan status',
    TRUE,
    '{
        "objects": [
            {"field_key": "zone_temp_c",       "object_type": "analogInput",  "object_instance": 1,  "unit": "C"},
            {"field_key": "setpoint_c",         "object_type": "analogOutput", "object_instance": 1,  "unit": "C"},
            {"field_key": "damper_position_pct","object_type": "analogOutput", "object_instance": 2,  "unit": "%"},
            {"field_key": "fan_status",         "object_type": "binaryInput",  "object_instance": 1,  "unit": ""},
            {"field_key": "cooling_active",     "object_type": "binaryInput",  "object_instance": 2,  "unit": ""},
            {"field_key": "heating_active",     "object_type": "binaryInput",  "object_instance": 3,  "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "ip_address":      {"type": "string",  "title": "Device IP",             "format": "ipv4"},
            "device_instance": {"type": "integer", "title": "BACnet Device Instance"},
            "property_id":     {"type": "string",  "title": "Property",              "default": "presentValue"}
        },
        "required": ["ip_address", "device_instance"]
    }'
),
(
    'bacnet',
    'Generic BACnet Chiller',
    '',
    '',
    'Generic BACnet chiller — supply/return temps, power, status',
    TRUE,
    '{
        "objects": [
            {"field_key": "chw_supply_temp_c", "object_type": "analogInput",  "object_instance": 1, "unit": "C"},
            {"field_key": "chw_return_temp_c", "object_type": "analogInput",  "object_instance": 2, "unit": "C"},
            {"field_key": "active_power_kw",   "object_type": "analogInput",  "object_instance": 3, "unit": "kW"},
            {"field_key": "chiller_status",    "object_type": "binaryInput",  "object_instance": 1, "unit": ""},
            {"field_key": "fault_active",      "object_type": "binaryInput",  "object_instance": 2, "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "ip_address":      {"type": "string",  "title": "Device IP",             "format": "ipv4"},
            "device_instance": {"type": "integer", "title": "BACnet Device Instance"},
            "property_id":     {"type": "string",  "title": "Property",              "default": "presentValue"}
        },
        "required": ["ip_address", "device_instance"]
    }'
);
```

### 2.2 LoRaWAN Device Templates

```sql
INSERT INTO device_templates (protocol, name, manufacturer, model, description,
    is_global, sensor_config, sensor_params_schema) VALUES
(
    'lorawan',
    'Dragino LHT65 Temp/Humidity',
    'Dragino',
    'LHT65',
    'Indoor/outdoor temperature and humidity sensor with external probe',
    TRUE,
    '{
        "readings": [
            {"field_key": "temperature_c",  "field": "TempC_SHT",  "unit": "C"},
            {"field_key": "humidity_pct",   "field": "Hum_SHT",    "unit": "%"},
            {"field_key": "ext_temp_c",     "field": "TempC_DS",   "unit": "C"},
            {"field_key": "battery_v",      "field": "BatV",       "unit": "V"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "dev_eui": {"type": "string", "title": "Device EUI (16 hex chars)"},
            "app_eui": {"type": "string", "title": "App EUI",  "description": "Optional — for TTN"}
        },
        "required": ["dev_eui"]
    }'
),
(
    'lorawan',
    'Dragino LDDS75 Distance',
    'Dragino',
    'LDDS75',
    'Ultrasonic distance/level sensor — tank levels, bin filling',
    TRUE,
    '{
        "readings": [
            {"field_key": "distance_mm",   "field": "Distance",  "unit": "mm"},
            {"field_key": "battery_v",     "field": "BatV",      "unit": "V"},
            {"field_key": "interrupt_flag","field": "INTERRUPT",  "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "dev_eui":   {"type": "string", "title": "Device EUI"},
            "tank_height_mm": {"type": "integer", "title": "Tank Height (mm)", "description": "Used to compute fill level"}
        },
        "required": ["dev_eui"]
    }'
),
(
    'lorawan',
    'Milesight EM310 Soil Sensor',
    'Milesight',
    'EM310-TILT',
    'Soil moisture, temperature and tilt sensor',
    TRUE,
    '{
        "readings": [
            {"field_key": "soil_moisture_pct", "field": "soil_moisture", "unit": "%"},
            {"field_key": "soil_temp_c",        "field": "soil_temp",     "unit": "C"},
            {"field_key": "tilt_deg",           "field": "tilt",          "unit": "deg"},
            {"field_key": "battery_pct",        "field": "battery",       "unit": "%"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "dev_eui": {"type": "string", "title": "Device EUI"}
        },
        "required": ["dev_eui"]
    }'
);
```

### 2.3 DNP3 Device Templates

```sql
INSERT INTO device_templates (protocol, name, manufacturer, model, description,
    is_global, sensor_config, sensor_params_schema) VALUES
(
    'dnp3',
    'Generic DNP3 RTU',
    '',
    '',
    'Generic DNP3 RTU — analog inputs, binary inputs, counters',
    TRUE,
    '{
        "points": [
            {"field_key": "analog_in_0",  "group": 30, "variation": 1, "index": 0, "unit": ""},
            {"field_key": "analog_in_1",  "group": 30, "variation": 1, "index": 1, "unit": ""},
            {"field_key": "binary_in_0",  "group": 1,  "variation": 2, "index": 0, "unit": ""},
            {"field_key": "binary_in_1",  "group": 1,  "variation": 2, "index": 1, "unit": ""},
            {"field_key": "counter_0",    "group": 20, "variation": 1, "index": 0, "unit": "count"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "outstation_address": {"type": "integer", "title": "Outstation Address", "default": 10}
        },
        "required": ["outstation_address"]
    }'
),
(
    'dnp3',
    'Power Substation Feeder',
    '',
    '',
    'DNP3 power substation feeder relay — voltage, current, power, trip status',
    TRUE,
    '{
        "points": [
            {"field_key": "voltage_v",       "group": 30, "variation": 5, "index": 0, "unit": "V"},
            {"field_key": "current_a",       "group": 30, "variation": 5, "index": 1, "unit": "A"},
            {"field_key": "active_power_kw", "group": 30, "variation": 5, "index": 2, "unit": "kW"},
            {"field_key": "trip_status",     "group": 1,  "variation": 2, "index": 0, "unit": ""},
            {"field_key": "alarm_active",    "group": 1,  "variation": 2, "index": 1, "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "outstation_address": {"type": "integer", "title": "Outstation Address", "default": 10}
        },
        "required": ["outstation_address"]
    }'
);
```

### 2.4 Additional Templates for Existing Protocols

#### Modbus TCP — Additional Industrial Devices

```sql
INSERT INTO device_templates (protocol, name, manufacturer, model, description,
    is_global, sensor_config, sensor_params_schema) VALUES
(
    'modbus_tcp',
    'Carlo Gavazzi EM24',
    'Carlo Gavazzi',
    'EM24',
    '3-phase energy analyzer — per-phase and total power, energy, PF',
    TRUE,
    '{
        "registers": [
            {"field_key": "voltage_l1_v",      "register_type": "Input", "address": 0,  "data_type": "int16",  "scale": 0.1,  "unit": "V"},
            {"field_key": "voltage_l2_v",      "register_type": "Input", "address": 2,  "data_type": "int16",  "scale": 0.1,  "unit": "V"},
            {"field_key": "voltage_l3_v",      "register_type": "Input", "address": 4,  "data_type": "int16",  "scale": 0.1,  "unit": "V"},
            {"field_key": "current_l1_a",      "register_type": "Input", "address": 12, "data_type": "int16",  "scale": 0.001,"unit": "A"},
            {"field_key": "current_l2_a",      "register_type": "Input", "address": 14, "data_type": "int16",  "scale": 0.001,"unit": "A"},
            {"field_key": "current_l3_a",      "register_type": "Input", "address": 16, "data_type": "int16",  "scale": 0.001,"unit": "A"},
            {"field_key": "active_power_w",    "register_type": "Input", "address": 40, "data_type": "int32",  "scale": 0.1,  "unit": "W"},
            {"field_key": "energy_kwh",        "register_type": "Input", "address": 78, "data_type": "int32",  "scale": 0.1,  "unit": "kWh"},
            {"field_key": "power_factor",      "register_type": "Input", "address": 62, "data_type": "int16",  "scale": 0.01, "unit": ""},
            {"field_key": "frequency_hz",      "register_type": "Input", "address": 70, "data_type": "int16",  "scale": 0.1,  "unit": "Hz"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id":         {"type": "integer", "title": "Modbus Unit ID",        "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset","default": 0}
        },
        "required": ["unit_id"]
    }'
),
(
    'modbus_tcp',
    'Generic Temperature Sensor (RTD/Thermocouple)',
    '',
    '',
    'Generic single-channel temperature input via Modbus TCP converter',
    TRUE,
    '{
        "registers": [
            {"field_key": "temperature_c", "register_type": "Holding", "address": 0, "data_type": "int16", "scale": 0.1, "unit": "C"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247}
        },
        "required": ["unit_id"]
    }'
),
(
    'modbus_tcp',
    'Sungrow SH Hybrid Inverter',
    'Sungrow',
    'SH-Series',
    'Sungrow hybrid solar inverter — PV power, battery, grid, load',
    TRUE,
    '{
        "registers": [
            {"field_key": "pv_power_w",         "register_type": "Input", "address": 5016, "data_type": "uint32", "scale": 1.0,  "unit": "W"},
            {"field_key": "battery_power_w",    "register_type": "Input", "address": 13021,"data_type": "int16",  "scale": 1.0,  "unit": "W"},
            {"field_key": "battery_soc_pct",    "register_type": "Input", "address": 13022,"data_type": "uint16", "scale": 0.1,  "unit": "%"},
            {"field_key": "grid_power_w",       "register_type": "Input", "address": 13009,"data_type": "int32",  "scale": 1.0,  "unit": "W"},
            {"field_key": "load_power_w",       "register_type": "Input", "address": 13007,"data_type": "uint32", "scale": 1.0,  "unit": "W"},
            {"field_key": "daily_pv_kwh",       "register_type": "Input", "address": 13001,"data_type": "uint32", "scale": 0.1,  "unit": "kWh"},
            {"field_key": "total_export_kwh",   "register_type": "Input", "address": 13043,"data_type": "uint32", "scale": 0.1,  "unit": "kWh"}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "unit_id": {"type": "integer", "title": "Modbus Unit ID (usually 1)", "default": 1},
            "register_offset": {"type": "integer", "title": "Register Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
);
```

#### SNMP — Additional Network / Environmental

```sql
INSERT INTO device_templates (protocol, name, manufacturer, model, description,
    is_global, sensor_config, sensor_params_schema) VALUES
(
    'snmp',
    'Generic Network Switch (RFC 2863)',
    '',
    '',
    'RFC 2863 MIB — interface octets, errors, admin/oper status',
    TRUE,
    '{
        "oids": [
            {"field_key": "if_in_octets",    "oid": ".1.3.6.1.2.1.2.2.1.10.1",  "unit": "bytes"},
            {"field_key": "if_out_octets",   "oid": ".1.3.6.1.2.1.2.2.1.16.1",  "unit": "bytes"},
            {"field_key": "if_in_errors",    "oid": ".1.3.6.1.2.1.2.2.1.14.1",  "unit": "count"},
            {"field_key": "if_out_errors",   "oid": ".1.3.6.1.2.1.2.2.1.20.1",  "unit": "count"},
            {"field_key": "if_oper_status",  "oid": ".1.3.6.1.2.1.2.2.1.8.1",   "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "host":      {"type": "string",  "title": "Device IP",        "format": "ipv4"},
            "community": {"type": "string",  "title": "Community String", "default": "public"},
            "version":   {"type": "string",  "title": "SNMP Version",     "enum": ["1","2c","3"], "default": "2c"}
        },
        "required": ["host"]
    }'
),
(
    'snmp',
    'AKCP SensorProbe Temperature',
    'AKCP',
    'SensorProbe',
    'AKCP environment sensor — temperature, humidity',
    TRUE,
    '{
        "oids": [
            {"field_key": "temperature_c",  "oid": ".1.3.6.1.4.1.3854.1.2.2.1.16.1.3",  "unit": "C"},
            {"field_key": "humidity_pct",   "oid": ".1.3.6.1.4.1.3854.1.2.2.1.17.1.3",  "unit": "%"},
            {"field_key": "sensor_status",  "oid": ".1.3.6.1.4.1.3854.1.2.2.1.16.1.4",  "unit": ""}
        ]
    }',
    '{
        "type": "object",
        "properties": {
            "host":      {"type": "string",  "title": "Device IP",        "format": "ipv4"},
            "community": {"type": "string",  "title": "Community String", "default": "public"},
            "version":   {"type": "string",  "title": "SNMP Version",     "enum": ["1","2c","3"], "default": "2c"}
        },
        "required": ["host"]
    }'
);
```

---

## 3. Standards Updates Required

The following files in `standards/` need updates to reflect the new protocols and UI guidance.

### 3.1 `standards/TEMPLATE_STANDARD.md` — Add New Protocol Formats

**Add after the HTTP section:**

```markdown
### BACnet/IP

sensor_config format:
```json
{
    "objects": [
        {
            "field_key": "zone_temp_c",
            "object_type": "analogInput",
            "object_instance": 1,
            "unit": "C"
        }
    ]
}
```

sensor_params_schema:
```json
{
    "properties": {
        "ip_address":      {"type": "string",  "title": "Device IP",             "format": "ipv4"},
        "device_instance": {"type": "integer", "title": "BACnet Device Instance"},
        "property_id":     {"type": "string",  "title": "Property",              "default": "presentValue"}
    },
    "required": ["ip_address", "device_instance"]
}
```

### LoRaWAN

sensor_config format:
```json
{
    "readings": [
        {"field_key": "temperature_c", "field": "TempC_SHT", "unit": "C"}
    ]
}
```

sensor_params_schema:
```json
{
    "properties": {
        "dev_eui": {"type": "string", "title": "Device EUI (16 hex chars)"},
        "app_eui": {"type": "string", "title": "App EUI (optional)"}
    },
    "required": ["dev_eui"]
}
```

### DNP3

sensor_config format:
```json
{
    "points": [
        {"field_key": "analog_in_0", "group": 30, "variation": 1, "index": 0, "unit": ""}
    ]
}
```

sensor_params_schema:
```json
{
    "properties": {
        "outstation_address": {"type": "integer", "title": "Outstation Address", "default": 10}
    },
    "required": ["outstation_address"]
}
```
```

### 3.2 `standards/READER_STANDARD.md` — Add Reader Standard Type Table

**Add to the reader standard section:**

```markdown
## Reader Standard Types

| Standard     | Description                                      | Examples               |
|-------------|--------------------------------------------------|------------------------|
| endpoint     | One container per connection endpoint            | Modbus TCP, OPC-UA, MQTT, LoRaWAN, DNP3 |
| multi_target | One container handles all targets of this type   | SNMP, HTTP, BACnet     |

For `multi_target` readers: each sensor's `config_json` contains the target address
(IP, URL, etc.). The single reader container iterates all sensors and polls each target.

For `endpoint` readers: the reader's `config_json` contains the connection (host/port/broker).
All sensors share the same endpoint. Connection parameters are set once at reader creation.
```

### 3.3 `standards/SQLITE_SCHEMA.md` — Verify sensor status column

The `sensors` table was recently updated to add a `status` column. Confirm this is documented
in the SQLite schema standard. If missing, add:

```markdown
## sensors table

| column       | type    | notes                                              |
|-------------|---------|----------------------------------------------------|
| id           | TEXT PK | UUID                                               |
| reader_id    | TEXT FK | readers.id                                        |
| name         | TEXT    | Human-readable sensor name                        |
| config_json  | TEXT    | Merged template config + user params              |
| tags_json    | TEXT    | Key=value pairs for InfluxDB tags                 |
| output       | TEXT    | "influxdb" | "live" | "influxdb,live"             |
| table_name   | TEXT    | InfluxDB measurement name (default "Measurements") |
| status       | TEXT    | "active" | "inactive" (default "active")          |
| created_at   | TEXT    | ISO timestamp                                      |
```

---

## 4. UI Architecture Overview

### Technology Choices

The new UI should be a **single HTML file** (to match the current approach and keep deployment
zero-friction: just serve the file from a static web server or open in a browser). It uses:

- **Vanilla JS + fetch API** — no build step required
- **CSS custom properties** — dark/light theme
- **WebSocket API** — live dashboard
- **Chart.js (CDN)** — telemetry charts
- **JSON Schema form renderer** — custom lightweight implementation to render
  `connection_schema` and `sensor_params_schema` forms dynamically

The file will be split into logical JS modules via `<script type="module">` inline sections
to maintain clarity without a bundler. Alternatively, split into multiple files under `test-ui/`:

```
test-ui/
├── index.html          (entry shell with nav + router)
├── app.js              (state, auth, router)
├── api.js              (all API calls, centralized)
├── components.js       (reusable UI: modal, table, form-from-schema)
├── pages/
│   ├── dashboard.js
│   ├── fleet.js
│   ├── onboarding.js
│   ├── readers.js
│   ├── sensors.js
│   ├── telemetry.js
│   ├── commands.js
│   ├── templates.js
│   ├── protocols.js
│   ├── users.js
│   ├── registry.js
│   └── profile.js
└── UI_DEVELOPMENT_PLAN.md  (this file)
```

### State Management

Global state object:
```js
const state = {
    token: null,          // JWT
    role: null,           // viewer | editor | admin | superadmin
    orgId: null,
    userId: null,
    baseUrl: 'http://localhost:8080',
    selectedQubeId: null,  // active Qube context
    ws: null,              // dashboard WebSocket
    protocols: [],         // cached from /api/v1/protocols
    readerTemplates: [],   // cached
};
```

---

## 5. Navigation Structure

```
[Qube Enterprise]
│
├── Dashboard          ← home/overview (all roles)
│
├── Fleet              ← Qube list + detail
│   ├── All Qubes
│   └── [Qube Detail]
│       ├── Overview
│       ├── Readers
│       ├── Sensors
│       ├── Containers
│       └── Commands
│
├── Add Device         ← new sensor wizard (editor+)
│
├── Sensors            ← org-wide sensor list (all roles)
│
├── Telemetry          ← data explorer + live dashboard (all roles)
│   ├── Live View
│   ├── History
│   ├── Sensor Summary
│   ├── MQTT Discovery  ← send mqtt_discover command, map results to template
│   └── SNMP Walk       ← send snmp_walk command, browse OIDs, build template
│
├── Templates          ← device + reader templates (viewer read, editor+ write)
│   ├── Device Templates
│   └── Reader Templates
│
├── Protocols          ← protocol list (viewer)
│
├── Users              ← user/team management (admin+)
│
├── Settings           ← org settings, registry (admin+/superadmin)
│   ├── Registry       ← (superadmin only)
│   └── Organisation
│
└── Profile            ← current user (all roles)
```

---

## 6. Page-by-Page Feature Plan

### 6.1 Login / Register Page

**Route:** `/login`
**Auth required:** None

**Layout:** Centered card, two tabs — "Sign In" and "Create Organisation"

**Sign In form:**
- Email input
- Password input
- "Remember me" (stores token in localStorage)
- Submit → `POST /api/v1/auth/login`
- On success: decode JWT, set `state.role`, redirect to Dashboard

**Create Organisation form:**
- Org Name
- Email
- Password
- Submit → `POST /api/v1/auth/register`
- On success: auto-login with returned token

**Error handling:** Show inline error messages (invalid credentials, email taken, etc.)

---

### 6.2 Dashboard (Home)

**Route:** `/dashboard`
**API calls:**
- `GET /api/v1/qubes` — fleet health summary
- `GET /api/v1/users/me` — current user info
- WebSocket `/ws/dashboard` — live sensor values

**Layout:** Grid of widgets

**Widgets:**

| Widget | Data Source | Description |
|--------|-------------|-------------|
| Fleet Health | `GET /qubes` | Doughnut chart: Online / Offline / Unclaimed |
| Total Qubes | `GET /qubes` | Count badge |
| Total Sensors | `GET /qubes/{id}/sensors` (all) | Sum across org |
| Qubes Online | `GET /qubes` | ws_connected count |
| Recently Seen | `GET /qubes` | Last 5 qubes by last_seen |
| Config Sync Status | `GET /qubes` | Count of qubes with config_version mismatch |
| Live Readings | WebSocket `/ws/dashboard` | Scrolling live feed of incoming sensor values |
| Quick Actions | — | Buttons: "Claim Qube", "Add Sensor", "View Telemetry" |

**Live feed (WebSocket):**
- Connect to `ws://<base>/ws/dashboard` with `Authorization: Bearer <token>`
- Receive `{type: "sensor_reading", sensor_id, field_key, value, unit, time}` messages
- Display scrolling table with timestamp, sensor name, field, value

---

### 6.3 Fleet — Qube Management

**Route:** `/fleet`
**API calls:** `GET /api/v1/qubes`

**Fleet List View:**

| Column | Source |
|--------|--------|
| ID | qube.id |
| Location | qube.location_label |
| Status | qube.ws_connected → "Online" / "Offline" |
| Last Seen | qube.last_seen (relative time: "2 min ago") |
| Config Version | qube.config_version |
| WS Connected | qube.ws_connected badge |
| Actions | View / Edit / Commands |

**Actions:**
- "Claim New Qube" button (Admin+) → inline modal: enter `register_key` → `POST /api/v1/qubes/claim`
- Click row → Qube Detail page

**Qube Detail Page — Route:** `/fleet/:qubeId`

**API calls:**
- `GET /api/v1/qubes/{id}`
- `GET /api/v1/qubes/{id}/readers`
- `GET /api/v1/qubes/{id}/sensors`
- `GET /api/v1/qubes/{id}/containers`

**Tabs:**

#### Overview Tab
- Qube ID, status badge (Online/Offline/Unclaimed)
- Location label (edit inline for Editor+)
- Poll interval setting (edit for Editor+)
- Config hash + version
- Last seen timestamp
- WS connected indicator
- `PUT /api/v1/qubes/{id}` on save

#### Readers Tab
Displays all readers on this Qube.

| Column | Data |
|--------|------|
| Name | reader.name |
| Protocol | reader.protocol (badge with color) |
| Status | reader.status |
| Container | reader.container_id (link) |
| Config | summarized connection params |
| Actions | View / Edit / Delete |

- "Add Reader" button (Editor+) → opens Reader Create modal
  - Protocol dropdown (from `GET /api/v1/protocols`)
  - Reader template select (filtered by protocol, from `GET /api/v1/reader-templates`)
  - Dynamic form rendered from `connection_schema` (JSON Schema form renderer)
  - Name field
  - Submit → `POST /api/v1/qubes/{id}/readers`
- Click reader → Reader Detail (sensors list + edit config)
- Delete → `DELETE /api/v1/readers/{id}` (with confirmation: "deletes all sensors and container")

#### Sensors Tab
All sensors across all readers on this Qube.

| Column | Data |
|--------|------|
| Name | sensor.name |
| Protocol | via reader |
| Reader | reader.name (link) |
| Output | sensor.output badge |
| Status | sensor.status badge |
| Latest Value | `GET /api/v1/data/sensors/{id}/latest` |
| Actions | Edit / Delete / View Data |

- "Add Sensor" button (Editor+) → opens Add Device Wizard (Section 6.4)
- Edit → inline modal: tags, output, table_name, status → `PUT /api/v1/sensors/{id}`
- Delete → `DELETE /api/v1/sensors/{id}`
- "View Data" → navigates to Telemetry page filtered to this sensor

#### Containers Tab
Read-only list of Docker containers/services on this Qube.

| Column | Data |
|--------|------|
| Name | container.name |
| Image | container.image |
| Status | container.status |
| Type | reader / infra |

From: `GET /api/v1/qubes/{id}/containers`

#### Commands Tab
Command history + send new commands.

- Recent commands list: id, type, status, sent_at, ack_at, result, delivered_via
- "Send Command" form:
  - Command type dropdown (grouped):
    - **Container/Config:** `ping`, `restart_qube`, `shutdown`, `restart_reader`, `stop_container`, `reload_config`, `get_logs`, `list_containers`, `update_sqlite`
    - **Network:** `reset_ips`, `set_eth`, `set_wifi`, `set_firewall`
    - **Identity/System:** `get_info`, `set_name`, `set_timezone`
    - **Backup/Restore:** `backup_data`, `restore_data`
    - **Maintenance:** `repair_fs`, `backup_image`, `restore_image`
    - **Services:** `service_add`, `service_rm`, `service_edit`
    - **Files:** `put_file`, `get_file`
  - Dynamic payload form rendered per command type (see DEVELOPER_GUIDE.md §11)
  - Submit → `POST /api/v1/qubes/{id}/commands`
- Display `delivery` (websocket / queued) badge and result JSON when ack received
- Auto-refresh command list every 5s while any command is in `pending`/`sent` state

---

### 6.4 Device Onboarding Wizard

**Route:** `/onboard` or modal from Fleet → Sensors Tab

This is the primary user workflow for adding a new sensor to a Qube.

**Step 1 — Select Qube**
- Dropdown of claimed Qubes (`GET /api/v1/qubes`)
- If navigated from Fleet, pre-selected

**Step 2 — Select Protocol**
- Cards for each protocol from `GET /api/v1/protocols`
- Show: icon, name, description, reader_standard badge
- User clicks one → proceeds

**Step 3 — Select Device Template**
- `GET /api/v1/device-templates?protocol=<selected>`
- Grid of template cards: name, manufacturer, model, description, field count
- Filter/search by name or manufacturer
- "Custom (no template)" option for advanced users
- User selects one → proceeds

**Step 4 — Configure Connection**

This step depends on `reader_standard` from the protocol record:

*For `endpoint` protocols (Modbus TCP, OPC-UA, MQTT, LoRaWAN, DNP3):*
- Fetch existing readers: `GET /api/v1/qubes/{id}/readers?protocol=<protocol>`
- Show existing reader cards (click to reuse) with name and connection summary
- Show a "New connection" option with a form rendered from `connection_schema`
- Reader name field (only shown when creating a new connection)
- If user selects an existing reader card: no connection form shown, skip to Step 5

*For `multi_target` protocols (SNMP, HTTP, BACnet):*
- **No connection form shown.** One shared container handles all devices.
- Show status indicator:
  - "Shared SNMP container exists — ready to add sensors" (green badge)
  - "Shared SNMP container will be created automatically" (info badge, if none exists yet)
- Skip directly to Step 5 (sensor params)
- The connection is handled automatically by the server.

**Step 5 — Configure Sensor Parameters**
- Sensor name field
- Dynamic form rendered from selected `device_template.sensor_params_schema`
  - Modbus: Unit ID, register offset
  - SNMP: `host` (device IP), `port` (161), `community`, `version`
  - MQTT: `topic` (subscribe topic, supports wildcards)
  - OPC-UA: `namespace_index`
  - HTTP: `url`, `method`, `auth_type`, credentials
  - LoRaWAN: `dev_eui`, `app_eui`
  - BACnet: `host` (device IP), `device_instance`, `property_id`
  - DNP3: `outstation_address`
- Output mode: `influxdb` / `live` / `influxdb,live` (dropdown)
- Tags (key=value pairs, add/remove rows)
- Table name (default "Measurements")

**Step 6 — Review & Confirm**
- Summary card:
  - Qube: Q-1001 (Server Room)
  - Protocol: Modbus TCP
  - Device: Schneider PM5100
  - Connection: 192.168.1.100:502 (reusing "Rack Panel") — OR — New reader at 192.168.1.10:502
  - Sensor Name: "Panel-A Main Breaker"
  - Params: Unit ID: 1
  - Output: influxdb
  - Fields: active_power_w, voltage_ll_v, current_a, energy_kwh (from template)
- "Back" and "Add Sensor" buttons

**On submit — always use the smart endpoint:**
```http
POST /api/v1/qubes/{id}/sensors
```
```json
{
  "name": "Panel-A Main Breaker",
  "template_id": "<device_template_uuid>",
  "params": {"unit_id": 1},
  "reader_config": {"host": "192.168.1.10", "port": 502, "poll_interval_sec": 20},
  "reader_name": "Rack Panel A",
  "output": "influxdb",
  "tags_json": {"location": "Server Room"}
}
```
- For `multi_target` protocols: omit `reader_config` entirely — server handles it.
- For `endpoint` protocols where user selected an existing reader: include `reader_config`
  matching that reader's connection (fingerprint match = reuse, no new container created).
- Show success: "Sensor added. Config sync in progress..."
- Poll `GET /api/v1/qubes/{id}` every 5s and show sync status (see `new_hash` in response).

---

### 6.5 Readers Management

**Route:** `/fleet/:qubeId/readers` or linked from Qube Detail

This is a standalone full-page reader manager.

**For a selected Qube:**
- Reader list table (as described in 6.3 Readers Tab)
- Click reader row → expand inline to show:
  - Full config JSON (view)
  - "Edit Config" button → rendered form from `connection_schema` or raw JSON editor
  - `PUT /api/v1/readers/{id}`
  - Sensors count on this reader
  - "View Sensors" link

---

### 6.6 Sensors Management (Org-Wide)

**Route:** `/sensors`
**API:** For each Qube: `GET /api/v1/qubes/{id}/sensors`

**Layout:** Full table, org-wide sensor inventory.

| Column | Data |
|--------|------|
| Name | sensor.name |
| Qube | reader.qube_id → qube name |
| Protocol | reader.protocol |
| Reader | reader.name |
| Fields | count from config_json |
| Output | sensor.output |
| Status | sensor.status |
| Last Value | latest reading timestamp |
| Actions | View Data / Edit / Delete |

**Filters/search:** by Qube, protocol, status, output mode

**Bulk actions (Editor+):**
- Select multiple → "Set Output to influxdb" / "Set Status to inactive" / "Delete selected"

**Sensor Detail (inline expand or modal):**
- All fields from `config_json` (registers / OIDs / nodes / json_paths / objects / readings)
- Rendered in a read-only table, not raw JSON
- Protocol-specific column headers:

| Protocol | Columns |
|----------|---------|
| modbus_tcp | field_key, register_type, address, data_type, scale, unit |
| snmp | field_key, OID, unit |
| mqtt | field_key, json_path, unit |
| opcua | field_key, node_id, type, unit |
| http | field_key, json_path, unit |
| bacnet | field_key, object_type, object_instance, unit |
| lorawan | field_key, field, unit |
| dnp3 | field_key, group, variation, index, unit |

**Edit Sensor:**
- Tags editor (key=value rows)
- Output mode
- Table name
- Status (`active` / `disabled`) — **Note:** the API accepts `"active"` or `"disabled"`, NOT `"inactive"` (see `sensors.go:228`)
- `PUT /api/v1/sensors/{id}`

---

### 6.7 Telemetry & Data Explorer

**Route:** `/telemetry`
**Tabs:** Live View | History | Sensor Summary | MQTT Discovery | SNMP Walk

#### Live View Tab

- Connects to `WebSocket /ws/dashboard`
- Sensor selector: choose sensors to display (multi-select)
- Real-time value cards: current value + unit + sparkline (last 20 points)
- Toggle between card view and table view
- Pause/resume button

#### History Tab

- Sensor selector (searchable dropdown from org-wide sensor list)
- Field key selector (from sensor config fields)
- Time range picker: last 1h / 6h / 24h / 7d / custom from-to
- "Query" button → `GET /api/v1/data/readings?sensor_id=...&field=...&from=...&to=...`
- Line chart (Chart.js) showing value over time
- Export to CSV button
- Statistics panel: min, max, avg, last

#### Sensor Summary Tab

- `GET /api/v1/data/sensors/{id}/latest` for all org sensors
- Table: sensor name, field keys, latest values, units, timestamp
- Color coding: green (< 5 min old), yellow (< 30 min), red (> 30 min / no data)

#### MQTT Discovery Tab

Used to discover what topics and JSON fields an MQTT broker publishes — so you can build an
accurate MQTT device template.

**UI flow:**
1. Select target Qube (dropdown)
2. Enter broker details: Host, Port (default 1883), Wildcard Topic (default `#`), Duration (30s)
3. Click "Start Discovery" → sends `mqtt_discover` command:
   ```http
   POST /api/v1/qubes/{qubeId}/commands
   {"command": "mqtt_discover", "payload": {"broker_host": "...", "broker_port": 1883, "topic": "#", "duration_sec": 30}}
   ```
4. Poll `GET /api/v1/commands/{id}` every 2s until executed
5. Display results table:

| Topic | Detected Fields | Sample Value |
|-------|-----------------|--------------|
| shellies/em-ABC/emeter/0/status | power, voltage, current, pf | {"power": 245.5, ...} |
| sensors/room1/temp | value, unit | {"value": 22.4, "unit": "C"} |

6. "Build Template" button: pre-fills a new device template with:
   - `json_paths` entries for each detected field
   - `sensor_params_schema` with a `topic` field pre-filled with the selected topic
   - Redirects to `/templates/device` with the form pre-filled

**Step-by-step guide shown below the results:**
> 1. Note the topic pattern (e.g. `shellies/em-ABC123/emeter/0/status`)
> 2. Replace the device-specific part with a wildcard or param (e.g. use `topic` in sensor params)
> 3. For each detected field, add a `json_paths` entry with the field name and JSONPath
> 4. Create the device template, then add sensors using that template

#### SNMP Walk Tab

Used to discover all OIDs and values on an SNMP device — so you can pick the right OIDs for
an SNMP device template.

**UI flow:**
1. Select target Qube
2. Enter device details: Host IP, Community (default `public`), SNMP Version (default `2c`),
   Root OID (default `.1.3.6.1`)
3. Click "Walk Device" → sends `snmp_walk` command:
   ```http
   POST /api/v1/qubes/{qubeId}/commands
   {"command": "snmp_walk", "payload": {"host": "10.0.1.50", "community": "public", "version": "2c", "oid": ".1.3.6.1"}}
   ```
4. Poll until executed, then display results table:

| OID | Type | Value |
|-----|------|-------|
| .1.3.6.1.2.1.1.1.0 | OctetString | APC Smart-UPS 1500 |
| .1.3.6.1.4.1.318.1.1.1.2.2.1.0 | Integer | 100 |
| .1.3.6.1.4.1.318.1.1.1.4.2.2.0 | Integer | 1245 |

5. Search/filter bar to find relevant OIDs (by OID string or value)
6. Checkbox per row → "Add to Template" button builds an SNMP device template

**Step-by-step guide:**
> 1. Search for the metric you need (e.g. "power", "temperature", "battery")
> 2. Check the OID rows you want to monitor
> 3. Click "Build Template" to create an SNMP device template with those OIDs
> 4. Name each field (field_key) and set the unit

---

### 6.8 Commands & Remote Actions

**Route:** `/fleet/:qubeId/commands` or inline in Qube Detail → Commands Tab

The enterprise conf-agent supports 28 commands grouped into 7 categories.
Commands are delivered via WebSocket if the Qube is connected, otherwise queued for HTTP polling.

**Command Categories & Types:**

| Category | Commands |
|----------|----------|
| Container/Config | `ping`, `restart_qube`, `reboot`, `shutdown`, `restart_reader`, `stop_container`, `reload_config`, `update_sqlite`, `get_logs`, `list_containers` |
| Network | `reset_ips`, `set_eth`, `set_wifi`, `set_firewall` |
| Identity/System | `get_info`, `set_name`, `set_timezone` |
| Backup/Restore | `backup_data`, `restore_data` |
| Maintenance Mode | `repair_fs`, `backup_image`, `restore_image` |
| Service Mgmt | `service_add`, `service_rm`, `service_edit` |
| File Transfer | `put_file`, `get_file` |
| Discovery | `mqtt_discover`, `snmp_walk` |

**Key payloads:**

| Command | Key Payload Fields |
|---------|-------------------|
| `ping` | `target` (default: 8.8.8.8) |
| `restart_reader` | `reader_id` or `service` name |
| `stop_container` | `service` name |
| `get_logs` | `service` (optional), `lines` (default: 100) |
| `set_eth` | `interface`, `mode` (auto/static), + static: `address`, `gateway`, `dns` |
| `set_wifi` | `interface`, `mode`, `ssid`, `password`, `key_mgmt`, + static fields |
| `set_firewall` | `rules` — comma-separated `proto:net:port` entries |
| `set_name` | `name` |
| `set_timezone` | `timezone` (IANA name, e.g. `Asia/Colombo`) |
| `backup_data` / `restore_data` | `type` (cifs/nfs), `path`, `user`, `pass` |
| `service_add` | `name`, `type`, `version`, `ports` |
| `service_rm` / `service_edit` | `name`, (edit: `ports`) |
| `put_file` | `path` (relative, no `..`), `data` (base64) |
| `get_file` | `path` |

**UI:**
- Grouped command dropdown (by category above)
- Dynamic payload form rendered per command (see `commandPayloadFields` in DEVELOPER_GUIDE.md §11)
- Maintenance mode commands show a warning: "Device will go offline for several minutes"
- Command history table: command, delivery (ws/queued badge), status, sent_at, executed_at, result
- Poll `GET /api/v1/commands/:id` every 2s while status is pending/sent
- `POST /api/v1/qubes/{id}/commands`

---

### 6.9 Templates — Device Templates

**Route:** `/templates/device`
**API:**
- `GET /api/v1/device-templates` (+ `?protocol=` filter)
- `POST /api/v1/device-templates` (Editor+)
- `PUT /api/v1/device-templates/{id}` (Editor+)
- `DELETE /api/v1/device-templates/{id}` (Editor+)
- `PATCH /api/v1/device-templates/{id}/config` (Editor+ fine-grained)

**Layout:**

Left panel: Protocol filter tabs (All | Modbus | SNMP | MQTT | OPC-UA | HTTP | BACnet | LoRaWAN | DNP3)

Right panel: Template cards

Each template card shows:
- Name, Manufacturer, Model
- "GLOBAL" badge if is_global (superadmin templates)
- Protocol badge
- Field count (number of registers/OIDs/nodes/etc.)
- "Edit" / "Clone" / "Delete" buttons (role-restricted)

**Create/Edit Modal — 3 tabs:**

1. **Basic Info**: name, manufacturer, model, description, protocol
2. **Sensor Config**: protocol-specific field editor
   - Visual table editor for registers/OIDs/nodes/json_paths/objects/readings/points
   - "Add field" row with appropriate column inputs per protocol
   - "Remove" button per row
   - Also has "Raw JSON" toggle for advanced users
3. **Sensor Params Schema**: JSON Schema editor for what users fill in per sensor
   - Visual key-value property builder
   - Or raw JSON toggle

**PATCH fine-grained actions:**
- On the field editor table, each row has "Save" / "Delete" which calls
  `PATCH /api/v1/device-templates/{id}/config` with `{action: "add"|"update"|"delete", key, value}`

---

### 6.10 Templates — Reader Templates

**Route:** `/templates/reader`
**API:**
- `GET /api/v1/reader-templates`
- Superadmin: `POST`, `PUT`, `DELETE /api/v1/reader-templates/{id}`

**Layout:** Table of reader templates

| Column | Data |
|--------|------|
| Protocol | colored badge |
| Name | name |
| Image Suffix | image_suffix |
| Reader Standard | endpoint / multi_target badge |
| Connection Fields | count of properties in connection_schema |
| Actions | View / Edit (superadmin) / Delete (superadmin) |

**Edit Modal (Superadmin only):**
- Protocol dropdown
- Name, description
- Image suffix
- Reader standard (endpoint / multi_target)
- Connection schema: JSON editor with schema preview
  - Show rendered form preview from schema in real-time
- Env defaults: key-value table
- `POST /api/v1/reader-templates` or `PUT /api/v1/reader-templates/{id}`

---

### 6.11 Protocols

**Route:** `/protocols`
**API:** `GET /api/v1/protocols`

**Read-only page** for all roles. Shows:

| Column | Data |
|--------|------|
| ID | protocol.id (code name) |
| Label | protocol.label |
| Description | protocol.description |
| Reader Standard | endpoint / multi_target |
| Reader Templates | count (fetched from reader-templates filtered) |
| Device Templates | count (fetched from device-templates filtered) |

Informational callout: "New protocols are added by the system administrator via database migration.
See ADDING-PROTOCOLS.md for the exact procedure."

---

### 6.12 Users & Team Management

**Route:** `/users`
**Roles:** Admin+ can view and manage. Viewer sees read-only.
**API:**
- `GET /api/v1/users` — list org users
- `POST /api/v1/users` — invite user
- `PATCH /api/v1/users/{id}` — change role
- `DELETE /api/v1/users/{id}` — remove user

**Layout:**

Users table:
| Column | Data |
|--------|------|
| Email | user.email |
| Role | colored badge (viewer / editor / admin / superadmin) |
| Joined | created_at |
| Actions | Edit Role / Remove (Admin+) |

**Invite User form:**
- Email input
- Role dropdown (viewer / editor / admin)
- Note: invited user receives credentials manually (no email system in v2)
- `POST /api/v1/users`

**Edit Role:**
- Inline dropdown change → `PATCH /api/v1/users/{id}` with `{role}`
- Can't demote yourself below your own role
- Superadmin role cannot be assigned via UI (superadmin-only)

**Role Descriptions shown in UI:**

| Role | Can Do |
|------|--------|
| viewer | Read-only: view qubes, sensors, telemetry |
| editor | Create/edit readers, sensors, templates |
| admin | Everything + user management, Qube claim |
| superadmin | Everything + global templates, registry, reader templates |

---

### 6.13 Registry Settings (Superadmin Only)

**Route:** `/settings/registry`
**API:**
- `GET /api/v1/admin/registry`
- `PUT /api/v1/admin/registry`

**Layout:** Form with registry mode selection

**Mode: GitHub GHCR**
- GitHub Base URL (e.g. `ghcr.io/your-org/qube-enterprise`)
- All images resolved as `<base>/<image_suffix>:<arch>.latest`

**Mode: GitLab**
- GitLab Base URL
- Individual image overrides per container type

**Mode: Custom**
- Per-image full URL table
- img_conf_agent, img_influx_sql, img_modbus_reader, img_snmp_reader,
  img_mqtt_reader, img_opcua_reader, img_http_reader, img_bacnet_reader,
  img_lorawan_reader, img_dnp3_reader

Preview: shows resolved full image path for each container type.

---

### 6.14 My Profile

**Route:** `/profile`
**API:** `GET /api/v1/users/me`

Shows:
- Email
- Role
- Org ID
- Org Name (if returned in future)
- JWT expiry (decoded from token)
- "Change Password" (future: add `PUT /api/v1/users/me/password` endpoint)
- "Sign Out" — clear localStorage, redirect to `/login`

---

## 7. API-to-UI Full Mapping

### Cloud API (port 8080)

| Endpoint | Method | Used In |
|----------|--------|---------|
| `/api/v1/auth/register` | POST | Login page — Create Org |
| `/api/v1/auth/login` | POST | Login page — Sign In |
| `/api/v1/users/me` | GET | Dashboard, Profile |
| `/api/v1/users` | GET | Users page |
| `/api/v1/users` | POST | Users page — Invite |
| `/api/v1/users/{id}` | PATCH | Users page — Edit Role |
| `/api/v1/users/{id}` | DELETE | Users page — Remove |
| `/api/v1/qubes` | GET | Dashboard, Fleet list |
| `/api/v1/qubes/claim` | POST | Fleet — Claim Qube |
| `/api/v1/qubes/{id}` | GET | Qube Detail — Overview |
| `/api/v1/qubes/{id}` | PUT | Qube Detail — Edit |
| `/api/v1/qubes/{id}/readers` | GET | Qube Detail — Readers Tab, Onboarding reader card display |
| `/api/v1/qubes/{id}/readers` | POST | Manual reader creation (advanced, not wizard) |
| `/api/v1/qubes/{id}/sensors` | GET | Qube Detail — Sensors Tab, Org Sensors |
| `/api/v1/qubes/{id}/sensors` | POST | **Onboarding Wizard submit** (smart endpoint — auto-reader) |
| `/api/v1/qubes/{id}/containers` | GET | Qube Detail — Containers Tab |
| `/api/v1/qubes/{id}/commands` | POST | Commands Tab — Send Command, MQTT Discovery, SNMP Walk |
| `/api/v1/readers/{id}` | GET | Reader Detail |
| `/api/v1/readers/{id}` | PUT | Reader Edit |
| `/api/v1/readers/{id}` | DELETE | Reader Delete |
| `/api/v1/readers/{id}/sensors` | GET | Reader → Sensors list |
| `/api/v1/readers/{id}/sensors` | POST | Sensor creation when reader is explicitly specified |
| `/api/v1/sensors/{id}` | PUT | Sensor Edit |
| `/api/v1/sensors/{id}` | DELETE | Sensor Delete |
| `/api/v1/protocols` | GET | Onboarding Wizard Step 2, Protocols page |
| `/api/v1/reader-templates` | GET | Reader Templates page, Onboarding Step 4 |
| `/api/v1/reader-templates/{id}` | GET | Reader Template Detail |
| `/api/v1/reader-templates` | POST | Reader Templates page — Create (SA) |
| `/api/v1/reader-templates/{id}` | PUT | Reader Templates page — Edit (SA) |
| `/api/v1/reader-templates/{id}` | DELETE | Reader Templates page — Delete (SA) |
| `/api/v1/device-templates` | GET | Device Templates page, Onboarding Step 3 |
| `/api/v1/device-templates/{id}` | GET | Device Template Detail |
| `/api/v1/device-templates` | POST | Device Templates page — Create |
| `/api/v1/device-templates/{id}` | PUT | Device Templates page — Edit |
| `/api/v1/device-templates/{id}` | DELETE | Device Templates page — Delete |
| `/api/v1/device-templates/{id}/config` | PATCH | Device Template field editor |
| `/api/v1/data/sensors/{id}/latest` | GET | Sensors table latest value, Telemetry Summary |
| `/api/v1/data/readings` | GET | Telemetry History chart |
| `/api/v1/admin/registry` | GET | Registry Settings (SA) |
| `/api/v1/admin/registry` | PUT | Registry Settings (SA) |
| `/ws` | WS | Qube config sync (not UI-initiated) |
| `/ws/dashboard` | WS | Telemetry Live View, Dashboard live feed |
| `/health` | GET | Settings page — health check |

### TP-API (port 8081) — informational display only in UI

These are Qube-facing. The UI can show their status/format for debugging purposes
(e.g., in a "Debug" panel or Qube detail advanced view):

| Endpoint | Display In UI |
|----------|---------------|
| `/v1/device/register` | Qube Detail — show onboarding status |
| `/v1/sync/state` | Qube Detail — config hash + version |
| `/v1/sync/config` | (not displayed, internal) |
| `/v1/heartbeat` | Qube Detail — last_seen derived from this |
| `/v1/commands/poll` | Commands tab — "polling" indicator |
| `/v1/commands/{id}/ack` | Commands tab — ack status |
| `/v1/telemetry/ingest` | Telemetry page — ingest rate |

---

## 8. Sensor Add Flow (Template-Driven)

This is the most critical workflow. Use the **smart sensor endpoint** (`POST /api/v1/qubes/:id/sensors`)
which handles reader creation/reuse automatically. The old two-step flow still works but is no
longer recommended for the onboarding wizard.

### Smart Sensor Creation (Recommended)

```
User selects Qube → User selects Protocol → User selects Device Template
    ↓
GET /api/v1/device-templates?protocol=modbus_tcp
    → returns [{id, name, sensor_config: {registers:[...]}, sensor_params_schema: {...}}]

User fills in sensor_params_schema form fields (e.g. unit_id=3, register_offset=0)

For endpoint protocol (Modbus TCP):
    User enters connection details (host=192.168.1.10, port=502) or selects existing reader

POST /api/v1/qubes/{qubeId}/sensors
body: {
    name: "PM5100 Rack A",
    template_id: "<device_template_uuid>",
    params: {
        "unit_id": 3,
        "register_offset": 0
    },
    reader_config: {"host":"192.168.1.10","port":502,"poll_interval_sec":20},
    reader_name: "Rack Panel A",
    output: "influxdb",
    tags_json: {"name":"PM5100_RackA","location":"ServerRoom"},
    table_name: "Measurements"
}
→ Server finds or creates the correct reader (matches by fingerprint modbus://192.168.1.10:502)
→ Server merges device_template.sensor_config + params → sensor.config_json
→ Server calls recomputeConfigHash() → pushes WebSocket "config_update" to Qube
→ conf-agent receives push → writes SQLite → restarts reader container
→ Reader reads SQLite → begins polling registers
```

**For SNMP (multi_target) — no reader_config needed:**
```
POST /api/v1/qubes/{qubeId}/sensors
body: {
    name: "APC UPS DataCenter",
    template_id: "<apc_ups_template_uuid>",
    params: {
        "host": "10.0.1.50",
        "community": "public",
        "version": "2c"
    },
    output: "influxdb",
    tags_json: {"name":"APC_UPS_DC1","location":"DataCenter"}
}
→ Server finds existing SNMP reader on Qube, or creates one automatically
→ Sensor is created under that reader
→ No need to check for existing readers in the UI
```

**Key field name corrections** (use these exact names in `params`):

| Protocol | Field | Old (wrong) | Correct |
|----------|-------|-------------|---------|
| SNMP | Device IP | `ip_address` | `host` |
| SNMP | SNMP version | `snmp_version` | `version` |
| SNMP | Poll interval | `fetch_interval_sec` (reader) | `poll_interval_sec` (reader) |
| SNMP | Timeout | `timeout_sec` (reader) | `timeout_ms` (reader) |
| MQTT | Broker host | full `broker_url` | `broker_host` + `broker_port` |

---

## 9. Protocol-Specific UI Forms

The UI must render dynamic forms from `connection_schema` and `sensor_params_schema`.
Implement a lightweight JSON Schema → HTML form renderer:

### JSON Schema Form Renderer

```js
function renderSchemaForm(schema, existingValues = {}) {
    // schema = {type:"object", properties:{...}, required:[...]}
    // Returns: div containing labeled inputs
    const form = document.createElement('div');
    for (const [key, prop] of Object.entries(schema.properties || {})) {
        const wrapper = document.createElement('div');
        const label = document.createElement('label');
        label.textContent = prop.title || key;
        if (schema.required?.includes(key)) label.textContent += ' *';
        
        let input;
        if (prop.enum) {
            input = document.createElement('select');
            prop.enum.forEach(v => {
                const opt = document.createElement('option');
                opt.value = v; opt.textContent = v;
                if (v === (existingValues[key] ?? prop.default)) opt.selected = true;
                input.appendChild(opt);
            });
        } else if (prop.type === 'integer' || prop.type === 'number') {
            input = document.createElement('input');
            input.type = 'number';
            input.min = prop.minimum ?? '';
            input.max = prop.maximum ?? '';
            input.value = existingValues[key] ?? prop.default ?? '';
        } else {
            input = document.createElement('input');
            input.type = prop.format === 'password' ? 'password' : 'text';
            input.value = existingValues[key] ?? prop.default ?? '';
            if (prop.format === 'ipv4') input.placeholder = '192.168.1.x';
            if (prop.format === 'uri')  input.placeholder = 'http://...';
        }
        input.name = key;
        input.dataset.type = prop.type;
        if (schema.required?.includes(key)) input.required = true;
        if (prop.description) {
            const hint = document.createElement('small');
            hint.textContent = prop.description;
            wrapper.appendChild(hint);
        }
        wrapper.appendChild(label);
        wrapper.appendChild(input);
        form.appendChild(wrapper);
    }
    return form;
}

function collectFormValues(formEl) {
    // Collect values, type-coerce integers
    const result = {};
    formEl.querySelectorAll('input, select').forEach(el => {
        if (el.dataset.type === 'integer') result[el.name] = parseInt(el.value);
        else if (el.dataset.type === 'number') result[el.name] = parseFloat(el.value);
        else result[el.name] = el.value;
    });
    return result;
}
```

### Protocol-Specific Sensor Config Field Editor

When creating/editing a device template, render a table appropriate to the protocol:

| Protocol | Array Key | Column Headers |
|----------|-----------|----------------|
| modbus_tcp | registers | field_key, register_type (Holding/Input/Coil), address, data_type (uint16/int16/float32/uint32/int32), scale, unit |
| snmp | oids | field_key, oid, scale (optional multiplier), unit |
| mqtt | json_paths | field_key, json_path, topic (optional per-field override), unit |
| opcua | nodes | field_key, node_id (ns=X;i=Y), type (float/int/bool/string), unit |
| http | json_paths | field_key, json_path, scale (optional multiplier), unit |
| bacnet | objects | field_key, object_type (enum), object_instance, unit |
| lorawan | readings | field_key, field (raw field name from NS payload), unit |
| dnp3 | points | field_key, group, variation, index, unit |

---

## 10. Role-Based Access Control in UI

All UI elements must be conditionally rendered based on `state.role`:

```js
const canEdit   = ['editor','admin','superadmin'].includes(state.role);
const canAdmin  = ['admin','superadmin'].includes(state.role);
const isSuperAdmin = state.role === 'superadmin';
```

| UI Element | Visible To |
|------------|-----------|
| "Claim Qube" button | admin+ |
| "Add Reader" button | editor+ |
| "Add Sensor" button | editor+ |
| "Edit" buttons (readers, sensors, qubes) | editor+ |
| "Delete" buttons | editor+ |
| "Send Command" form | editor+ |
| Device Templates — create/edit | editor+ |
| Device Templates — mark global | superadmin |
| Reader Templates — create/edit/delete | superadmin |
| Users page | admin+ (read), admin+ (manage) |
| Registry Settings | superadmin |
| Protocols page | viewer+ (read-only) |
| Telemetry data | viewer+ |

Show a lock icon + tooltip "Requires [role] access" on disabled buttons rather than hiding them
entirely — this helps users understand the permission model.

---

## 11. Real-Time WebSocket Integration

### Dashboard WebSocket

```js
function connectDashboardWS() {
    const ws = new WebSocket(`ws://${state.baseUrl.replace('http://','')}/ws/dashboard`);
    ws.onopen = () => {
        ws.send(JSON.stringify({type: 'auth', token: state.token}));
        // Alternative: connect with ?token=<jwt> query param
    };
    ws.onmessage = (evt) => {
        const msg = JSON.parse(evt.data);
        if (msg.type === 'sensor_reading') {
            updateLiveFeed(msg);    // scrolling table
            updateSensorCard(msg);  // live value cards
        }
        if (msg.type === 'config_update') {
            showNotification(`Config update pushed to ${msg.qube_id}`);
        }
        if (msg.type === 'qube_connected') {
            updateQubeStatus(msg.qube_id, 'online');
        }
        if (msg.type === 'qube_disconnected') {
            updateQubeStatus(msg.qube_id, 'offline');
        }
    };
    ws.onclose = () => {
        setTimeout(connectDashboardWS, 5000);  // auto-reconnect
    };
    state.ws = ws;
}
```

### Config Sync Status Indicator

In the Fleet list and Qube Detail header, show a sync indicator:
- Green check: `config_version` matches last known, `ws_connected = true`
- Yellow spinner: WebSocket push sent, waiting for ack
- Red warning: last_seen > 5 min ago or ws_connected = false

---

## 12. Implementation Phases

### Phase 1 — Core Infrastructure & Auth (Week 1)

**Deliverables:**
- [ ] New `test-ui/app.js` with state management, router, API client
- [ ] Login / Register page
- [ ] Dashboard skeleton (fleet health widgets, no live data yet)
- [ ] Fleet list page (Qubes table + claim modal)
- [ ] Qube Detail page — Overview tab only
- [ ] JSON Schema form renderer (core component)
- [ ] Role-based element visibility
- [ ] Health check + base URL config

**Key APIs:** `/auth/*`, `/qubes`, `/users/me`, `/health`

---

### Phase 2 — Device Onboarding Wizard (Week 2)

**Deliverables:**
- [ ] Protocol selection step (cards from `/protocols`)
- [ ] Device template selection (cards from `/device-templates?protocol=`)
- [ ] Auto-reader logic: endpoint protocols show existing reader cards + new connection form
- [ ] Auto-reader logic: multi_target protocols show status badge, no connection form
- [ ] Sensor params form (rendered from `sensor_params_schema`)
- [ ] Review + submit flow using `POST /api/v1/qubes/{id}/sensors` (smart endpoint)
- [ ] Success confirmation with config sync status (poll `new_hash` vs `qube.config_hash`)
- [ ] Sensor list on Qube Detail — Sensors Tab

**Key APIs:** `/protocols`, `/device-templates`, `/reader-templates`,
`/qubes/{id}/readers` (GET), `/qubes/{id}/sensors` (POST — smart endpoint)

---

### Phase 3 — Full Readers & Sensors Management (Week 3)

**Deliverables:**
- [ ] Qube Detail — Readers Tab (CRUD)
- [ ] Qube Detail — Sensors Tab (list, edit, delete, latest value)
- [ ] Qube Detail — Containers Tab
- [ ] Org-wide Sensors page (all Qubes, filters, bulk actions)
- [ ] Sensor config field viewer (protocol-specific table rendering)
- [ ] Reader edit form (dynamic from connection_schema)

**Key APIs:** `/readers/{id}` (GET/PUT/DELETE), `/sensors/{id}` (PUT/DELETE),
`/qubes/{id}/containers`, `/data/sensors/{id}/latest`

---

### Phase 4 — Templates & Protocols Admin (Week 4)

**Deliverables:**
- [ ] Device Templates page (list + filter by protocol)
- [ ] Device Template create/edit modal — 3 tabs (basic, field editor, params schema)
- [ ] Protocol-specific field editor tables (all 8 protocols)
- [ ] PATCH `/device-templates/{id}/config` integration
- [ ] Reader Templates page (list + superadmin CRUD)
- [ ] Protocols page (read-only info)

**Key APIs:** `/device-templates` (all), `/reader-templates` (all)

---

### Phase 5 — Telemetry Dashboard (Week 5)

**Deliverables:**
- [ ] WebSocket dashboard connection + live feed table
- [ ] Live sensor value cards with mini sparklines
- [ ] Telemetry History: sensor picker + time range + Chart.js line chart
- [ ] Sensor Summary table (latest values for all org sensors)
- [ ] Export to CSV
- [ ] Dashboard widgets (fleet health donut, online count, etc.)
- [ ] MQTT Discovery tab: form → `mqtt_discover` command → topic/field results table → build template
- [ ] SNMP Walk tab: form → `snmp_walk` command → OID/value results table → build template

**Key APIs:** `/data/sensors/{id}/latest`, `/data/readings`, WebSocket `/ws/dashboard`,
`/qubes/{id}/commands` (mqtt_discover, snmp_walk), `/commands/{id}` (poll result)

---

### Phase 6 — Commands, Users & Admin (Week 6)

**Deliverables:**
- [ ] Qube Detail — Commands Tab (send + history)
- [ ] Users & Team Management page
- [ ] Registry Settings page (superadmin)
- [ ] Profile page
- [ ] My Profile page with JWT info

**Key APIs:** `/qubes/{id}/commands`, `/users` (all), `/admin/registry`

---

### Phase 7 — New Protocols Integration (Week 7)

**Deliverables:**
- [ ] Add SQL migrations for BACnet, LoRaWAN, DNP3 protocols + templates
- [ ] Add new device templates SQL (Section 2)
- [ ] Update `protocolArrayKey()` in `templates.go`
- [ ] Update `standards/TEMPLATE_STANDARD.md` (Section 3.1)
- [ ] Update `standards/READER_STANDARD.md` (Section 3.2)
- [ ] Verify UI protocol-specific forms render correctly for new protocols
- [ ] Add BACnet/LoRaWAN/DNP3 to field editor table (Section 9)

**Key code changes:** `cloud/internal/api/templates.go:protocolArrayKey()`,
`cloud/migrations/002_global_data.sql` or new `004_new_protocols.sql`

---

### Phase 8 — Polish & Production Readiness (Week 8)

**Deliverables:**
- [ ] Responsive layout (mobile-friendly for field technicians)
- [ ] Dark/light theme toggle
- [ ] Error handling: all API errors shown with retry option
- [ ] Loading states on all async operations
- [ ] Confirmation dialogs on destructive actions
- [ ] Keyboard navigation (accessibility)
- [ ] Browser storage: remember last selected Qube, base URL, preferences
- [ ] Print-friendly sensor inventory report

---

## 13. File Structure for New UI

```
test-ui/
├── index.html                    ← Shell: nav, router outlet, modals layer
├── app.js                        ← State, auth, client-side router
├── api.js                        ← All fetch() wrappers for every API endpoint
├── components/
│   ├── schema-form.js            ← JSON Schema → HTML form renderer
│   ├── modal.js                  ← Reusable modal component
│   ├── table.js                  ← Sortable/filterable table component
│   ├── sensor-fields-editor.js   ← Protocol-specific register/OID/node editor
│   ├── chart.js                  ← Chart.js wrapper for telemetry charts
│   └── ws-feed.js                ← WebSocket live feed component
├── pages/
│   ├── login.js
│   ├── dashboard.js
│   ├── fleet.js                  ← Qube list + detail tabs
│   ├── onboarding.js             ← Add Device wizard (multi-step)
│   ├── sensors.js                ← Org-wide sensor inventory
│   ├── telemetry.js              ← Live + history views
│   ├── commands.js
│   ├── device-templates.js
│   ├── reader-templates.js
│   ├── protocols.js
│   ├── users.js
│   ├── registry.js               ← Superadmin only
│   └── profile.js
├── styles/
│   ├── main.css                  ← Layout, nav, theme variables
│   ├── components.css            ← Modal, table, form, badge styles
│   └── pages.css                 ← Page-specific styles
└── UI_DEVELOPMENT_PLAN.md        ← This file
```

---

## Appendix A — Protocol Badge Colors

Consistent protocol color coding across all pages:

| Protocol | Color |
|----------|-------|
| modbus_tcp | #e85d04 (industrial orange) |
| snmp | #6a0dad (purple) |
| mqtt | #2196F3 (blue) |
| opcua | #009688 (teal) |
| http | #4CAF50 (green) |
| bacnet | #FF9800 (amber) |
| lorawan | #795548 (brown) |
| dnp3 | #F44336 (red — utility/critical) |

---

## Appendix B — Status Badge Colors

| Status | Color |
|--------|-------|
| online / ws_connected | green |
| offline | red |
| unclaimed | grey |
| active (sensor) | green |
| inactive (sensor) | yellow |
| pending (command) | blue |
| ack (command) | green |
| failed (command) | red |
| endpoint (reader_standard) | blue |
| multi_target (reader_standard) | purple |
| GLOBAL (template) | gold |

---

## Appendix C — Sensor Output Modes

| Mode | Meaning | UI Label |
|------|---------|----------|
| `influxdb` | Data stored to TimescaleDB only | "Store" |
| `live` | Streamed to WebSocket dashboard only | "Live" |
| `influxdb,live` | Both stored and streamed | "Store + Live" |

---

---

## Related Documents

- **`DEVELOPER_GUIDE.md`** — Complete API reference with request/response examples,
  template merge mechanics, superadmin workflows, and protocol-specific form reference.
  Give this to developers building the UI.

- **`../ADDING-PROTOCOLS.md`** — How to add a new protocol (SQL + Go code + Docker)
- **`../standards/TEMPLATE_STANDARD.md`** — sensor_config format per protocol
- **`../UI-API-GUIDE.md`** — Curl-based API reference

---

*End of UI Development Plan — Qube Enterprise v2*
*Generated: 2026-04-07*
