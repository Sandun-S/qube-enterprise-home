# Qube Enterprise v2

Cloud-to-edge IoT fleet management platform. A **Qube** is a Raspberry Pi / Kadas edge device
running protocol reader containers (Modbus TCP, OPC-UA, SNMP, MQTT, HTTP). The Enterprise layer
adds zero-touch provisioning, template-driven reader deployment, real-time WebSocket sync, and a
TimescaleDB telemetry pipeline — all without manual device intervention after initial claim.

---

## What changed in v2

| v1 | v2 |
|----|-----|
| CSV files pushed to Qube | SQLite database on Qube (shared Docker volume) |
| HTTP polling every 30s | WebSocket push (fallback: HTTP polling) |
| "gateway" containers | "reader" containers |
| Single `sensor_templates` table | Split: `device_templates` + `reader_templates` |
| YAML `configs.yml` per gateway | JSON config stored in SQLite |
| Internal MQTT broker on Qube | Removed — no Grafana on Qube |
| Single Postgres database | `qubedb` (management) + `qubedata` (TimescaleDB) |
| CSV config regenerated on sync | JSON diff pushed via WebSocket |

---

## Repository structure

```
qube-enterprise/
├── cloud/                          # Cloud API + TP-API + WebSocket (single Go binary)
│   ├── cmd/server/main.go          # Entry point — starts :8080 and :8081
│   ├── internal/api/               # Cloud Management API (JWT, port 8080)
│   │   ├── auth.go                 # Register / login
│   │   ├── qubes.go                # Qube CRUD + claim by register_key
│   │   ├── readers.go              # Reader CRUD + auto container creation
│   │   ├── sensors.go              # Sensor CRUD + template merging
│   │   ├── templates.go            # Device + reader template CRUD
│   │   ├── containers.go           # Container list (auto-managed)
│   │   ├── telemetry.go            # Telemetry query endpoints
│   │   ├── hash.go                 # Config hash recomputation
│   │   ├── commands.go             # Remote command dispatch (WS + DB queue)
│   │   ├── registry.go             # Container registry settings
│   │   ├── middleware.go           # JWT + RBAC
│   │   └── router.go               # All route registration
│   ├── internal/tpapi/             # TP-API (HMAC, port 8081) — Qube-facing only
│   │   ├── router.go               # Routes + HMAC middleware
│   │   ├── sync.go                 # sync/state, sync/config (JSON SQLite data)
│   │   ├── telemetry.go            # telemetry/ingest (SenML → TimescaleDB)
│   │   └── commands.go             # commands/poll, commands/:id/ack
│   ├── internal/ws/                # WebSocket server (/ws, /ws/dashboard)
│   ├── migrations/                 # Management DB (qubedb)
│   │   ├── 001_init.sql            # Core schema
│   │   ├── 002_global_data.sql     # Protocols + reader templates + device templates
│   │   └── 003_test_seeds.sql      # Dev superadmin + Q-1001..Q-1020
│   └── migrations-telemetry/       # Telemetry DB (qubedata)
│       └── 001_timescale_init.sql  # TimescaleDB hypertable
│
├── conf-agent/                     # Edge agent — runs on every Qube
│   ├── main.go                     # Entry point
│   ├── register.go                 # Self-registration from /boot/mit.txt
│   ├── websocket.go                # WebSocket client (primary sync)
│   ├── poll.go                     # HTTP polling fallback
│   ├── apply.go                    # Config message → SQLite writer
│   ├── docker.go                   # Docker API: stop/restart containers
│   ├── deploy.go                   # Docker stack deploy
│   ├── heartbeat.go                # Periodic heartbeat to TP-API
│   └── commands.go                 # Remote command executor
│
├── enterprise-influx-to-sql/       # Telemetry bridge — reads InfluxDB → TP-API
│   └── main.go                     # Reads SQLite sensor_map, uploads SenML
│
├── modbus-reader/                  # Modbus TCP reader container (PLC4X)
├── snmp-reader/                    # SNMP reader container (gosnmp)
├── opcua-reader/                   # OPC-UA reader container (gopcua)
├── mqtt-gateway/                   # MQTT reader container (paho)
├── http-reader/                    # HTTP/REST reader container
│
├── pkg/                            # Shared Go modules (imported at build time)
│   ├── sqlitedb/                   # SQLite schema + reader helpers
│   └── coreswitchclient/           # Core-switch HTTP client
│
├── core-switch/                    # Edge data router (InfluxDB + live WebSocket output)
├── con-checker/                    # Connectivity checker utility
│
├── standards/                      # Architecture standards
│   ├── READER_STANDARD.md          # Reader container interface spec
│   ├── SQLITE_SCHEMA.md            # Edge SQLite schema spec
│   ├── TEMPLATE_STANDARD.md        # Template JSON schema spec
│   └── CORESWITCH_FORMAT.md        # Core-switch DataIn format
│
├── test/
│   ├── test_api.sh                 # Full API test suite (v2, ~220 assertions)
│   └── mit.txt                     # Dev /boot/mit.txt for conf-agent
│
├── test-ui/index.html              # Browser dev console (http://localhost:8888)
├── docker-compose.dev.yml          # Full local dev stack (TimescaleDB, no MQTT broker)
├── ARCHITECTURE.md                 # Detailed architecture and data flow
├── DEPLOYMENT.md                   # Production deployment guide
├── TESTING.md                      # Manual testing scenarios
├── MIGRATION_GUIDE.md              # v1 → v2 migration guide
└── QUBE_ENTERPRISE_V2_ARCHITECTURE.md  # Full v2 design document
```

