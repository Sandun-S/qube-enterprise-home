-- 004_template_updates.sql
-- Updates existing global device templates with real-world OIDs and adds new production templates.
-- Run this against an existing qubedb to apply without a full reset.

-- ===================== FIX EXISTING SNMP TEMPLATES =====================

-- Fix Liebert GXT RT UPS — replace placeholder OIDs with real RFC 1628 MIB OIDs
UPDATE device_templates SET
    description = 'Liebert GXT RT UPS — RFC 1628 MIB — battery status, runtime, input/output voltage, current, power, load',
    sensor_config = '{
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
    }'
WHERE name = 'Liebert GXT RT UPS' AND is_global = TRUE;

-- Expand Vertiv ITA2 UPS — add missing OIDs (inputCurrents, powerFactor, bypass, negativeBattery, environment temp, discharge count)
UPDATE device_templates SET
    description = 'Vertiv ITA2 3-phase UPS — full telemetry: input/output voltages, currents, power, load, bypass, battery',
    sensor_config = '{
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
    }'
WHERE name = 'Vertiv ITA2 UPS' AND is_global = TRUE;

-- ===================== NEW TEMPLATES =====================

INSERT INTO device_templates (protocol, name, manufacturer, model, description, is_global, sensor_config, sensor_params_schema)
VALUES

-- Vertiv APM150 UPS (Synergi MIB — 1.3.6.1.4.1.13400.2.20.*)
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
            "ip_address": {"type": "string", "title": "Device IP", "format": "ipv4"},
            "community": {"type": "string", "title": "Community String", "default": "public"},
            "snmp_version": {"type": "string", "title": "SNMP Version", "enum": ["1", "2c", "3"], "default": "2c"}
        },
        "required": ["ip_address"]
    }'
),

-- CCS 3-Phase Power Analyzer Panel (MQTT)
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

-- CCS HT Power Summary Panel (MQTT)
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

-- Production Line Breakdown Counter (Modbus)
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
            "unit_id":         {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
            "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
        },
        "required": ["unit_id"]
    }'
);
