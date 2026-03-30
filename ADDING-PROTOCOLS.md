# Adding New Protocols to Qube Enterprise v2

This guide covers adding a new reader protocol (e.g. LoRaWAN, BACnet, Wialon).
In v2, protocols are database-driven. Most of the work is in the reader container binary itself.

---

## Overview

| Step | What | Where | Code change? |
|------|------|-------|-------------|
| 1 | Register protocol | `protocols` table | SQL only |
| 2 | Create reader template | `reader_templates` table | SQL only |
| 3 | Build reader container | New Go binary | Yes |
| 4 | Push to registry | Docker registry | Build/push |

---

## Step 1: Register the protocol

```sql
INSERT INTO protocols (id, label, description, reader_standard) VALUES
(
    'lorawan',
    'LoRaWAN',
    'Long-range, low-power IoT devices via LoRaWAN network server',
    'endpoint'   -- or 'multi_target' if one container handles many devices
);
```

`reader_standard` values:
- `endpoint` — one reader per network endpoint (like Modbus, OPC-UA, MQTT)
- `multi_target` — one reader handles all targets (like SNMP, HTTP)

The UI renders this protocol automatically in dropdowns — no frontend code change needed.

---

## Step 2: Create a reader template

```sql
INSERT INTO reader_templates (
    protocol,
    name,
    description,
    image_suffix,
    connection_schema,
    env_defaults
) VALUES (
    'lorawan',
    'LoRaWAN NS Reader',
    'Connects to a LoRaWAN Network Server (Chirpstack, TTN) and subscribes to device uplinks',
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
);
```

`image_suffix` is the tail of the container image name. With `QUBE_IMAGE_REGISTRY=ghcr.io/foo/bar`
the full image becomes `ghcr.io/foo/bar/lorawan-reader:arm64.latest`.

`connection_schema` is a JSON Schema that the UI renders as a form when the user creates
a reader of this protocol. The submitted values become `reader.config_json`.

---

## Step 3: Create the reader container binary

The reader must implement the **Reader Standard** (see `standards/READER_STANDARD.md`).

### Minimum structure

```
lorawan-reader/
├── main.go
├── Dockerfile
└── go.mod
```

### main.go skeleton

```go
package main

import (
    "log"
    "os"
    "time"

    "github.com/your-org/qube-enterprise/pkg/sqlitedb"
    "github.com/your-org/qube-enterprise/pkg/coreswitchclient"
)

func main() {
    readerID := os.Getenv("READER_ID")        // injected by conf-agent
    sqlitePath := os.Getenv("SQLITE_PATH")    // /opt/qube/data/qube.db
    csURL := os.Getenv("CORESWITCH_URL")      // http://core-switch:8585

    db, err := sqlitedb.OpenReadOnly(sqlitePath)
    if err != nil {
        log.Fatalf("open sqlite: %v", err)
    }
    defer db.Close()

    // Load reader config (connection params from cloud portal)
    cfg, err := db.LoadReaderConfig(readerID)
    if err != nil {
        log.Fatalf("load reader config: %v", err)
    }

    // Load sensors for this reader
    sensors, err := db.LoadSensors(readerID)
    if err != nil {
        log.Fatalf("load sensors: %v", err)
    }

    cs := coreswitchclient.New(csURL)

    // Main loop — connect to LoRaWAN NS and process uplinks
    for {
        readings, err := pollLoRaWAN(cfg, sensors)
        if err != nil {
            log.Printf("poll error: %v", err)
            time.Sleep(10 * time.Second)
            continue
        }
        if err := cs.SendBatch(readings); err != nil {
            log.Printf("coreswitch send error: %v", err)
        }
    }
}
```

### Core-switch batch format

Each reading sent to core-switch:

```go
coreswitchclient.DataIn{
    Table:     sensor.TableName,        // "Measurements"
    Equipment: sensor.Name,             // "Dev-001"
    Reading:   "rssi",                  // field name
    Output:    sensor.Output,           // "influxdb" | "live" | "influxdb,live"
    Sender:    "lorawan-reader",
    Tags:      sensor.Tags,
    Time:      time.Now().UnixMicro(),
    Value:     "-85",                   // always string
}
```

### Dockerfile

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lorawan-reader .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/lorawan-reader .
CMD ["./lorawan-reader"]
```

---

## Step 4: Add to CI/CD

Add a build entry in `.github/workflows/build-push.yml`:

```yaml
- name: Build lorawan-reader (arm64)
  uses: docker/build-push-action@v5
  with:
    context: ./lorawan-reader
    platforms: linux/arm64
    push: true
    tags: ghcr.io/sandun-s/qube-enterprise-home/lorawan-reader:arm64.latest
```

---

## What happens automatically after these steps

1. The new protocol appears in `GET /api/v1/protocols`
2. Users can create a LoRaWAN reader via `POST /api/v1/qubes/:id/readers`
3. The connection_schema renders as a form in the test UI
4. conf-agent receives a config push → updates SQLite → deploys the reader container
5. The reader container starts, reads config from SQLite, and begins polling

---

## Device template (optional)

If the new protocol has well-known sensors, add a global device template:

```sql
INSERT INTO device_templates (org_id, protocol, name, manufacturer, model,
    sensor_config, sensor_params_schema, is_global) VALUES
(
    NULL,        -- NULL = global (superadmin)
    'lorawan',
    'Dragino LHT65 Temp/Humidity',
    'Dragino',
    'LHT65',
    '{"readings": [
        {"name": "temperature_c", "field": "TempC_SHT", "unit": "°C"},
        {"name": "humidity_pct",  "field": "Hum_SHT",   "unit": "%"}
    ]}',
    '{"type":"object","properties":{"dev_eui":{"type":"string","title":"Device EUI"}}}',
    TRUE
);
```

Users select this template when adding a sensor — the `sensor_config` is merged with their
`params` to build the final `sensor.config_json` that the reader sees in SQLite.

---

## One optional code change — PATCH sensor_config support

The `PATCH /api/v1/device-templates/{id}/config` endpoint supports fine-grained
add/update/delete actions on individual entries in `sensor_config`. It uses
`protocolArrayKey()` in `cloud/internal/api/templates.go` to find the right array
key for the protocol.

If you want the action-based mode to work for your new protocol, add a case:

```go
// cloud/internal/api/templates.go
func protocolArrayKey(protocol string) string {
    switch protocol {
    case "modbus_tcp": return "registers"
    case "opcua":      return "nodes"
    case "snmp":       return "oids"
    case "mqtt":       return "json_paths"
    case "http":       return "json_paths"
    case "lorawan":    return "readings"   // ← add your protocol here
    default:           return "entries"
    }
}
```

If you skip this, the full-replacement mode (`{"sensor_config": {...}}`) still works
for any protocol — only the action-based mode falls back to `"entries"` as the key.

---

## No code changes required for

- Protocol dropdown in UI
- API validation (protocol existence checked via `protocols` table)
- Config hash computation
- Docker compose generation (containers table drives this)
- TP-API sync (readers/sensors are protocol-agnostic JSON)
- Telemetry pipeline (sensor_id is all that matters)
