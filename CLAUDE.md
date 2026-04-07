# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Qube Enterprise v2** is a cloud-to-edge IoT fleet management system. A **Qube** is a Raspberry Pi/Kadas edge device running Docker Swarm with protocol reader containers (Modbus TCP, OPC-UA, SNMP, MQTT, HTTP). The Enterprise layer adds zero-touch provisioning, WebSocket-based config sync, SQLite edge config store, and TimescaleDB telemetry pipeline.

**Technology Stack:**
- Go 1.22 (all backend services)
- PostgreSQL — `qubedb` (management) + `qubedata` (TimescaleDB telemetry)
- SQLite (edge config store on each Qube — WAL mode, shared Docker volume)
- InfluxDB v1 (edge data buffer — matches existing core-switch)
- Docker + Docker Swarm (deployment)
- JWT + HMAC (dual auth: users and devices)
- WebSocket (primary cloud→Qube sync; HTTP polling fallback)

## Repository Structure

```
qube-enterprise/
├── cloud/                          # Cloud API + TP-API + WebSocket (single Go binary)
│   ├── cmd/server/main.go          # Entry point — starts :8080 and :8081
│   ├── internal/api/               # Cloud Management API (JWT, port 8080)
│   │   ├── auth.go                 # Register / login
│   │   ├── qubes.go                # Qube CRUD + claim by register_key
│   │   ├── readers.go              # Reader CRUD + auto container creation
│   │   ├── sensors.go              # Sensor CRUD + template config merging
│   │   ├── templates.go            # Device + reader template CRUD
│   │   ├── containers.go           # Container list (auto-managed)
│   │   ├── telemetry.go            # Telemetry query endpoints (TimescaleDB)
│   │   ├── hash.go                 # Config hash recomputation
│   │   ├── commands.go             # Remote command dispatch (WS + DB queue) — 28 valid command types
│   │   ├── registry.go             # Container registry settings
│   │   ├── middleware.go           # JWT + RBAC
│   │   ├── wshub.go                # WebSocket hub (Qube connections)
│   │   ├── websocket.go            # Qube WebSocket handler (/ws)
│   │   ├── dashboard_ws.go         # Dashboard WebSocket handler (/ws/dashboard)
│   │   └── router.go               # All route registration
│   ├── internal/tpapi/             # TP-API (HMAC, port 8081) — Qube-facing only
│   │   ├── router.go               # Routes + HMAC middleware
│   │   ├── sync.go                 # sync/state, sync/config (JSON SQLite data)
│   │   ├── telemetry.go            # telemetry/ingest (SenML → TimescaleDB)
│   │   └── commands.go             # commands/poll, commands/:id/ack
│   ├── migrations/                 # Management DB (qubedb)
│   │   ├── 001_init.sql            # Core schema (orgs, users, qubes, readers, sensors, etc.)
│   │   ├── 002_global_data.sql     # Protocols + reader templates + global device templates
│   │   └── 003_test_seeds.sql      # Dev superadmin + Q-1001..Q-1020
│   └── migrations-telemetry/       # Telemetry DB (qubedata)
│       └── 001_timescale_init.sql  # TimescaleDB hypertable: sensor_readings
│
├── conf-agent/                     # Edge agent — runs on every Qube (upgraded from conf-agent-master)
│   ├── main.go                     # Startup: logrus + config + mit.txt + local HTTP + enterprise agent
│   ├── configs/config.go           # Merged config: YAML (v1) + env var overrides + ReadMitTxt()
│   ├── agent/agent.go              # WebSocket + polling loop + ExecCommand (28 command types)
│   ├── docker/deploy.go            # Docker Swarm / Compose deploy
│   ├── sqlite/sqlite.go            # SQLite schema init + WriteConfig (readers/sensors/settings)
│   ├── tpapi/client.go             # TP-API HTTP client (replaces v1 HMAC conf-api client)
│   ├── http/http.go                # Local management HTTP server (web UI on :Port)
│   ├── http/run.go                 # v1 polling loop stub (replaced by agent/agent.go)
│   ├── scripts/                    # 17 shell scripts for device management (from conf-agent-master)
│   ├── html/                       # 5 web UI pages: index, reboot, shutdown, repair, reset-ips
│   ├── network/                    # Netplan templates, iptables rules, systemd service unit
│   ├── config.yml                  # YAML config (LogLevel, Port, DownloadTimeout)
│   └── config-prod.yml             # Production config reference
│
├── enterprise-influx-to-sql/       # Telemetry bridge
│   └── main.go                     # Reads InfluxDB v1 + SQLite sensor_map → SenML → TP-API
│
├── modbus-reader/                  # Modbus TCP reader (PLC4X)
├── snmp-reader/                    # SNMP reader (gosnmp)
├── opcua-reader/                   # OPC-UA reader (gopcua)
├── mqtt-gateway/                   # MQTT reader (paho)
├── http-reader/                    # HTTP/REST reader
│
├── pkg/                            # Shared Go modules (imported at build time)
│   ├── sqlitedb/                   # SQLite schema init + read helpers
│   └── coreswitchclient/           # core-switch HTTP client
│
├── core-switch/                    # Edge data router (InfluxDB + live WebSocket output)
├── con-checker/                    # Connectivity checker
│
├── standards/                      # Architecture standards
│   ├── READER_STANDARD.md, SQLITE_SCHEMA.md
│   ├── TEMPLATE_STANDARD.md, CORESWITCH_FORMAT.md
│
├── test/
│   ├── test_api.sh                 # Full API test suite (v2, ~220 assertions)
│   └── mit.txt                     # Dev /boot/mit.txt for conf-agent
│
├── test-ui/index.html              # Browser dev console (http://localhost:8888)
├── docker-compose.dev.yml          # Local dev stack (TimescaleDB, SQLite volume, no MQTT broker)
├── ARCHITECTURE.md                 # System architecture and data flow
├── DEPLOYMENT.md                   # Production deployment guide
├── TESTING.md                      # Manual curl testing scenarios
├── ADDING-PROTOCOLS.md             # How to add a new protocol
├── MIGRATION_GUIDE.md              # v1 → v2 migration
└── UI-API-GUIDE.md                 # Full API reference
```

