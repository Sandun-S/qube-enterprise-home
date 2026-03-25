-- 003_device_catalog.sql
-- Real-world device templates for the Qube Enterprise catalog.
-- IoT team maintains these. All orgs can use them read-only.
-- Add more with: POST /api/v1/catalog  (X-IoT-Admin: true)

-- ===================== MODBUS TCP DEVICES =====================

-- Schneider Electric PM5100 Power Meter
INSERT INTO sensor_templates (name, protocol, description, is_global, config_json, influx_fields_json) VALUES
(
  'Schneider PM5100',
  'modbus_tcp',
  'Schneider Electric PM5100 power meter — active power, voltage L-L, current, energy',
  TRUE,
  '{
    "registers": [
      {"address": 3000, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1,  "field_key": "active_power_w",  "table": "Measurements"},
      {"address": 3020, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1,  "field_key": "voltage_ll_v",    "table": "Measurements"},
      {"address": 3054, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.01, "field_key": "current_a",       "table": "Measurements"},
      {"address": 3204, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1,  "field_key": "energy_kwh",      "table": "Measurements"},
      {"address": 3110, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.001,"field_key": "power_factor",    "table": "Measurements"},
      {"address": 3060, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1,  "field_key": "frequency_hz",    "table": "Measurements"}
    ]
  }',
  '{
    "active_power_w":  {"display_label": "Active Power",  "unit": "W"},
    "voltage_ll_v":    {"display_label": "Voltage (L-L)", "unit": "V"},
    "current_a":       {"display_label": "Current",       "unit": "A"},
    "energy_kwh":      {"display_label": "Energy",        "unit": "kWh"},
    "power_factor":    {"display_label": "Power Factor",  "unit": ""},
    "frequency_hz":    {"display_label": "Frequency",     "unit": "Hz"}
  }'
),
-- Schneider PM2100
(
  'Schneider PM2100',
  'modbus_tcp',
  'Schneider Electric PM2100 power meter — basic power and energy',
  TRUE,
  '{
    "registers": [
      {"address": 3000, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1, "field_key": "active_power_w", "table": "Measurements"},
      {"address": 3020, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1, "field_key": "voltage_v",      "table": "Measurements"},
      {"address": 3204, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 0.1, "field_key": "energy_kwh",     "table": "Measurements"}
    ]
  }',
  '{
    "active_power_w": {"display_label": "Active Power", "unit": "W"},
    "voltage_v":      {"display_label": "Voltage",      "unit": "V"},
    "energy_kwh":     {"display_label": "Energy",       "unit": "kWh"}
  }'
),
-- Generic Modbus Holding Register (for unknown devices)
(
  'Generic Modbus Register',
  'modbus_tcp',
  'Generic Modbus TCP holding register — single value. Add registers manually.',
  TRUE,
  '{
    "registers": [
      {"address": 0, "register_type": "Holding", "data_type": "uint16", "count": 1, "scale": 1.0, "field_key": "value", "table": "Measurements"}
    ]
  }',
  '{
    "value": {"display_label": "Value", "unit": ""}
  }'
),
-- Eastron SDM630 three-phase energy meter
(
  'Eastron SDM630',
  'modbus_tcp',
  'Eastron SDM630 three-phase energy meter — power, voltage, current, energy',
  TRUE,
  '{
    "registers": [
      {"address": 0,   "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "voltage_l1_v",    "table": "Measurements"},
      {"address": 2,   "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "voltage_l2_v",    "table": "Measurements"},
      {"address": 4,   "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "voltage_l3_v",    "table": "Measurements"},
      {"address": 6,   "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "current_l1_a",    "table": "Measurements"},
      {"address": 8,   "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "current_l2_a",    "table": "Measurements"},
      {"address": 10,  "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "current_l3_a",    "table": "Measurements"},
      {"address": 52,  "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "active_power_w",  "table": "Measurements"},
      {"address": 342, "register_type": "Input", "data_type": "float32", "count": 2, "scale": 1.0, "field_key": "energy_kwh",      "table": "Measurements"}
    ]
  }',
  '{
    "voltage_l1_v":   {"display_label": "Voltage L1",     "unit": "V"},
    "voltage_l2_v":   {"display_label": "Voltage L2",     "unit": "V"},
    "voltage_l3_v":   {"display_label": "Voltage L3",     "unit": "V"},
    "current_l1_a":   {"display_label": "Current L1",     "unit": "A"},
    "current_l2_a":   {"display_label": "Current L2",     "unit": "A"},
    "current_l3_a":   {"display_label": "Current L3",     "unit": "A"},
    "active_power_w": {"display_label": "Active Power",   "unit": "W"},
    "energy_kwh":     {"display_label": "Energy",         "unit": "kWh"}
  }'
),

-- ===================== SNMP DEVICES =====================

-- APC Smart-UPS
(
  'APC Smart-UPS',
  'snmp',
  'APC Smart-UPS — battery capacity, runtime, input/output voltage, load',
  TRUE,
  '{
    "map_file": "apc-ups.csv",
    "table": "snmp_data",
    "oids": [
      {"oid": "1.3.6.1.4.1.318.1.1.1.2.2.1.0",  "field_key": "battery_capacity_pct"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.2.2.3.0",  "field_key": "battery_runtime_min"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.3.2.1.0",  "field_key": "input_voltage_v"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.4.2.1.0",  "field_key": "output_voltage_v"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.4.2.3.0",  "field_key": "load_pct"},
      {"oid": "1.3.6.1.4.1.318.1.1.1.2.2.4.0",  "field_key": "battery_temp_c"}
    ]
  }',
  '{
    "battery_capacity_pct": {"display_label": "Battery Capacity", "unit": "%"},
    "battery_runtime_min":  {"display_label": "Battery Runtime",  "unit": "min"},
    "input_voltage_v":      {"display_label": "Input Voltage",    "unit": "V"},
    "output_voltage_v":     {"display_label": "Output Voltage",   "unit": "V"},
    "load_pct":             {"display_label": "Load",             "unit": "%"},
    "battery_temp_c":       {"display_label": "Battery Temp",     "unit": "C"}
  }'
),
-- Liebert GXT RT UPS (matching real Qube Lite deployment)
(
  'Liebert GXT RT UPS',
  'snmp',
  'Liebert GXT RT UPS — battery, runtime, voltages, load',
  TRUE,
  '{
    "map_file": "gxt-rt-ups.csv",
    "table": "snmp_data",
   "oids": [
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.1",  "field_key": "battery_capacity_pct"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.2",  "field_key": "battery_runtime_min"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.3",  "field_key": "input_voltage_v"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.4",  "field_key": "output_voltage_v"},
      {"oid": "1.3.6.1.4.1.476.1.42.3.9.20.1.20.1.2.1.4.5",  "field_key": "load_pct"}
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

-- ===================== OPC-UA DEVICES =====================

-- Generic OPC-UA power meter
(
  'Generic OPC-UA Power Meter',
  'opcua',
  'Generic OPC-UA power meter. Update node IDs to match your server namespace.',
  TRUE,
  '{
    "nodes": [
      {"node_id": "ns=2;points/ActivePower",  "field_key": "active_power_w",  "data_type": "float", "table": "Measurements", "freq_sec": 10},
      {"node_id": "ns=2;points/Voltage",      "field_key": "voltage_v",       "data_type": "float", "table": "Measurements", "freq_sec": 10},
      {"node_id": "ns=2;points/Current",      "field_key": "current_a",       "data_type": "float", "table": "Measurements", "freq_sec": 10},
      {"node_id": "ns=2;points/Energy",       "field_key": "energy_kwh",      "data_type": "float", "table": "Measurements", "freq_sec": 30}
    ]
  }',
  '{
    "active_power_w": {"display_label": "Active Power", "unit": "W"},
    "voltage_v":      {"display_label": "Voltage",      "unit": "V"},
    "current_a":      {"display_label": "Current",      "unit": "A"},
    "energy_kwh":     {"display_label": "Energy",       "unit": "kWh"}
  }'
),
-- Generic OPC-UA temperature sensor
(
  'Generic OPC-UA Temperature',
  'opcua',
  'Generic OPC-UA temperature sensor. Set node_id in address_params when adding.',
  TRUE,
  '{
    "nodes": [
      {"node_id": "ns=2;points/Temperature", "field_key": "temperature_c", "data_type": "float", "table": "Measurements", "freq_sec": 15},
      {"node_id": "ns=2;points/Humidity",    "field_key": "humidity_pct",  "data_type": "float", "table": "Measurements", "freq_sec": 15}
    ]
  }',
  '{
    "temperature_c": {"display_label": "Temperature", "unit": "C"},
    "humidity_pct":  {"display_label": "Humidity",    "unit": "%"}
  }'
),

