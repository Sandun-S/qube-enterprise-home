# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Qube Enterprise** is a cloud-to-edge IoT fleet management system. A **Qube** is a Raspberry Pi/Kadas edge device running Docker Swarm with protocol gateways (Modbus TCP, OPC-UA, SNMP, MQTT). The Enterprise layer adds automated gateway/sensor provisioning via a Cloud API and TP-API for device communication, with telemetry flowing to cloud Postgres.

**Technology Stack:**
- Go 1.22 (all backend services)
- PostgreSQL (enterprise data)
- InfluxDB v1 (edge data buffer)
- Docker + Docker Swarm (deployment)
- JWT + HMAC (authentication)

## Repository Structure

```
qube-enterprise/
├── cloud/                          # Cloud API + TP-API (single Go binary)
│   ├── cmd/server/main.go          # Entry point — starts :8080 and :8081
│   ├── internal/api/               # Cloud Management API (JWT, port 8080)
│   │   ├── auth.go                 # Register / login
│   │   ├── qubes.go                # Qube CRUD + claim by register_key
│   │   ├── gateways.go             # Gateway CRUD + auto service creation
│   │   ├── sensors.go              # Sensor CRUD + CSV row generation
│   │   ├── templates.go            # Device catalog CRUD + register patch
│   │   ├── telemetry.go            # Telemetry query endpoints
│   │   ├── hash.go                 # Config hash recomputation
│   │   ├── commands.go             # Remote command dispatch
│   │   ├── middleware.go           # JWT + RBAC
│   │   └── router.go               # All route registration
│   ├── internal/tpapi/             # TP-API (HMAC, port 8081) — Qube-facing only
│   │   ├── router.go               # Routes + HMAC middleware
│   │   ├── sync.go                 # sync/state, sync/config, device/register
│   │   ├── telemetry.go            # telemetry/ingest
│   │   └── commands.go             # commands/poll, commands/:id/ack
│   └── migrations/
│       ├── 001_init.sql            # Core schema + test device seeds
│       ├── 002_gateways_sensors.sql # Gateways, sensors, templates, readings
│       └── 003_device_catalog.sql  # Global device templates (Schneider, UPS, etc.)
│
├── conf-agent/                     # Edge agent — runs on every Qube
│   └── main.go                     # Self-registers, hash sync, docker stack deploy
│
├── enterprise-influx-to-sql/       # Edge telemetry bridge — reads InfluxDB → TP-API
│   ├── main.go
│   └── configs.yml
│
├── mqtt-gateway/                   # Enterprise MQTT gateway container
│   └── main.go
│
├── scripts/
│   ├── setup-cloud.sh              # Provision cloud VM
│   ├── setup-qube.sh               # Provision qube VM
│   └── write-to-database.sh        # Flash-time script — inserts into both DBs
│
├── test/
│   ├── test_api.sh                 # Full API test suite (197 tests)
│   ├── mit.txt                     # Dev fake /boot/mit.txt for compose testing
│   └── mosquitto/mosquitto.conf
│
├── test-ui/index.html              # Browser dev console (http://localhost:8888)
├── docker-compose.dev.yml          # Local dev stack (all services)
├── ARCHITECTURE.md                 # Detailed architecture & system design
├── DEPLOYMENT.md                   # Production deployment guide
├── TESTING.md                      # Manual testing scenarios
├── ADDING-PROTOCOLS.md             # Complete guide for new protocol implementation
└── README.md                       # Project overview & quick start
```

## Quick Start

### Run locally (Docker Compose)

```bash
# Start full stack (rebuilds images)
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d --build

# Access services
# Cloud API:      http://localhost:8080
# TP-API:         http://localhost:8081
# Test UI:        http://localhost:8888
# Postgres:       localhost:5432
# InfluxDB:       localhost:8086
# MQTT:           localhost:1883

# Check health
curl -s http://localhost:8080/health | jq .
curl -s http://localhost:8081/health | jq .

# Run full test suite (197 tests)
./test/test_api.sh

# View logs
docker compose -f docker-compose.dev.yml logs -f cloud-api
docker compose -f docker-compose.dev.yml logs -f conf-agent
docker compose -f docker-compose.dev.yml logs -f enterprise-influx-to-sql

# Stop everything
docker compose -f docker-compose.dev.yml down
```

