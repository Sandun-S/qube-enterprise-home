# Core-Switch Data Format Reference

> This documents the exact data format between readers and core-switch.
> This format is unchanged from v1 (backward compatible with existing gateways).

---

## DataIn Struct

```go
// From core-switch/schema/schema.go
type DataIn struct {
    Table     string  // InfluxDB measurement/table name
    Equipment string  // Source device/sensor name
    Reading   string  // Metric/field key name
    Output    string  // Where to route: "influxdb", "live", "influxdb,live"
    Sender    string  // Which reader sent this (e.g., "modbus-reader")
    Tags      string  // Comma-separated: key1=val1,key2=val2
    Time      int64   // Unix MICROSECONDS
    Value     string  // Always string (even for numbers)
}
```

## JSON Example

```json
[
    {
        "Table": "Measurements",
        "Equipment": "PM5100_Rack1",
        "Reading": "active_power_w",
        "Output": "influxdb",
        "Sender": "modbus-reader",
        "Tags": "name=PM5100_Rack1,location=rack1",
        "Time": 1711446600000000,
        "Value": "1250.5"
    },
    {
        "Table": "Measurements",
        "Equipment": "PM5100_Rack1",
        "Reading": "voltage_ll_v",
        "Output": "influxdb",
        "Sender": "modbus-reader",
        "Tags": "name=PM5100_Rack1,location=rack1",
        "Time": 1711446600000000,
        "Value": "230.1"
    }
]
```

## HTTP Endpoints

| Endpoint | Method | Body | Use |
|----------|--------|------|-----|
| `/v3/batch` | POST | `[]DataIn` (JSON array) | Batch of readings (preferred) |
| `/v3/data` | POST | `DataIn` (single JSON object) | Single reading |
| `/v3/alerts` | POST | `Alert` (JSON object) | Connectivity alerts |

## Output Routing (v2)

The `Output` field controls where core-switch sends the data:

| Output Value | Behavior |
|-------------|----------|
| `"influxdb"` | Write to InfluxDB v1 (edge buffer) |
| `"live"` | Forward via WebSocket to cloud (real-time dashboards) |
| `"influxdb,live"` | Both — buffer AND stream live |

### v1 → v2 Change

| v1 | v2 | Notes |
|----|----|----|
| `"influxdb"` | `"influxdb"` | Unchanged |
| `"mqtt"` | **Removed** | No internal MQTT broker on Qube |
| `"mqtt,influxdb"` | `"influxdb,live"` | New: live WebSocket replaces MQTT |
| — | `"live"` | New: real-time only (no edge buffer) |

## Alert Format

```json
{
    "Sender": "modbus-reader",
    "Message": "Cannot connect to 192.168.1.50:502 - connection refused",
    "Type": "connectivity",
    "Mode": 1
}
```

| Mode | Meaning |
|------|---------|
| `1` | Problem active (complaining) |
| `0` | Problem resolved |

## Timestamp Rules

- Use `time.Now().UnixMicro()` (Go)
- This gives Unix timestamp in **microseconds** (not seconds, not nanoseconds)
- Example: `1711446600000000` = 2024-03-26T10:30:00Z in microseconds
- Core-switch converts to InfluxDB precision automatically

## Tags Format

- Comma-separated `key=value` pairs
- No spaces in values (use underscores)
- Example: `"name=PM5100_Rack1,location=rack1,floor=2"`
- Tags become InfluxDB tags (indexed, queryable)

## Batch Rules

- All records in a `/v3/batch` call should share the same `Equipment`
- Different `Reading` values within the same batch are fine
- Different `Table` values within the same batch are fine
- Group by Equipment before sending for best performance