-- ===================== MQTT DEVICES =====================

-- Generic MQTT JSON sensor
(
  'Generic MQTT JSON Sensor',
  'mqtt',
  'MQTT device publishing JSON. Set topic_suffix in address_params when adding.',
  TRUE,
  '{
    "topic_pattern": "{base_topic}/{topic_suffix}",
    "readings": [
      {"json_path": "$.value",       "field_key": "value",       "unit": ""},
      {"json_path": "$.temperature", "field_key": "temperature", "unit": "C"},
      {"json_path": "$.humidity",    "field_key": "humidity",    "unit": "%"},
      {"json_path": "$.pressure",    "field_key": "pressure",    "unit": "hPa"}
    ]
  }',
  '{
    "value":       {"display_label": "Value",       "unit": ""},
    "temperature": {"display_label": "Temperature", "unit": "C"},
    "humidity":    {"display_label": "Humidity",    "unit": "%"},
    "pressure":    {"display_label": "Pressure",    "unit": "hPa"}
  }'
),
-- MQTT energy monitor (e.g. Shelly EM)
(
  'MQTT Energy Monitor (Shelly EM)',
  'mqtt',
  'Shelly EM MQTT energy monitor — power and energy consumption',
  TRUE,
  '{
    "topic_pattern": "{base_topic}/{topic_suffix}",
    "readings": [
      {"json_path": "$.power",        "field_key": "active_power_w", "unit": "W"},
      {"json_path": "$.total",        "field_key": "energy_kwh",     "unit": "kWh"},
      {"json_path": "$.voltage",      "field_key": "voltage_v",      "unit": "V"},
      {"json_path": "$.current",      "field_key": "current_a",      "unit": "A"},
      {"json_path": "$.pf",           "field_key": "power_factor",   "unit": ""}
    ]
  }',
  '{
    "active_power_w": {"display_label": "Active Power",  "unit": "W"},
    "energy_kwh":     {"display_label": "Energy",        "unit": "kWh"},
    "voltage_v":      {"display_label": "Voltage",       "unit": "V"},
    "current_a":      {"display_label": "Current",       "unit": "A"},
    "power_factor":   {"display_label": "Power Factor",  "unit": ""}
  }'
);

