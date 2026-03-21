# 07 — CSV Generation: Templates to Real Gateway Config Files

This is the core of the automation. A user picks "Schneider PM5100" from a list and enters an IP address. The system generates the correct CSV files automatically. Here's how.

---

## The three levels

```
sensor_templates (definition — what registers does this device have?)
    ↓
sensors (instance — apply this template to this gateway with these params)
    ↓
service_csv_rows (expanded rows — one row per register, stored in Postgres)
    ↓
config.csv (file on Qube filesystem — what the gateway binary reads)
```

---

## Level 1: Template config_json

The template stores the register map as JSON. For the Schneider PM5100:

```json
{
  "registers": [
    {
      "address": 3000,
      "register_type": "Holding",
      "data_type": "uint16",
      "count": 1,
      "scale": 0.1,
      "field_key": "active_power_w",
      "table": "Measurements"
    },
    {
      "address": 3020,
      "register_type": "Holding",
      "data_type": "uint16",
      "count": 1,
      "scale": 0.1,
      "field_key": "voltage_ll_v",
      "table": "Measurements"
    }
  ]
}
```

This is created once by the IoT team and reused for every Schneider PM5100 in every customer installation.

---

## Level 2: generateCSVRows() — template → rows

When a user adds a sensor, this function reads the template and creates one row per register:

```go
// cloud/internal/api/sensors.go
func generateCSVRows(
    sensorID, sensorName, protocol string,
    tmplCfgRaw []byte,     // template's config_json
    addrParams, tagsJSON any,
    gwCfgRaw []byte,
    gwHost string, gwPort int,
) ([]map[string]any, string, error) {

    // Parse the template config
    var tmplCfg map[string]any
    json.Unmarshal(tmplCfgRaw, &tmplCfg)

    // Parse address_params (user-provided: {"unit_id": 1, "register_offset": 0})
    ap, _ := addrParams.(map[string]any)
    if ap == nil { ap = map[string]any{} }

    switch protocol {
    case "modbus_tcp":
        registers, _ := tmplCfg["registers"].([]any)
        offset := toInt(ap["register_offset"], 0)  // optional offset for multi-device
        
        rows := make([]map[string]any, 0, len(registers))
        for _, r := range registers {
            reg := r.(map[string]any)
            addr := toInt(reg["address"], 0) + offset  // apply offset
            
            rows = append(rows, map[string]any{
                // Must match EXACTLY what modbus-gateway reads:
                // #Equipment,Reading,RegType,Address,type,Output,Table,Tags
                "Equipment": sensorName,              // "Main_Meter"
                "Reading":   reg["field_key"],        // "active_power_w"
                "RegType":   reg["register_type"],    // "Holding"
                "Address":   addr,                    // 3000
                "Type":      reg["data_type"],        // "uint16"
                "Output":    "influxdb",
                "Table":     reg["table"],            // "Measurements"
                "Tags":      flattenTags(tags),       // "location=panel_a"
            })
        }
        return rows, "registers", nil
    }
}
```

The `register_offset` is key. If you have 5 identical meters at unit IDs 1-5, they all use the same template but with different `unit_id` values in `address_params`. The offset shifts all register addresses (useful for some devices that use different base addresses per unit).

---

## Level 3: renderGatewayFiles() — rows → CSV text

When the Qube polls `GET /v1/sync/config`, the TP-API calls this function to turn Postgres rows into actual file content:

```go
// cloud/internal/tpapi/sync.go
func renderGatewayFiles(protocol, svcName string, rows []csvEntry) (string, map[string]string) {
    var b bytes.Buffer
    extraFiles := map[string]string{}

    switch protocol {
    case "modbus_tcp":
        // Header line with # — modbus-gateway skips lines starting with #
        b.WriteString("#Equipment,Reading,RegType,Address,type,Output,Table,Tags\n")
        for _, r := range rows {
            fmt.Fprintf(&b, "%s,%s,%s,%v,%s,%s,%s,%s\n",
                sv(r.Data, "Equipment"),  // "Main_Meter"
                sv(r.Data, "Reading"),    // "active_power_w"
                sv(r.Data, "RegType"),    // "Holding"
                r.Data["Address"],        // 3000
                sv(r.Data, "Type"),       // "uint16"
                sv(r.Data, "Output"),     // "influxdb"
                sv(r.Data, "Table"),      // "Measurements"
                sv(r.Data, "Tags"))       // "location=panel_a"
        }
    }
    return b.String(), extraFiles
}
```