## Quick Start

### Run locally (Docker Compose)

```bash
# Start full stack (rebuilds images)
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d --build

# Access services
# Cloud API + WebSocket: http://localhost:8080
# TP-API:               http://localhost:8081
# Test UI:              http://localhost:8888
# Postgres:             localhost:5432
# InfluxDB:             localhost:8086

# Check health
curl -s http://localhost:8080/health | jq .   # {"version":"2",...}
curl -s http://localhost:8081/health | jq .

# Run full test suite
./test/test_api.sh

# View logs
docker compose -f docker-compose.dev.yml logs -f cloud-api
docker compose -f docker-compose.dev.yml logs -f conf-agent

# Stop everything
docker compose -f docker-compose.dev.yml down
```

### Build cloud-api locally

```bash
cd cloud
go build -o cloud-api ./cmd/server
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable \
TELEMETRY_DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedata?sslmode=disable \
JWT_SECRET=dev-jwt-secret-change-in-production \
./cloud-api
```

## Architecture — The Data Flow

```
USER → Cloud API (:8080, JWT)
  ↓ creates/updates readers + sensors
readers + sensors → PostgreSQL (qubedb)
  ↓ recomputeConfigHash() → config_state.hash changes
  ↓ WebSocket push: {"type":"config_update",...}
conf-agent (Qube) receives push OR polls /v1/sync/state
  ↓ hash mismatch → GET /v1/sync/config
Returns: {readers, sensors, containers, docker_compose_yml}
  ↓ conf-agent writes to SQLite /opt/qube/data/qube.db
  ↓ Docker API: stop affected reader containers
  ↓ Docker Swarm: recreates containers → reads fresh SQLite
Reader containers (modbus-reader, snmp-reader, etc.)
  ↓ POST /v3/batch to core-switch:8585
  ↓ output=influxdb → InfluxDB v1 (edgex)
  ↓ output=live → WebSocket to cloud (:8080/ws/dashboard)
enterprise-influx-to-sql
  ↓ reads InfluxDB + SQLite sensor_map (sensors table)
  ↓ POST /v1/telemetry/ingest (SenML) → TimescaleDB (qubedata)
Cloud API /api/v1/data/readings → User
```

## Ports

| Port | Service | Auth | Usage |
|------|---------|------|-------|
| 8080 | Cloud API + WebSocket | JWT Bearer | Frontend, developers, Qube WS |
| 8081 | TP-API | HMAC-SHA256 | Qube devices only |
| 5432 | PostgreSQL (qubedb + qubedata) | password | Cloud API internal |
| 8086 | InfluxDB v1 | none | Edge data buffer |
| 8585 | core-switch | none | Reader → core-switch |
| 8888 | Test UI | none | Browser dev console |

