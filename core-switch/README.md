# Core-Switch v2

Edge data router for Qube Enterprise. Accepts `DataIn` payloads from all reader containers and routes to InfluxDB (edge buffer) and/or live forwarding (via conf-agent → cloud WebSocket).

MQTT output was removed in v2. Use `Output: "live"` for real-time cloud streaming.

---

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v3/batch` | Batch data ingestion (JSON array of DataIn) |
| POST | `/v3/data` | Single data point |
| POST | `/v3/alerts` | Alert from a reader or service |
| GET | `/metrics` | Prometheus metrics |

---

## DataIn Format

All readers send data using this struct:

```json
{
  "Table":     "Measurements",
  "Equipment": "PM5100_Rack1",
  "Reading":   "active_power_w",
  "Output":    "influxdb,live",
  "Sender":    "modbus-reader",
  "Tags":      "name=PM5100_Rack1,location=rack1",
  "Time":      1711446600000000,
  "Value":     "1250.5"
}
```

| Field | Type | Notes |
|-------|------|-------|
| `Table` | string | InfluxDB measurement name (e.g. `Measurements`) |
| `Equipment` | string | Device identifier — becomes `device` tag in InfluxDB |
| `Reading` | string | Metric name — becomes `reading` tag in InfluxDB |
| `Output` | string | Comma-separated routing: `influxdb`, `live`, or `influxdb,live` |
| `Sender` | string | Reader container name (for debugging) |
| `Tags` | string | Extra InfluxDB tags: `key=val,key2=val2` |
| `Time` | int64 | Unix **microseconds** (`time.Now().UnixMicro()`) |
| `Value` | string | Always a string, even for numbers |

---

## Output Routing

| Output value | Behaviour |
|-------------|-----------|
| `influxdb` | Write to InfluxDB v1 via line protocol (microsecond precision) |
| `live` | HTTP POST to conf-agent, forwarded to cloud via WebSocket |
| `influxdb,live` | Both simultaneously |

InfluxDB line protocol written by core-switch:

```
Measurements,device=PM5100_Rack1,reading=active_power_w,name=PM5100_Rack1 value=1250.5 1711446600000000
```

---

## Alert Format

`POST /v3/alerts`

```json
{
  "Sender":  "modbus-reader",
  "Message": "device unreachable: 192.168.1.10",
  "Type":    "connectivity",
  "Mode":    1
}
```

- `Type`: `connectivity`, `data`, or `other`
- `Mode`: `0` = resolved, `1` = active/complaining
- Duplicate alerts (same Sender+Message) are suppressed for `ALERTS_IGNORE_INTERVAL_SEC` seconds
- When live forwarding is enabled, alerts are also forwarded to conf-agent at `<CONF_AGENT_LIVE_URL>/alert`

---

## Configuration (Environment Variables)

No config file required. All settings via environment variables with sensible defaults.

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | `8585` | HTTP listen port |
| `INFLUX_URL` | `http://127.0.0.1:8086` | InfluxDB v1 endpoint |
| `INFLUX_DB` | `edgex` | InfluxDB database name |
| `INFLUX_USER` | `root` | InfluxDB username |
| `INFLUX_PASS` | `root` | InfluxDB password |
| `CONF_AGENT_LIVE_URL` | `http://enterprise-conf-agent:8585/v3/live` | Live relay endpoint on conf-agent |
| `ALERTS_IGNORE_INTERVAL_SEC` | `300` | Seconds between forwarding duplicate alerts |
| `SQLITE_PATH` | *(unset)* | Path to edge SQLite DB — enables dynamic settings override |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

---

## SQLite Dynamic Settings

When `SQLITE_PATH` is set, core-switch reads its output routing from the SQLite `coreswitch_settings` table on startup. This allows the cloud to push config changes without a manual restart:

```
key                value (JSON)
───────────────    ──────────────────────────────────────
outputs            {"influxdb": true, "live": false}
batch_size         100
flush_interval_ms  5000
```

**Update flow:**

```
Cloud API PUT /api/v1/qubes/{id}/coreswitch/settings
  → conf-agent receives via WebSocket
  → writes to SQLite coreswitch_settings table
  → Docker API stops core-switch container
  → Swarm recreates container
  → core-switch reads fresh SQLite on startup
```

If `SQLITE_PATH` is not set or the table does not exist, environment variable defaults apply.

---

## Prometheus Metrics

Available at `GET /metrics`.

| Metric | Description |
|--------|-------------|
| `data_points_total` | Total data points received (all outputs) |
| `data_points_influx` | Data points written to InfluxDB |
| `data_points_live` | Data points forwarded via live relay |

---

## Build & Run

```bash
# Docker (copies ../pkg at build time)
docker build -t core-switch:dev .
docker run -p 8585:8585 \
  -e INFLUX_URL=http://influxdb:8086 \
  -e INFLUX_DB=edgex \
  core-switch:dev

# Local
go build -o core-switch .
HTTP_PORT=8585 INFLUX_URL=http://localhost:8086 INFLUX_DB=edgex ./core-switch

# Test a batch POST
curl -s -X POST http://localhost:8585/v3/batch \
  -H "Content-Type: application/json" \
  -d '[{"Table":"Measurements","Equipment":"PM5100","Reading":"active_power_w","Output":"influxdb","Sender":"test","Tags":"","Time":1711446600000000,"Value":"1250.5"}]'
```

---

## Module

```
module github.com/qube-enterprise/core-switch
```

Imports shared `pkg` module (`../pkg`) for SQLite config loading (`sqliteconfig.OpenReadOnly`, `sqliteconfig.LoadCoreSwitchSettings`). The `replace` directive in `go.mod` points to the local pkg directory at build time.

---

## File Structure

```
core-switch/
├── main.go             Entry point — loads config, inits subsystems
├── configs/
│   └── config.go       Config loading from env vars + optional SQLite override
├── http/
│   └── http.go         HTTP endpoints, output routing, alert dedup, live forwarding
├── influx/
│   └── influx.go       InfluxDB v1 line protocol writer
├── schema/
│   └── schema.go       DataIn and Alert structs
├── go.mod
├── Dockerfile
└── README.md           This file
```