The result is a file like this:
```
#Equipment,Reading,RegType,Address,type,Output,Table,Tags
Main_Meter,active_power_w,Holding,3000,uint16,influxdb,Measurements,location=panel_a
Main_Meter,voltage_ll_v,Holding,3020,uint16,influxdb,Measurements,location=panel_a
Main_Meter,current_a,Holding,3054,uint16,influxdb,Measurements,location=panel_a
```

This exact format is what the real `modbus-gateway` binary expects. If a column is missing or in the wrong order, the gateway won't read the data.

---

## Different CSV formats per protocol

| Protocol | File name | Format |
|---|---|---|
| Modbus TCP | `config.csv` | `Equipment,Reading,RegType,Address,type,Output,Table,Tags` |
| OPC-UA | `config.csv` | `Table,Device,Reading,OpcNode,Type,Freq,Output,Tags` |
| SNMP | `config.csv` | `Table,Device,SNMP_csv,Community,Version,Output,Tags` + per-device OID files |
| MQTT | `config.csv` | YAML format (not CSV) — `mapping.yml` style |

SNMP is interesting — each device gets two files:
1. `config.csv` — one row per device listing the community string and which OID file to use
2. `UPS_Main.csv` — the actual OID list for that device

```go
case "snmp":
    b.WriteString("#Table,Device,SNMP_csv,Community,Version,Output,Tags\n")
    
    written := map[string]bool{}
    for _, r := range rows {
        deviceKey := sv(r.Data, "Device")
        if !written[deviceKey] {
            written[deviceKey] = true
            snmpFile := sv(r.Data, "SNMP_csv")  // "UPS_Main.csv"
            
            // Main devices.csv row
            fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n", ...)
            
            // Write separate OID file
            if oids, ok := r.Data["_oids"].([]any); ok {
                var oidBuf bytes.Buffer
                oidBuf.WriteString("#OID,FieldKey,Type\n")
                for _, o := range oids {
                    om := o.(map[string]any)
                    fmt.Fprintf(&oidBuf, "%s,%s,%s\n",
                        sv(om, "oid"),
                        sv(om, "field_key"),
                        sv(om, "type"))
                }
                // Store as extra file to be written separately
                extraFiles["configs/"+svcName+"/"+snmpFile] = oidBuf.String()
            }
        }
    }
```

---

## The configs.yml file

Each gateway also gets a `configs.yml` — the connection settings the gateway binary reads at startup:

```go
// For Modbus TCP gateway
func renderGatewayConfig(svc svcMeta) string {
    server := fmt.Sprintf("modbus-tcp://%s:%d", svc.Host, svc.GwPort)
    return fmt.Sprintf(`LogLevel: "Info"
Modbus:
  Server: "%s"
  ReadingsFile: "config.csv"
  FreqSec: %d
HTTP:
  Data: "http://core-switch:8080/v3/batch"
  Alerts: "http://core-switch:8080/v3/alerts"
`, server, freqSec)
}
```

The gateway reads `configs.yml` for WHERE to connect (the device IP), and reads `config.csv` for WHAT to read (the registers). Both files are mounted as read-only volumes in the Docker container.

---

## How to fix a wrong register address

If you discover a register address was wrong (e.g. `active_power_w` is at 3001 not 3000):

```bash
# 1. List current rows for the sensor
GET /api/v1/sensors/{sensor_id}/rows
# Returns: [{id, csv_type, row_data, row_order}, ...]

# 2. Find the row with Reading=active_power_w, note its id

# 3. Fix just that row
PUT /api/v1/sensors/{sensor_id}/rows/{row_id}
Body: {
  "row_data": {
    "Equipment": "Main_Meter",
    "Reading": "active_power_w",
    "RegType": "Holding",
    "Address": 3001,   ← fixed
    "Type": "uint16",
    "Output": "influxdb",
    "Table": "Measurements",
    "Tags": "location=panel_a"
  }
}
```

The response includes `new_hash` — the config hash has been recalculated. Within 30 seconds, the Qube detects the hash change, downloads the new config, and writes the corrected CSV.

---

## What about the template fix vs sensor row fix?

**Fix the template** when ALL sensors of this device type have the wrong address. Use `PATCH /api/v1/templates/{id}/registers` to update the template, then re-add any sensors (or they'll keep the old rows until deleted and re-added).

**Fix the sensor row** when just ONE specific sensor has a non-standard address. The template stays correct for other sensors.
