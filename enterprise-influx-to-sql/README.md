# enterprise-influx-to-sql v2

Edge telemetry bridge for Qube Enterprise. Polls InfluxDB v1 (written by core-switch), maps Equipment+Reading pairs to cloud sensor UUIDs, and POSTs batches to the Enterprise TP-API telemetry ingest endpoint.

This is the production replacement for the v1 `influx-to-sql` service on Enterprise Qubes. The v2 version sends telemetry to Postgres via the TP-API instead of writing directly to a SQL database.

---

## How It Works

```
InfluxDB v1 (edge buffer, written by core-switch)
  ↓  QueryTable — mean(value) GROUP BY time(1m), device, reading
RawRecord { Equipment, Reading, Value, Time }
  ↓  lookupSensor(SensorMap, equipment, reading)
Reading { SensorID, FieldKey, Value, Time }
  ↓  POST /v1/telemetry/ingest (batches of 1000)
Enterprise TP-API → PostgreSQL sensor_readings
```

---

## Sensor Map

The service needs a map from `"Equipment.Reading"` keys to cloud sensor UUIDs. It supports two sources, checked in priority order:

1. **SQLite `telemetry_settings` table** — when `SQLITE_PATH` is set (v2 preferred)
2. **`sensor_map.json` file** — fallback; written by conf-agent on config sync

SQLite example (written by conf-agent when cloud pushes sensor config):

```sql
SELECT device, reading, sensor_id FROM telemetry_settings;
-- PM5100.active_power_w → "uuid-sensor-1"
-- PM5100.voltage_ll_v   → "uuid-sensor-2"
```

JSON example (`/config/sensor_map.json`):

```json
{
  "PM5100.active_power_w": "uuid-sensor-1",
  "PM5100.voltage_ll_v":   "uuid-sensor-2",
  "PM5100":                "uuid-sensor-3"
}
```

Lookup order: `Equipment.Reading` first, then `Equipment` alone (for sensors with a single reading).

The sensor map is reloaded on every transfer cycle — no restart needed when conf-agent updates it.

---

## Configuration (Environment Variables)

All YAML values can be overridden by environment variables. Explicit env vars always win.

| Variable | Default | Description |
|----------|---------|-------------|
| `CONFIG_PATH` | `configs.yml` | Path to config file |
| `SQLITE_PATH` | *(unset)* | Edge SQLite DB path — enables SQLite sensor map |
| `TPAPI_URL` | `http://cloud-api:8081` | Enterprise TP-API endpoint |
| `QUBE_ID` | *(from config)* | Qube device ID |
| `QUBE_TOKEN` | *(from config)* | Qube HMAC token |
| `INFLUX_URL` | `http://influxdb:8086` | InfluxDB v1 endpoint |
| `INFLUX_DB` | `edgex` | InfluxDB database (must match core-switch) |
| `INFLUX_USER` | `root` | InfluxDB username |
| `INFLUX_PASS` | `root` | InfluxDB password |
| `SENSOR_MAP_PATH` | `/config/sensor_map.json` | JSON sensor map path (fallback) |
| `POLL_INTERVAL` | `60` | Seconds between transfer runs |
| `LOOKBACK_MINS` | `5` | Minutes of InfluxDB history to query each run |

---

## Config File (configs.yml)

```yaml
Service:
  PollInterval: 60
  LookbackMins: 5
  SensorMapPath: "/config/sensor_map.json"   # JSON fallback
  SQLitePath: ""                              # set to enable SQLite sensor map

InfluxDB:
  URL: "http://influxdb:8086"
  DB: "edgex"                   # must match INFLUX_DB set for core-switch
  User: "root"
  Pass: "root"
  Tables:
    - "Measurements"             # must match Table field used by readers

TPAPI:
  URL: "http://cloud-api:8081"
  QubeID: "Q-1001"
  QubeToken: ""                  # set via QUBE_TOKEN env after device claim
```

---

## TP-API Request Format

Each run POSTs to `POST /v1/telemetry/ingest` in batches of 1000:

```json
{
  "readings": [
    {
      "time":      "2024-03-26T10:00:00Z",
      "sensor_id": "uuid-sensor-1",
      "field_key": "active_power_w",
      "value":     1250.5,
      "unit":      ""
    }
  ]
}
```

Headers:

```
Content-Type:  application/json
X-Qube-ID:     Q-1001
Authorization: Bearer <QUBE_TOKEN>
```

---

## Build & Run

```bash
# Docker (copies ../pkg at build time)
docker build -t enterprise-influx-to-sql:dev .
docker run \
  -e INFLUX_URL=http://influxdb:8086 \
  -e INFLUX_DB=edgex \
  -e TPAPI_URL=http://cloud-api:8081 \
  -e QUBE_ID=Q-1001 \
  -e QUBE_TOKEN=your-token \
  enterprise-influx-to-sql:dev

# Local
go build -o enterprise-influx-to-sql .
INFLUX_URL=http://localhost:8086 TPAPI_URL=http://localhost:8081 QUBE_ID=Q-1001 QUBE_TOKEN=abc ./enterprise-influx-to-sql
```

---

## Module

```
module github.com/qube-enterprise/enterprise-influx-to-sql
```

Imports shared `pkg` module (`../pkg`) for SQLite access (`sqliteconfig.OpenReadOnly`, `sqliteconfig.LoadTelemetrySettings`). The `replace` directive in `go.mod` points to the local pkg directory at build time.

---

## File Structure

```
enterprise-influx-to-sql/
├── main.go             Entry point — load config, ping influx, run ticker loop
├── configs/
│   └── configs.go      Config loading from YAML + env var overrides
├── schema/
│   └── schema.go       Reading, RawRecord, SensorMap types
├── influxdb/
│   └── influx.go       InfluxDB v1 query (mean aggregation, GROUP BY device/reading)
├── tpapi/
│   └── tpapi.go        TP-API POST /v1/telemetry/ingest client
├── transfer/
│   └── transfer.go     ETL loop: load sensor_map → query → map → send
├── go.mod
├── configs.yml         Default config (all values overridable via env)
├── Dockerfile
└── README.md           This file
```

---

## Differences From v1 (influx-to-sql)

| Aspect | v1 | v2 |
|--------|----|----|
| Output | Direct SQL (PostgreSQL / MySQL) | TP-API → Postgres |
| Sensor mapping | `uploads.csv` (Device, AggFunc, ToTable, Tags) | `sensor_map.json` or SQLite `telemetry_settings` |
| Aggregation | Configurable per device (mean, sum, count, etc.) | Fixed `mean(value)` per 1-minute bucket |
| State | Stateful (tracks last uploaded timestamp per device) | Stateless (fixed lookback window, idempotent) |
| Multi-DB | MySQL + PostgreSQL | TP-API only |
| Dependencies | pq, go-sql-driver, robfig/cron | influxdb1-client, optional pkg/sqliteconfig |