## Authentication & Authorization

**Cloud API (port 8080):**
- JWT tokens via `Authorization: Bearer <token>`
- Issued by `POST /api/v1/auth/login`
- Roles: `superadmin`, `admin`, `editor`, `viewer`
- Context values: `ctxUserID`, `ctxOrgID`, `ctxRole` (from middleware)

**TP-API (port 8081):**
- Headers: `X-Qube-ID: Q-1001` + `Authorization: Bearer <token>`
- Token = `HMAC-SHA256(key=orgSecret, data=qubeID+":"+orgSecret)`
- Obtained via `POST /v1/device/register` (one-time, after claim)
- Context values: `ctxQubeID`, `ctxOrgID` (from qubeAuthMiddleware)

**Dev test accounts:**
- Superadmin: `iotteam@internal.local` / `iotteam2024`
- Pre-registered Qubes: Q-1001..Q-1020 with keys `TEST-Q1001-REG` etc.

## Common Development Tasks

### Run/filter tests

```bash
./test/test_api.sh 2>&1 | grep "✗"    # Show failures only
./test/test_api.sh 2>&1 | grep "✓"    # Show passes only
```

### Debug conf-agent sync

```bash
docker compose -f docker-compose.dev.yml logs -f conf-agent
# conf-agent does:
# 1. Reads /boot/mit.txt → POST /v1/device/register → gets QUBE_TOKEN
# 2. Connects WebSocket ws://cloud-api:8080/ws
# 3. On "config_update" message → GET /v1/sync/config
# 4. Writes readers/sensors to SQLite /opt/qube/data/qube.db
# 5. Docker API: stop affected containers → Swarm recreates them
# 6. Also polls TP-API every POLL_INTERVAL as fallback
```

### Simulate sensor data in dev

```bash
# Seed InfluxDB (what reader containers produce)
docker compose -f docker-compose.dev.yml run --rm influx-seeder

# Verify InfluxDB data
curl 'http://localhost:8086/query?q=SHOW+MEASUREMENTS&db=edgex'

# Check enterprise-influx-to-sql forwarding
docker compose -f docker-compose.dev.yml logs enterprise-influx-to-sql
```

### Add a new endpoint

1. Write handler in `cloud/internal/api/` or `cloud/internal/tpapi/`
2. Register in `cloud/internal/api/router.go` or `tpapi/router.go`
3. Add test to `test/test_api.sh`
4. Add curl example to `TESTING.md`
5. Responses: `writeJSON(w, status, map[string]any{...})` or `writeError(w, status, msg)`

### Add a field to a response

Find the handler, update the `map[string]any` passed to `writeJSON()`. No DTO structs.

### Add a global device template

```bash
curl -X POST http://localhost:8080/api/v1/device-templates \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"...","protocol":"modbus_tcp","sensor_config":{...}}'
```
Or seed in `cloud/migrations/002_global_data.sql`.

### Reset the database (dev only)

```bash
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d postgres
# Migrations auto-run via docker-entrypoint-initdb.d/
```

## Database Schema (qubedb — management)

- `organisations`, `users` — multi-tenant
- `qubes` — devices with auth_token_hash, ws_connected
- `config_state` — hash + config_version per Qube
- `readers` — one per container (protocol + connection), linked to qube
- `sensors` — linked to reader + optional device_template, config_json = merged
- `containers` — auto-created with reader, deployed by conf-agent
- `device_templates` — sensor config schemas (org or global)
- `reader_templates` — container specs (superadmin only)
- `protocols` — active protocols
- `qube_commands` — command queue (WS + polling)
- `coreswitch_settings`, `telemetry_settings` — per-Qube settings
- `registry_settings` — container registry config (one row, superadmin)

## Database Schema (qubedata — telemetry)

- `sensor_readings` — TimescaleDB hypertable on `time`
  - columns: `time`, `qube_id`, `sensor_id`, `field_key`, `value`, `unit`, `tags`

## Key Code Patterns

- **Responses:** `writeJSON(w, http.StatusOK, data)` or `writeError(w, http.StatusBadRequest, "msg")`
- **DB access (single row):** `pool.QueryRow(ctx, sql, args...).Scan(&vars...)`
- **DB access (multi-row):** `rows, _ := pool.Query(ctx, sql, args...); defer rows.Close(); for rows.Next() { ... }`
- **Context values:** `r.Context().Value(ctxOrgID).(string)` (JWT middleware)
- **URL params:** `chi.URLParam(r, "reader_id")`
- **JSON safely:** `if v, ok := obj["key"].(string); ok { ... }`
- **Logging:** `log.Printf()` (standard library only, no structured logger)
- **No ORM:** Raw SQL with `pgx` driver, `$1,$2` placeholders