### Build a single service (Go)

```bash
# Cloud API (builds and runs locally outside Docker)
cd cloud
go build -o cloud-api ./cmd/server
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable \
JWT_SECRET=dev-jwt-secret-change-in-production \
./cloud-api

# Or with docker build
docker build -f cloud/Dockerfile -t cloud-api:dev ./cloud
```

## Architecture — The Data Flow

```
USER → Cloud API (:8080, JWT)
  ↓ creates/updates
gateways + sensors + templates (PostgreSQL)
  ↓ config hash changes
conf-agent (on Qube) polls TP-API /v1/sync/state
  ↓ hash mismatch
Downloads: docker-compose.yml + CSV files + sensor_map.json
  ↓ docker stack deploy
Gateway containers start (Modbus/MQTT/SNMP/OPC-UA)
  ↓ poll devices
POST to core-switch :8080/v3/batch
  ↓ writes to
InfluxDB v1 (edgex DB)
  ↓ enterprise-influx-to-sql polls
Maps via sensor_map.json → sensor UUIDs
  ↓ POST
TP-API /v1/telemetry/ingest (HMAC)
  ↓ writes to
Postgres sensor_readings
  ↓ queried by
Cloud API /api/v1/data/readings → User
```

**Key Design Decisions:**
- **InfluxDB v1** used to match existing core-switch (NOT v2)
- **sensor_map.json** bridges InfluxDB measurement names to sensor UUIDs
- **Dual auth systems:** JWT for users (8080), HMAC for devices (8081)
- **Zero-touch provisioning:** conf-agent auto-deploys from hash sync
- **Protocol-driven:** `protocols` table defines UI schemas and routing

## Ports

| Port | Service | Auth | Usage |
|------|---------|------|-------|
| 8080 | Cloud API | JWT Bearer | Frontend, developers |
| 8081 | TP-API | HMAC-SHA256 | Qube devices only |
| 5432 | PostgreSQL | password | Cloud API internal |
| 8086 | InfluxDB v1 | none | Edge data buffer |
| 1883 | MQTT (mosquitto) | none | Message broker |
| 8888 | Test UI | none | Browser dev console |

## Authentication & Authorization

**Cloud API (port 8080):**
- JWT tokens via `Authorization: Bearer <token>`
- Issued by `POST /api/v1/auth/login` or `POST /api/v1/auth/register`
- Roles: `superadmin`, `admin`, `editor`, `viewer`
- Context values: `ctxUserID`, `ctxOrgID`, `ctxRole` (from middleware)

**TP-API (port 8081):**
- HMAC-SHA256 signature in `X-HMAC-Signature` header
- Base string: `QUBE_ID:QUBE_TOKEN`
- Token obtained via device self-registration: `POST /v1/device/register`
- Context value: `ctxQubeID`

**Dev test accounts:**
- Superadmin: `iotteam@internal.local` / `iotteam2024`
- Pre-registered Qubes: Q-1001..Q-1020 with keys `TEST-Q1001-REG` etc.

## Common Development Tasks

### Run a single API test

```bash
# The test suite is a single shell script with 197 assertions
# To run specific tests, comment out sections in test/test_api.sh
# Or filter output:

./test/test_api.sh 2>&1 | grep -A3 "✗"  # Show failures
./test/test_api.sh 2>&1 | grep "PASS"   # Show all passes
```

### Debug conf-agent sync

```bash
# View conf-agent logs (simulated Qube in docker-compose.dev.yml)
docker compose -f docker-compose.dev.yml logs -f conf-agent

# conf-agent does:
# 1. Reads /boot/mit.txt → device_id, register_key
# 2. POST /v1/device/register → gets QUBE_TOKEN
# 3. Polls /v1/sync/state every 30s (hash compare)
# 4. On mismatch, GET /v1/sync/config → downloads compose + CSVs
# 5. Runs: docker stack deploy -c /opt/qube/docker-compose.yml qube
```

### Simulate sensor data in dev

