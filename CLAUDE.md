# Qube Enterprise — Claude Code Guide

This file tells Claude Code everything it needs to know to work on this project.

## What this project is

Qube Enterprise is a cloud-to-edge IoT fleet management system. A **Qube** is a Raspberry Pi running Docker Swarm. The Enterprise platform adds automated sensor/gateway provisioning via a Cloud API, with a TP-API for device communication.

## Repository structure

```
cloud/                          Go backend (builds single binary)
  cmd/server/main.go            Starts Cloud API :8080 + TP-API :8081
  internal/api/                 Customer-facing REST API (JWT auth)
    router.go                   All routes
    auth.go                     Register/login
    gateways.go                 Gateway CRUD
    sensors.go                  Sensor CRUD + generateCSVRows()
    templates.go                Template CRUD + preview
    users.go                    User management
    protocols.go                GET /api/v1/protocols (public)
    registry.go                 Image registry config (superadmin)
  internal/tpapi/               Device-facing API (HMAC auth)
    router.go                   TP-API routes
    sync.go                     Config sync — generates docker-compose.yml + CSVs
      renderGatewayConfig()     Generates configs.yml per protocol
      renderGatewayFiles()      Generates config.csv per protocol
      buildFullComposeYML()     Generates full docker-compose.yml
  migrations/
    001_init.sql                orgs, users, qubes — pre-registers Q-1001..Q-1020
    002_gateways_sensors.sql    protocols, sensor_templates, gateways, sensors...
    003_device_catalog.sql      Seeded global templates (Schneider PM5100, UPS, etc.)

conf-agent/main.go              Edge agent: polls TP-API, deploys docker stack
mqtt-gateway/                   MQTT gateway container (arm64)
enterprise-influx-to-sql/       Reads InfluxDB, forwards to TP-API telemetry
test/test_api.sh                Full API test suite (197 tests)
test-ui/index.html              Browser-based dev console
docker-compose.dev.yml          Local dev stack (all services)
```

## How to run locally

```bash
# Start everything (rebuilds Go binary)
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d --build

# Run tests
./test/test_api.sh

# Check logs
docker compose -f docker-compose.dev.yml logs cloud-api -f
```

## Key architecture facts

**Protocols table drives everything** — adding a new protocol is a DB INSERT, no code change needed for routing/validation. The only code changes needed for new protocols are in:
1. `sync.go` → `renderGatewayConfig()` — add `case "new_protocol":`
2. `sync.go` → `renderGatewayFiles()` — add `case "new_protocol":`
3. `sensors.go` → `generateCSVRows()` — add `case "new_protocol":`

**Gateway ↔ Sensor relationship:**
- One gateway = one Docker container = one protocol+host connection
- Many sensors share one gateway (same protocol+host)
- SNMP exception: ONE gateway handles ALL SNMP devices (different IPs per sensor via addr_params.device_ip)
- Config hash changes whenever sensors/gateways are added → triggers conf-agent sync

**CSV formats per protocol (must match exactly):**

Modbus `config.csv`:
```
#Section,Equipment,Reading,RegType,Address,type,Output
Measurements,Main_Meter,active_power_w,Holding,3000,float32,influxdb
```

OPC-UA `config.csv`:
```
#Table,Device,Reading,OpcNode,Type,Freq,Output,Tags
Measurements,Sensor_A,active_power_w,ns=2;points/ActivePower,float,10,influxdb,name=x
```

SNMP `config.csv` (devices.csv):
```
#Table, Device, SNMP csv, Community, Version, Output, Tags
snmp_data,192.168.1.200,gxt-rt-ups.csv,public,2c,influxdb,name=UPS_Room1
```
SNMP also gets `maps/` folder with `field_key,OID` files (no header).

**configs.yml formats per protocol:**

Modbus:
```yaml
loglevel: "info"
modbus:
  server: "tcp://host:port"
  readingsfile: "config.csv"
  freqsec: 20
  singlereadcount: 120
http:
  data: "http://core-switch:8585/batch"
  alerts: "http://core-switch:8585/alerts"
```

SNMP:
```yaml
loglevel: "INFO"
snmp:
  fetch_interval: 15
  connect_timeout: 10
  worker_count: 2
  devices_file: "config.csv"
  maps_folder: "./maps"
http:
  data_url: "http://core-switch:8585/v3/batch"
  alerts_url: "http://core-switch:8585/v3/alerts"
```

## Auth

- Cloud API: JWT Bearer token. `ctxUserID`, `ctxOrgID`, `ctxRole` set by middleware.
- TP-API: HMAC using `QUBE_TOKEN`. `ctxQubeID` set by middleware.
- Superadmin: `iotteam@internal.local` / `iotteam2024`
- Dev Qubes: pre-registered Q-1001..Q-1020 with keys `TEST-Q1001-REG` etc.

## Common tasks

**Fix a failing test:**
```bash
./test/test_api.sh 2>&1 | grep -A3 "✗"
```
Then read the relevant handler in `cloud/internal/api/` and fix.

**Add a field to a response:**
Find the handler in `cloud/internal/api/`, update the `writeJSON()` call. No DTO structs — responses are `map[string]any`.

**Add a new endpoint:**
1. Write handler function in relevant file
2. Register in `cloud/internal/api/router.go`
3. Add test in `test/test_api.sh`
4. Add curl example in `TESTING.md`

**Change CSV generation:**
Edit `sensors.go` → `generateCSVRows()` for row data.
Edit `sync.go` → `renderGatewayFiles()` for file format.
Edit `sync.go` → `renderGatewayConfig()` for configs.yml format.

**Add a global template:**
Use `SUPER_TOKEN` and `POST /api/v1/templates` — superadmin-created templates auto-set `is_global=true`.

## Test accounts after fresh start

```
superadmin:  iotteam@internal.local / iotteam2024
dev qubes:   Q-1001 (register key: TEST-Q1001-REG)
             Q-1002 (register key: TEST-Q1002-REG)
             ... up to Q-1020
```

## Ports

```
8080  Cloud API (JWT)
8081  TP-API (HMAC)
8888  Test UI (nginx serving test-ui/index.html)
5432  PostgreSQL
8086  InfluxDB
1883  MQTT (mosquitto)
```

## Go module

`github.com/qube-enterprise/cloud` — all internal packages use this path.

## Known patterns

- `writeJSON(w, status, data)` — all JSON responses
- `writeError(w, status, "message")` — all error responses  
- `pool.QueryRow(ctx, sql, args...).Scan(&vars...)` — single row queries
- `pool.Query(ctx, sql, args...)` — multi-row queries, always `defer rows.Close()`
- `r.Context().Value(ctxUserID).(string)` — get user from JWT context
- `chi.URLParam(r, "param_name")` — URL path params