---

## How it works

```
1. Factory flashes Qube → writes /boot/mit.txt (device_id, register_key)
   → inserts into Postgres via write-to-database.sh

2. Customer claims Qube in portal → POST /api/v1/qubes/claim
   → HMAC auth token generated

3. Qube boots → conf-agent reads /boot/mit.txt → POST /v1/device/register
   → gets QUBE_TOKEN → connects to WebSocket ws://cloud:8080/ws

4. User adds reader + sensors in portal
   → config hash changes
   → cloud pushes {"type":"config_update"} via WebSocket

5. conf-agent receives push → GET /v1/sync/config
   → writes readers/sensors/containers to SQLite
   → stops affected containers via Docker API (Swarm recreates them)

6. Reader container starts → reads config from SQLite (shared volume)
   → polls device → sends data to core-switch → InfluxDB v1
   OR "live" output → direct WebSocket to cloud

7. enterprise-influx-to-sql polls InfluxDB
   → reads sensor_map from SQLite
   → POST /v1/telemetry/ingest (SenML) → TimescaleDB (qubedata)

8. Frontend queries Cloud API :8080 → readings from TimescaleDB
```

---

## Ports

| Port | Service | Auth | Who calls it |
|------|---------|------|-------------|
| 8080 | Cloud API + WebSocket | JWT | Frontend, Qubes (WebSocket) |
| 8081 | TP-API | HMAC-SHA256 | Qubes only (HTTP polling fallback) |
| 5432 | PostgreSQL (qubedb + qubedata) | password | Cloud API internal |
| 8086 | InfluxDB v1 | none | Edge: readers → core-switch → InfluxDB |
| 8888 | Test UI | none | Browser dev console |

---

## Quick start — local dev

```bash
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d --build
open http://localhost:8888   # test UI

# Run full test suite
./test/test_api.sh

# View logs
docker compose -f docker-compose.dev.yml logs -f cloud-api
docker compose -f docker-compose.dev.yml logs -f conf-agent

# Seed InfluxDB with test data
docker compose -f docker-compose.dev.yml run --rm influx-seeder

# Start optional simulators (Modbus, SNMP)
docker compose -f docker-compose.dev.yml --profile simulators up -d
```

---

## Authentication

**Cloud API (port 8080):**
- JWT Bearer tokens via `POST /api/v1/auth/login`
- Roles: `superadmin`, `admin`, `editor`, `viewer`

**TP-API (port 8081):**
- HMAC-SHA256 in `Authorization: Bearer <token>` + `X-Qube-ID: <id>` headers
- Token obtained once via `POST /v1/device/register` (after Qube is claimed)
- Token = `HMAC-SHA256(key=orgSecret, data=qubeID+":"+orgSecret)`

