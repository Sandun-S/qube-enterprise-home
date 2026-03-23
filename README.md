# Qube Enterprise

Cloud-to-edge IoT fleet management platform. A Qube is a Raspberry Pi / Kadas
edge device running protocol gateways (Modbus TCP, OPC-UA, SNMP, MQTT). The
Enterprise layer adds automated gateway/sensor provisioning, template-driven CSV
generation, and a telemetry pipeline to cloud Postgres — all without touching
the device manually after initial claim.

---

## Repository structure

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
│       └── 003_device_catalog.sql  # Global device templates
│
├── conf-agent/                     # Edge agent — runs on every Qube
│   └── main.go                     # Self-registers, hash sync, docker stack deploy
│
├── enterprise-influx-to-sql/       # Edge telemetry bridge — runs on every Qube
│   ├── main.go                     # InfluxDB v1 → sensor_map.json → TP-API
│   └── configs.yml
│
├── mqtt-gateway/                   # Enterprise MQTT gateway container
│   └── main.go                     # Reads topics.csv → subscribes → core-switch
│
├── scripts/
│   ├── setup-cloud.sh              # Provision cloud VM
│   ├── setup-qube.sh               # Provision qube VM
│   └── write-to-database.sh        # Flash-time script — inserts into both DBs
│
├── test/
│   ├── mit.txt                     # Dev fake /boot/mit.txt for compose testing
│   └── mosquitto/mosquitto.conf
│
├── test-ui/index.html              # Browser dev console
├── docker-compose.dev.yml          # Full local dev stack
├── ARCHITECTURE.md                 # Detailed architecture and bridge doc
└── README.md
```

---

## How it works in 60 seconds

```
1. Factory flashes Qube → generates device_id + register_key →
   writes /boot/mit.txt + inserts into Postgres (Enterprise) and MySQL (Qube Lite)

2. Customer buys Qube → enters register_key in portal → device claimed
   → HMAC token generated

3. Qube boots → conf-agent reads /boot/mit.txt → calls POST /v1/device/register
   → gets QUBE_TOKEN automatically → no manual step needed

4. User adds gateway + sensor in portal → Cloud generates CSV rows
   → config hash changes

5. conf-agent detects hash mismatch → downloads docker-compose.yml + CSV files
   → docker stack deploy → gateway container starts → polls device

6. Gateway → core-switch → influxdb-relay → InfluxDB v1
   enterprise-influx-to-sql reads InfluxDB, maps via sensor_map.json
   → POST /v1/telemetry/ingest → Postgres sensor_readings

7. Frontend queries Cloud API :8080 → live readings
```

---

## Ports

| Port | Service | Auth | Who calls it |
|------|---------|------|-------------|
| 8080 | Cloud Management API | JWT | Frontend, developers |
| 8081 | TP-API | HMAC | Qubes only |
| 5432 | Postgres | password | Cloud API internal |

Frontend only uses port 8080. TP-API port 8081 is Qube-facing only, never frontend.

---

## Quick start — local dev (Docker Compose)

```bash
docker compose -f docker-compose.dev.yml down -v
docker compose -f docker-compose.dev.yml up -d --build
open http://localhost:8888   # test UI
```

After stack is up, run through the test scenarios in TESTING.md.

---

## Roles

| Role | Who | Permissions |
|------|-----|-------------|
| `superadmin` | IoT team | Manage global templates, bypass org checks |
| `admin` | Org admin | Claim devices, full org management |
| `editor` | Org staff | Add/edit gateways, sensors, templates, commands |
| `viewer` | Read-only | View all data |

Dev superadmin: `iotteam@internal.local` / `iotteam2024`

---

## Device provisioning

At flash time `write-to-database.sh` is called with hostname, register_key, maintain_key.
Set these environment variables on the flash machine:
```
ENTERPRISE_DB_HOST=cloud-vm:5432
ENTERPRISE_DB_USER=qubeadmin
ENTERPRISE_DB_PASS=qubepass
ENTERPRISE_DB_NAME=qubedb
```

On first boot conf-agent reads `/boot/mit.txt`:
```yaml
deviceid: Qube-1302
devicename: Qube-1302
devicetype: rasp4
register: 4D4L-R4KY-ZTQ5
maintain: KC3L-T7XT-7T7E
```

Calls `POST /v1/device/register` → polls every 60s until customer claims →
receives QUBE_TOKEN → saves to `/opt/qube/.env` → begins normal sync.

---

## Images to build and push to GitLab registry

| Image | Arch | Notes |
|-------|------|-------|
| `enterprise-cloud-api` | amd64 | Runs on cloud VM |
| `enterprise-conf-agent` | arm64 | Runs on every Qube from boot |
| `enterprise-influx-to-sql` | arm64 | Runs on every Qube from boot |
| `mqtt-gateway` | arm64 | Auto-deployed when user adds MQTT gateway |

Existing images (already in registry, use as-is):
`modbus-gateway`, `opc-ua-gateway`, `snmp-gateway`

---

## Environment variables

### Cloud VM
```
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
JWT_SECRET=<strong-random-secret>
```

### Qube device — /opt/qube/.env
```
TPAPI_URL=https://cloud.yourcompany.com:8081
QUBE_ID=Qube-1302
QUBE_TOKEN=<auto-obtained via self-registration>
WORK_DIR=/opt/qube
POLL_INTERVAL=30
```

### enterprise-influx-to-sql — configs.yml
```yaml
InfluxDB:
  URL: http://influxdb:8086
  DB: qube-db        # must match core-switch config
TPAPI:
  URL: http://cloud:8081
  QubeID: Qube-1302
  QubeToken: <same as QUBE_TOKEN>
```

---

## VM test (2 Multipass VMs)

```bash
multipass launch --name cloud-vm --cpus 2 --memory 2G --disk 10G
multipass launch --name qube-vm  --cpus 2 --memory 2G --disk 10G

CLOUD_IP=$(multipass info cloud-vm | grep IPv4 | awk '{print $2}')

multipass transfer -r qube-enterprise/ cloud-vm:/home/ubuntu/qube-enterprise
multipass exec cloud-vm -- bash /home/ubuntu/qube-enterprise/scripts/setup-cloud.sh

multipass transfer -r qube-enterprise/ qube-vm:/home/ubuntu/qube-enterprise
multipass exec qube-vm -- bash /home/ubuntu/qube-enterprise/scripts/setup-qube.sh $CLOUD_IP
```

---

## Implementation status

| Phase | Feature | Status |
|-------|---------|--------|
| 1 | Schema, auth, claiming, commands, heartbeat | ✅ |
| 2 | Gateways, sensors, templates, CSV gen, telemetry | ✅ |
| 3 | Self-registration from mit.txt, register_key claim | ✅ |
| 3 | Sensor row CRUD, template register PATCH | ✅ |
| 3 | Correct CSV formats for all 4 protocols | ✅ |
| 3 | Docker Swarm compose generation | ✅ |
| 3 | MQTT gateway | ✅ |
| 4 | enterprise-influx-to-sql (InfluxDB v1 → Postgres) | ✅ |
| 5 | Keycloak JWT integration | 🔲 Future |
| 5 | GitLab CI/CD pipelines | 🔲 Future |
| 5 | Grafana auto-provisioning | 🔲 Out of scope |