```bash
# Send test data to InfluxDB (what gateway containers would produce)
curl -X POST http://localhost:8086/write?db=qube-db&precision=s \
  --data-binary 'Measurements,device=Main_Meter,reading=active_power_w value=1250.5'

# Or use the influx-seeder profile:
docker compose -f docker-compose.dev.yml run --rm influx-seeder

# Then check enterprise-influx-to-sql logs to see telemetry forwarded
```

### Add a new endpoint

1. Write handler in `cloud/internal/api/` or `cloud/internal/tpapi/`
2. Register in `cloud/internal/api/router.go` (with middleware) or `tpapi/router.go` (with HMAC)
3. Add test to `test/test_api.sh`
4. Add curl example to `TESTING.md` if user-facing
5. Responses: use `writeJSON(w, status, map[string]any{...})` or `writeError(w, status, msg)`

### Add a field to a response

Find the handler in `cloud/internal/api/`, update the map passed to `writeJSON()`. No DTO structs — all responses are `map[string]any`.

### Change CSV generation for a protocol

Three places to edit:

1. **CSV row structure** — `cloud/internal/api/sensors.go` → `generateCSVRows()` case
2. **CSV file format** — `cloud/internal/tpapi/sync.go` → `renderGatewayFiles()` case
3. **configs.yml format** — `cloud/internal/tpapi/sync.go` → `renderGatewayConfig()` case

See `ADDING-PROTOCOLS.md` for complete protocol addition workflow.

### Add a global template (device catalog)

Use superadmin token and `POST /api/v1/templates` with `"is_global": true`. Or seed in migration `003_device_catalog.sql`. Templates are protocol-specific and define `config_json` structure that `generateCSVRows()` uses.

### Reset the database (dev only)

```bash
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d postgres
# Migrations auto-run on postgres container init
```

## Protocol Implementation Patterns

The system supports 4 protocols (modbus_tcp, opcua, snmp, mqtt). Each has:

- **configs.yml**: Connection settings for the gateway container
- **config.csv**: What to poll/subscribe (format varies by protocol)
- **maps/ folder**: Only for SNMP (OID → field_key mapping)

CSV generation is protocol-specific in `generateCSVRows()` (sensors.go). The `csv_type` returned determines which case in `renderGatewayFiles()` writes the file.

**Key differences:**