Dev accounts:
- Superadmin: `iotteam@internal.local` / `iotteam2024`
- Pre-registered qubes: `Q-1001..Q-1020` with keys `TEST-Q1001-REG` etc.

---

## Roles

| Role | Permissions |
|------|-------------|
| `superadmin` | Global templates, registry settings, all orgs |
| `admin` | Claim qubes, user management, full org access |
| `editor` | Add/edit readers, sensors, templates, send commands |
| `viewer` | Read-only — all data |

---

## Environment variables

### Cloud VM
```bash
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
TELEMETRY_DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedata?sslmode=disable
JWT_SECRET=<strong-random-secret>
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home
# GitLab: QUBE_IMAGE_REGISTRY=registry.gitlab.com/iot-team4/product
```

### Qube device
```bash
CLOUD_WS_URL=ws://cloud.yourcompany.com:8080/ws   # WebSocket (primary)
TPAPI_URL=http://cloud.yourcompany.com:8081         # HTTP polling (fallback)
SQLITE_PATH=/opt/qube/data/qube.db
WORK_DIR=/opt/qube
POLL_INTERVAL=30
# QUBE_ID and QUBE_TOKEN auto-obtained via self-registration
```

### enterprise-influx-to-sql
```bash
SQLITE_PATH=/opt/qube/data/qube.db
TPAPI_URL=http://cloud:8081
QUBE_ID=Q-1001
QUBE_TOKEN=<same as QUBE_TOKEN from device/register>
INFLUX_URL=http://127.0.0.1:8086
INFLUX_DB=edgex
```

---

## Device provisioning (flash time)

`write-to-database.sh` is called with `device_id`, `register_key`, `maintain_key`:
```bash
ENTERPRISE_DB_HOST=cloud-vm:5432
ENTERPRISE_DB_USER=qubeadmin
ENTERPRISE_DB_PASS=qubepass
ENTERPRISE_DB_NAME=qubedb
./scripts/write-to-database.sh Q-1001 REG-KEY-HERE MNT-KEY-HERE
```

On first boot conf-agent reads `/boot/mit.txt`:
```yaml
deviceid: Q-1001
register: REG-KEY-HERE
maintain: MNT-KEY-HERE
```

Calls `POST /v1/device/register` → polls every 60s until claimed →
receives `QUBE_TOKEN` → connects WebSocket → begins sync.

---

## Container images

| Image | Arch | Registry suffix |
|-------|------|----------------|
| `cloud-api` | amd64 | `enterprise-cloud-api` |
| `conf-agent` | arm64 | `enterprise-conf-agent` |
| `enterprise-influx-to-sql` | arm64 | `enterprise-influx-to-sql` |
| `modbus-reader` | arm64 | `modbus-reader` |
| `snmp-reader` | arm64 | `snmp-reader` |
| `mqtt-reader` | arm64 | `mqtt-reader` |
| `opcua-reader` | arm64 | `opcua-reader` |
| `http-reader` | arm64 | `http-reader` |

Registry is controlled by `QUBE_IMAGE_REGISTRY` on the cloud-api. The registry settings
endpoint (`PUT /api/v1/admin/registry`) lets IoT team switch between GitHub and GitLab
registries without redeployment.

---

## Implementation status

| Phase | Feature | Status |
|-------|---------|--------|
| 0 | Standards, shared pkg/, docker-compose v2 | ✅ |
| 1 | Cloud API rewrite (readers, templates, WebSocket, TimescaleDB) | ✅ |
| 2 | TP-API v2 (JSON sync, SenML telemetry) | ✅ |
| 3 | conf-agent v2 (WebSocket, SQLite writer, Docker API reload) | ✅ |
| 4 | Reader containers (modbus, snmp, mqtt, opcua, http) | ✅ |
| 5 | Core-switch v2, enterprise-influx-to-sql v2 | ✅ |
| 6 | Testing & documentation | ✅ |
| 7 | CI/CD (GitHub Actions amd64 + arm64) | 🔲 |