## Key File Locations

**Config sync:**
- `cloud/internal/tpapi/sync.go:syncConfigHandler()` — builds sync payload (readers + sensors + containers + compose)
- `cloud/internal/tpapi/sync.go:buildComposeYML()` — generates docker-compose.yml from containers table
- `cloud/internal/api/hash.go:recomputeConfigHash()` — called after every mutation

**CRUD handlers:**
- Readers: `cloud/internal/api/readers.go`
- Sensors: `cloud/internal/api/sensors.go`
- Templates: `cloud/internal/api/templates.go`
- Qubes: `cloud/internal/api/qubes.go`
- Auth: `cloud/internal/api/auth.go`

**API routes:**
- `cloud/internal/api/router.go` (port 8080) — all Cloud API endpoints
- `cloud/internal/tpapi/router.go` (port 8081) — all TP-API endpoints

**Database migrations:**
- `cloud/migrations/001_init.sql` — all management tables
- `cloud/migrations/002_global_data.sql` — protocols, reader templates, global device templates
- `cloud/migrations/003_test_seeds.sql` — dev seeds (superadmin + Q-1001..Q-1020)
- `cloud/migrations-telemetry/001_timescale_init.sql` — TimescaleDB sensor_readings

## Testing

**Full automated test suite:**
```bash
./test/test_api.sh
```

Covers: auth, RBAC, qube claim, user management, protocols, reader templates, device templates,
registry, readers (all protocols), sensors (with/without templates), config hash propagation,
sync state/config, containers, commands (send/poll/ack), telemetry ingest/query, delete cascades,
multi-org isolation.

**Manual UI testing:** `http://localhost:8888`

**Influx data seeder:** `docker compose -f docker-compose.dev.yml run --rm influx-seeder`

## Environment Variables

**Cloud VM (production):**
```bash
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
TELEMETRY_DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedata?sslmode=disable
JWT_SECRET=<strong-random-secret>
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home  # or GitLab
```

**Qube device:**
```bash
CLOUD_WS_URL=ws://<cloud-ip>:8080/ws  # WebSocket (primary)
TPAPI_URL=http://<cloud-ip>:8081       # HTTP polling (fallback)
SQLITE_PATH=/opt/qube/data/qube.db
WORK_DIR=/opt/qube
POLL_INTERVAL=30
# QUBE_ID and QUBE_TOKEN auto-obtained via /v1/device/register
```

**enterprise-influx-to-sql:**
```bash
SQLITE_PATH=/opt/qube/data/qube.db
TPAPI_URL=http://<cloud-ip>:8081
QUBE_ID=Q-1001
QUBE_TOKEN=<from device/register>
INFLUX_URL=http://127.0.0.1:8086
INFLUX_DB=edgex
```

## CI/CD

**GitHub Actions** (`.github/workflows/build-push.yml`):
- On push to main: builds amd64 + arm64 images, pushes to GHCR
- ARM64 cross-compiled via QEMU

**GitLab production:** Same codebase, different `QUBE_IMAGE_REGISTRY`:
- `registry.gitlab.com/iot-team4/product/enterprise-cloud-api:amd64.latest`
- `registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest`
- etc.

## Known Gotchas & Debugging

**conf-agent not syncing:**
- Check `/boot/mit.txt` mounted: `docker compose logs conf-agent | head -20`
- WebSocket failing? conf-agent falls back to HTTP polling automatically
- Verify: `curl http://cloud-api:8081/health` from inside conf-agent container

**No sensor data:**
1. Is reader container running? `docker service ls` (on Qube) or `docker compose ps`
2. Is reader writing to InfluxDB? `curl 'http://localhost:8086/query?q=SHOW+MEASUREMENTS&db=edgex'`
3. Is enterprise-influx-to-sql running? Check logs for "forwarded X readings"
4. Is SQLite populated? `sqlite3 /path/to/qube.db ".tables"` — sensors table should have rows
5. Query API: `GET /api/v1/data/sensors/{id}/latest`

**Tests failing:**
- DB not initialized? `docker compose down -v && docker compose up -d postgres && sleep 5`
- JWT_SECRET changed? Keep `JWT_SECRET=dev-jwt-secret-change-in-production` in dev compose
- Ports in use? Stop other services on 8080/8081/5432/8086
- TimescaleDB not ready? Wait 10s after `docker compose up`