-- Add version column to sensor_templates for tracking updates
ALTER TABLE sensor_templates ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;

-- Vertiv ITA2 UPS
INSERT INTO sensor_templates (name, protocol, description, is_global, config_json, influx_fields_json)
VALUES (
  'Vertiv ITA2 UPS',
  'snmp',
  'Vertiv ITA2 3-phase UPS — input/output voltages, currents, load, battery',
  TRUE,
  '{
    "map_file": "vertiv-ita2.csv",
    "table": "snmp_data",
    "oids": [
      {"oid": "1.3.6.1.4.1.13400.2.54.2.1.1.0", "field_key": "systemStatus"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.1.2.0", "field_key": "upsOutputSource"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.2.1.0", "field_key": "inputPhaseVoltageA"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.2.2.0", "field_key": "inputPhaseVoltageB"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.2.3.0", "field_key": "inputPhaseVoltageC"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.2.4.0", "field_key": "inputFrequency"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.1.0", "field_key": "outputPhaseVoltageA"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.2.0", "field_key": "outputPhaseVoltageB"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.3.0", "field_key": "outputPhaseVoltageC"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.4.0", "field_key": "outputCurrentA"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.5.0", "field_key": "outputCurrentB"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.6.0", "field_key": "outputCurrentC"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.7.0", "field_key": "outputFrequency"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.8.0", "field_key": "outputActivePowerA"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.9.0", "field_key": "outputActivePowerB"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.10.0", "field_key": "outputActivePowerC"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.14.0", "field_key": "outputLoadA"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.15.0", "field_key": "outputLoadB"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.3.16.0", "field_key": "outputLoadC"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.7.0", "field_key": "batteryRemainsTime"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.8.0", "field_key": "batteryTemperature"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.10.0", "field_key": "batteryCapacity"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.1.0", "field_key": "positiveBatteryVoltage"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.3.0", "field_key": "positiveBatteryChargingCurrent"},
      {"oid": "1.3.6.1.4.1.13400.2.54.2.5.4.0", "field_key": "positiveBatteryDischargingCurrent"}
    ]
  }',
  '{
    "systemStatus": {"display_label": "Systemstatus", "unit": ""},
    "upsOutputSource": {"display_label": "Upsoutputsource", "unit": ""},
    "inputPhaseVoltageA": {"display_label": "Inputphasevoltagea", "unit": ""},
    "inputPhaseVoltageB": {"display_label": "Inputphasevoltageb", "unit": ""},
    "inputPhaseVoltageC": {"display_label": "Inputphasevoltagec", "unit": ""},
    "inputFrequency": {"display_label": "Inputfrequency", "unit": ""},
    "outputPhaseVoltageA": {"display_label": "Outputphasevoltagea", "unit": ""},
    "outputPhaseVoltageB": {"display_label": "Outputphasevoltageb", "unit": ""},
    "outputPhaseVoltageC": {"display_label": "Outputphasevoltagec", "unit": ""},
    "outputCurrentA": {"display_label": "Outputcurrenta", "unit": ""},
    "outputCurrentB": {"display_label": "Outputcurrentb", "unit": ""},
    "outputCurrentC": {"display_label": "Outputcurrentc", "unit": ""}
  }'
);