| Property | modbus_tcp | opcua | snmp | mqtt |
|----------|-----------|-------|------|------|
| Gateway matches by | protocol+host | protocol+host | protocol only | protocol+host |
| Config file name | config.csv | config.csv | config.csv | config.csv |
| Extra files | none | none | maps/*.csv | none |
| Device IP location | gateway.host | gateway.host | sensor addr_params | gateway.host |
| Credentials | none | none | community (sensor) | username/password (gw) |

See `ADDING-PROTOCOLS.md` for step-by-step guide to add a new protocol (LoRaWAN, etc.).

## Database Schema Highlights

**Enterprise Postgres (cloud-api):**
- `organisations`, `users` — multi-tenant
- `qubes` — devices with HMAC token
- `gateways` — one per container (protocol + host + port)
- `sensors` — linked to gateway + template
- `sensor_templates` — device catalog with `config_json`
- `service_csv_rows` — generated CSV rows per sensor
- `sensor_readings` — time-series telemetry
- `config_state` — hash tracking for sync

**Query pattern:**
```go
pool.QueryRow(ctx, `SELECT id FROM sensors WHERE gateway_id=$1 AND name=$2`, gwID, name).Scan(&id)
pool.Query(ctx, `SELECT * FROM sensor_readings WHERE sensor_id=$1 ORDER BY ts DESC LIMIT $2`, sensorID, 100)
```

## Key Code Patterns

- **Responses:** `writeJSON(w, http.StatusOK, data)` or `writeError(w, http.StatusBadRequest, "msg")`
- **DB access:** `pool.QueryRow(ctx, sql, args...).Scan(&vars...)` (single row)
- **Multi-row:** `rows, _ := pool.Query(ctx, sql, args...); defer rows.Close(); for rows.Next() { ... }`
- **Context values:** `ctxUserID := r.Context().Value(ctxUserID).(string)` (JWT middleware)
- **URL params:** `chi.URLParam(r, "gateway_id")`
- **JSON safely:** Check type assertions, e.g., `if v, ok := obj["key"].(string); ok { ... }`
- **Logging:** Use `log.Printf()` (standard library). No structured logger.

## Testing

**Full automated test suite:**
```bash
./test/test_api.sh
```

Covers: auth, org/user CRUD, qube claim, protocols list, gateway/sensor/template CRUD, CSV generation, docker-compose generation, config hash, commands, heartbeat, telemetry ingestion.

**Manual UI testing:**
Open `http://localhost:8888` — the test-ui has forms for all API endpoints with token management.

**Test data seeder:**
```bash
docker compose -f docker-compose.dev.yml run --rm influx-seeder
```

**Multipass VM integration test:**
```bash
./scripts/test-multipass.sh
```

Creates 2 VMs (cloud + qube), runs full flow: self-registration → config sync → telemetry.

## Environment Variables

**Cloud VM (production):**
```bash
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
JWT_SECRET=<strong-random-secret>
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home  # or GitLab registry
```

**Qube device (/opt/qube/.env):**
```bash
TPAPI_URL=http://<cloud-ip>:8081
QUBE_ID=Q-1001                     # Written by conf-agent after self-register
QUBE_TOKEN=<auto-obtained>         # Not set initially, obtained from /v1/device/register
WORK_DIR=/opt/qube
POLL_INTERVAL=30
MIT_TXT_PATH=/boot/mit.txt
```

**enterprise-influx-to-sql (configs.yml):**
```yaml
Service:
  PollInterval: 60
  LookbackMins: 5
  SensorMapPath: /config/sensor_map.json
InfluxDB:
  URL: http://127.0.0.1:8086
  DB: edgex
  Tables: [Measurements]
TPAPI:
  URL: http://<cloud-ip>:8081
  QubeID: Q-1001
  QubeToken: <same as QUBE_TOKEN>
```

## CI/CD

**GitHub Actions** (`.github/workflows/build-push.yml`):
- On push to main: builds amd64 + arm64 images, pushes to GHCR, deploys to Azure VM (if secrets set)
- ARM64 cross-compiled via QEMU (no ARM hardware needed)

**GitLab production** uses separate repos per service but same codebase:
- `registry.gitlab.com/iot-team4/product/enterprise-cloud-api:amd64.latest`
- `registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest`
- etc.

Only difference: `QUBE_IMAGE_REGISTRY` env var on cloud-api.

## Known Gotchas & Debugging

**conf-agent not deploying:**
- Check `/boot/mit.txt` exists in dev (mounted at `./test/mit.txt`)
- Check logs: `docker compose logs conf-agent`
- QUBE_ID/QUBE_TOKEN not set? Agent should self-register from mit.txt first.
- Verify TP-API reachable: `curl http://cloud-api:8081/health` from inside conf-agent container

**No sensor data appearing:**
1. Is gateway container running? `docker compose ps`
2. Is gateway writing to InfluxDB? Check InfluxDB: `curl 'http://localhost:8086/query?q=SHOW%20MEASUREMENTS'`
3. Is enterprise-influx-to-sql running? Check logs for "forwarded X readings"
4. Does `sensor_map.json` exist in `dev-qube-workdir/`? (mounted to `/config` in container)
5. Query API: `GET /api/v1/data/sensors/{id}/latest?token=...`

**Tests failing:**
- DB not initialized? `docker compose down -v && docker compose up -d postgres`
- JWT_SECRET changed? Tests use hardcoded dev secret. Keep `JWT_SECRET=dev-jwt-secret-change-in-production` in compose.
- Ports in use? Stop other services on 8080/8081/5432/8086.

**Gateway CSV not updating:**
- Config hash not changing? Hash includes all CSV rows. Adding a sensor triggers hash change automatically via `recomputeConfigHash()`.
- conf-agent caching? It polls every 30s. Check logs for "hash mismatch" and "downloaded config".

## File Locations Reference

**Protocol config generation:**
- `cloud/internal/tpapi/sync.go:renderGatewayConfig()` → configs.yml
- `cloud/internal/tpapi/sync.go:renderGatewayFiles()` → config.csv (+ maps/)
- `cloud/internal/api/sensors.go:generateCSVRows()` → CSV row data from template

**CRUD handlers:**
- Gateways: `cloud/internal/api/gateways.go`
- Sensors: `cloud/internal/api/sensors.go`
- Templates: `cloud/internal/api/templates.go`
- Qubes: `cloud/internal/api/qubes.go`
- Auth: `cloud/internal/api/auth.go`

**API routes:**
- `cloud/internal/api/router.go` (port 8080) — all Cloud API endpoints
- `cloud/internal/tpapi/router.go` (port 8081) — all TP-API endpoints

**Database migrations:**
- `cloud/migrations/001_init.sql` — orgs, users, qubes, protocols, initial Q-1001..Q-1020
- `cloud/migrations/002_gateways_sensors.sql` — gateways, sensors, templates, readings, service_csv_rows
- `cloud/migrations/003_device_catalog.sql` — global templates for Schneider, APC, etc.

## Important Implementation Notes

1. **Automatic protocol registration:** New protocols only require DB INSERT into `protocols` table. UI renders connection + address fields from `connection_params_schema` / `addr_params_schema` JSONB. No code change needed for routing/validation.

2. **Gateway deduplication:** Only one gateway container per `(protocol, host)` pair on a Qube. Adding a sensor to an existing gateway just appends CSV rows. The exception is SNMP: only ONE SNMP gateway per Qube regardless of host.

3. **CSV formats are not self-describing:** They must exactly match what the gateway binary expects. These formats are protocol-specific and hard-coded in `renderGatewayFiles()`.

4. **sensor_map.json is auto-generated:** Based on `(Equipment, Reading)` pairs from all sensors' CSV rows. The enterprise-influx-to-sql reads this to map InfluxDB measurements to sensor UUIDs. Do not edit manually.

5. **HMAC token lifecycle:** Qube self-registers with `register_key` from `/boot/mit.txt` → gets `QUBE_TOKEN` → TP-API uses that token for ALL subsequent calls. Token rotates only on manual re-claim.

6. **Docker Swarm detection:** conf-agent checks for swarm mode. Active swarm → `docker stack deploy`; otherwise `docker compose up -d`. The generated `docker-compose.yml` uses overlay network `qube-net` for swarm.

7. **GitHub vs GitLab:** Same codebase, different registry. Set `QUBE_IMAGE_REGISTRY` accordingly. Cloud-api uses this to generate docker-compose.yml for Qubes.

8. **No ORM:** Raw SQL with `pgx` driver. Use `?` placeholders, not `$1,$2` when using `sqlc`-style queries. Current code uses `$1,$2` with `pgx`.

9. **Test suite structure:** `test/test_api.sh` uses helper functions (`api`, `assert_status`, `code`, etc.). All tests depend on previous ones (IDs carried forward).

10. **Zero-touch dev workflow:** After initial `docker compose up -d`, the test-ui (port 8888) provides forms to exercise every endpoint. Register an org, claim a Qube, add gateway+sensor, watch the telemetry flow.

## Related Documentation

- `README.md` — Project overview & quick start
- `ARCHITECTURE.md` — Detailed system architecture & design decisions
- `DEPLOYMENT.md` — Production deployment on cloud VM + Qube integration
- `TESTING.md` — Manual testing scenarios (19 scenarios)
- `ADDING-PROTOCOLS.md` — Complete guide to add a new protocol (LoRaWAN, etc.)
- `UI-API-GUIDE.md` — API reference with curl examples for all endpoints

---

**For future Claude Code instances:** This repository is Go-based with Docker Compose dev environment. All services run in a single binary (`cloud-api`) on ports 8080 (JWT) and 8081 (HMAC). Edge agents are separate Go programs. The architecture is protocol-driven via database schemas, not code enums. Patterns: `writeJSON()`, `writeError()`, `pool.Query/QueryRow`, middleware context values. Key files: `sensors.go` (CSV generation), `sync.go` (config generation), `router.go` (route mapping).