**Config hash not changing:**
- `recomputeConfigHash()` is called after every reader/sensor mutation
- Check `SELECT hash, config_version FROM config_state WHERE qube_id='Q-1001'`
- If stale: `UPDATE config_state SET hash='', config_version=config_version+1 WHERE qube_id='Q-1001'`

**Reader containers not deploying:**
- conf-agent needs Docker socket mounted
- Check containers table: `SELECT * FROM containers WHERE qube_id='Q-1001'`
- Image resolved from registry_settings + reader_template.image_suffix

## Important Implementation Notes

1. **SQLite = only writer is conf-agent.** Reader containers open read-only. No live polling — config reload = Docker stop → Swarm recreate.

2. **WebSocket hub** in `wshub.go` tracks connected Qubes. Commands try WS delivery first, fall back to DB queue. Hub calls `pool.Exec()` on connect/disconnect to update `qubes.ws_connected`.

3. **Config hash** includes readers + sensors + containers. Any mutation calls `recomputeConfigHash()` which triggers conf-agent sync on next WebSocket event or poll.

4. **Template merging:** `sensor.config_json` = `device_template.sensor_config` merged with user `params`. The reader sees the merged result in SQLite — no template lookup at runtime.

5. **HMAC token** = `HMAC-SHA256(key=org.org_secret, data=qubeID+":"+org.org_secret)`. Stable per org/qube. Changes only on re-claim (org_secret remains same, so token is deterministic).

6. **Two databases, one Postgres instance.** `pool` = qubedb (management). `telemetryPool` = qubedata (TimescaleDB). Both pools passed to handlers that need them.

7. **No MQTT broker on Qube.** MQTT protocol is for connecting to external brokers. The mqtt-reader container subscribes to an external broker.

8. **Protocol-driven UI.** Adding a protocol = 1 SQL INSERT. `reader_template.connection_schema` is a JSON Schema that drives the UI form for reader creation.

9. **Sensor output modes:** `"influxdb"` (→ InfluxDB v1), `"live"` (→ WebSocket dashboard), `"influxdb,live"` (both). Readers check this from SQLite and route accordingly via core-switch.

10. **conf-agent is an upgrade of conf-agent-master.** The enterprise conf-agent keeps the entire original structure (`scripts/`, `html/`, `network/`, `http/` local server, logrus, YAML config) and layers enterprise features on top. Key additions: `agent/agent.go` (WS + polling), `sqlite/sqlite.go`, `docker/deploy.go`, `tpapi/client.go`. The old HMAC-to-conf-api polling (`http/run.go`) is replaced by `agent.Start()`. Config is YAML (LogLevel/Port) + env var overrides (TPAPI_URL, CLOUD_WS_URL, etc.).

11. **28 valid command types.** `cloud/internal/api/commands.go:validCommands` lists all. Groups: enterprise container/config (10), network (4), identity/system (3), backup/restore (2), maintenance mode (3), service management (3), file transfer (2). All dispatched via the same qube_commands table + WS/poll infrastructure — no API changes needed for new command types.

12. **Local HTTP server on Qube.** conf-agent starts a web server on `conf.Port` (default 8081) for local device management: `/` (device info), `/reboot`, `/shutdown`, `/reset-ips`, `/repair`, `/logs`. Protected by the `maintain` key from `/boot/mit.txt`. Separate from the TP-API port.

## Related Documentation

- `README.md` — Project overview & quick start
- `ARCHITECTURE.md` — System architecture, data flow, design decisions
- `DEPLOYMENT.md` — Cloud VM + Qube device production setup
- `TESTING.md` — Manual curl testing scenarios for all endpoints
- `ADDING-PROTOCOLS.md` — How to add a new protocol (LoRaWAN, BACnet, etc.)
- `MIGRATION_GUIDE.md` — v1 → v2 migration steps
- `UI-API-GUIDE.md` — API reference for test UI and direct API use
- `standards/` — Reader, SQLite schema, template, and core-switch format specs

---

**For future Claude Code instances:** This is a v2 Go-based IoT management system. Cloud API runs on :8080 (JWT) and :8081 (HMAC). Qubes sync config via WebSocket and store it in SQLite. Reader containers are auto-deployed by conf-agent. Key patterns: `writeJSON()`, `writeError()`, `pool.Query/QueryRow`, middleware context values. Readers = gateways v2. SQLite replaces CSV files. No CSV generation code exists in v2.
