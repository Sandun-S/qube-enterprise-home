# Reader Container Standard

> All reader containers (modbus-reader, snmp-reader, mqtt-reader, opcua-reader, http-reader) MUST follow this contract.

---

## Lifecycle

```
1. Container starts (created by Docker Swarm)
2. Read READER_ID from env var
3. Open SQLite database (read-only)
4. Load reader config + all sensors from SQLite
5. Close SQLite connection
6. Start protocol-specific polling/subscription loop
7. Send data to core-switch via HTTP POST /v3/batch
8. Run forever until container is stopped

Config changes:
  - Conf-agent writes new config to SQLite
  - Conf-agent calls: docker stop <container-name>
  - Swarm recreates the container
  - New container starts at step 1 with fresh data
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `READER_ID` | Yes | — | UUID of this reader in SQLite `readers` table |
| `SQLITE_PATH` | No | `/opt/qube/data/qube.db` | Path to SQLite database file |
| `CORESWITCH_URL` | No | `http://core-switch:8585` | Core-switch HTTP endpoint |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error` |

## SQLite Access Rules

- Open with `?mode=ro&_journal_mode=WAL` (read-only, WAL mode)
- Read config ONCE on startup
- Close connection immediately after loading
- NEVER write to SQLite from a reader

## Data Output Format

All readers send data to core-switch using `DataIn` struct:

```json
{
    "Table": "Measurements",
    "Equipment": "PM5100_Rack1",
    "Reading": "active_power_w",
    "Output": "influxdb",
    "Sender": "modbus-reader",
    "Tags": "name=PM5100_Rack1,location=rack1",
    "Time": 1711446600000000,
    "Value": "1250.5"
}
```

### Field Rules

| Field | Type | Rules |
|-------|------|-------|
| `Table` | string | From sensor's `table_name` in SQLite (e.g., "Measurements") |
| `Equipment` | string | Sensor name (from `sensors.name`) |
| `Reading` | string | Field key (from sensor's `config_json`, e.g., "active_power_w") |
| `Output` | string | From sensor's `output` field: `"influxdb"`, `"live"`, or `"influxdb,live"` |
| `Sender` | string | Reader name (e.g., "modbus-reader") |
| `Tags` | string | Comma-separated `key=value` pairs from `sensors.tags_json` |
| `Time` | int64 | **Unix MICROSECONDS** — `time.Now().UnixMicro()` |
| `Value` | string | **Always a string**, even for numbers |

### Batch Rules

- All records in a `/v3/batch` call should have the same `Equipment`
- Group readings by equipment before sending
- Use batch endpoint for efficiency (not single `/v3/data` per reading)

## Alert Format

When a reader can't connect to a device:

```json
POST /v3/alerts
{
    "Sender": "modbus-reader",
    "Message": "Cannot connect to 192.168.1.50:502 - connection refused",
    "Type": "connectivity",
    "Mode": 1
}
```

When connectivity is restored: send same alert with `"Mode": 0`.

## Two Reader Standards

### Standard A: Endpoint-Based (modbus_tcp, opcua, mqtt)

- One container per endpoint (IP:port or broker URL)
- Multiple sensors from that single endpoint
- Container connects to ONE target, reads many measurements

```
modbus-reader-panel-a  → connects to 192.168.1.50:502
  reads: sensor1 (registers 3000-3020), sensor2 (registers 100-110)

mqtt-reader-factory    → connects to broker.internal:1883
  subscribes: sensor1 (topic/a), sensor2 (topic/b)

lorawan-reader-ns      → connects to lorawan-ns:1700
  subscribes: sensor1 (dev-eui-1), sensor2 (dev-eui-2)

dnp3-reader-substation → connects to 10.0.5.10:20000
  polls: breaker1 (points 0-10), switch1 (points 20-30)
```

### Standard B: Multi-Target (snmp, http)

- ONE container per Qube for this protocol
- Handles ALL targets/IPs internally
- Each sensor has its own target address in `config_json`

```
snmp-reader → one container
  polls: 10.0.0.50 (sensor1), 10.0.0.51 (sensor2), 10.0.0.60 (sensor3)

http-reader → one container
  polls: https://api1.com/data (sensor1), https://api2.com/data (sensor2)

bacnet-reader → one container
  polls: 192.168.1.100 (sensor1), 192.168.1.101 (sensor2)
```

## Shared Go Module

Import the shared `pkg/` module in your reader's `go.mod`:

```go
require github.com/qube-enterprise/pkg v0.0.0
replace github.com/qube-enterprise/pkg => ../pkg
```

Use:
```go
import (
    "github.com/qube-enterprise/pkg/coreswitch"
    "github.com/qube-enterprise/pkg/sqliteconfig"
    "github.com/qube-enterprise/pkg/logger"
)
```

## Dockerfile Template

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY ../pkg ../pkg
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o reader .

FROM alpine:3.19
RUN apk add --no-cache sqlite-libs
COPY --from=builder /build/reader /app/reader
ENTRYPOINT ["/app/reader"]
```

Note: `CGO_ENABLED=1` is required for go-sqlite3 (C library).
