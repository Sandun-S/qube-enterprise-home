# Qube Enterprise — Architecture Bridge

## The Real Qube Lite Components (GitHub files)

These are the production services running on every Qube Lite device today.
**Do not modify these.** The Enterprise system works alongside them.

| Service | Where | What it does |
|---|---|---|
| `conf-agent` | On Qube | Polls `conf-api` for shell commands. Executes them. Handles reboot, shutdown, file transfer, service management via `service_add/edit/rm.sh` |
| `conf-api` | Cloud | MySQL-backed command queue. Sends commands to devices. Tracks online/offline. |
| `tp-api` (Lite) | Cloud | JWT-authenticated client API for service add/edit/delete. Queues `PUT_FILE` + `service_add.sh` commands via conf-api. |
| `core-switch` | On Qube | Receives JSON from gateways via HTTP. Routes to InfluxDB v1 (line protocol) or MQTT. |
| `influx-to-sql` | On Qube | Reads InfluxDB v1 by device UUID list (`uploads.csv`). Aggregates and writes to Postgres/MySQL. |

## The Qube Enterprise Components (this repo)

These are NEW or EXTENDED components that add the IoT fleet automation layer.

| Service | Where | What it does |
|---|---|---|
| `cloud` (Cloud API + TP-API) | Cloud | All org/qube/gateway/sensor/template management. Hash-based config sync. |
| `conf-agent` (Enterprise) | On Qube | Extended: adds hash-based config sync from Enterprise TP-API. Still compatible with existing patterns. |
| `enterprise-influx-to-sql` | On Qube | NEW: reads InfluxDB v1 (same as existing coreswitch output), maps via `sensor_map.json` to sensor UUIDs, POSTs to Enterprise TP-API. |
| `mqtt-gateway` | On Qube | NEW Docker service for MQTT protocol. Reads `topics.csv`, subscribes, sends to coreswitch. |
| Postgres | Cloud | Enterprise data store: orgs, qubes, gateways, sensors, templates, sensor_readings. |

## How They Fit Together

```
CLOUD SIDE
──────────
Frontend / API client
    │
    ▼
Enterprise Cloud API (:8080)    ←── users manage gateways/sensors/templates here
    │
    ▼
Enterprise Postgres             ←── all state lives here
    │
    ▼
Enterprise TP-API (:8081)       ←── Qube-facing only (HMAC auth)
    │
    │  poll every 30s (hash-based sync)
    ▼
QUBE SIDE
─────────
Enterprise Conf-Agent           ←── sees hash changed → downloads docker-compose.yml + CSV files
    │
    ▼
docker stack deploy             ←── gateway containers start/update
    │
    ▼
[Modbus/MQTT/SNMP/OPC-UA gateway containers]
    │   reads CSV config
    │   polls devices
    ▼
Core-Switch (:8080)             ←── UNCHANGED from Qube Lite
    │
    ├──▶ InfluxDB v1 (output=influxdb)
    │
    └──▶ MQTT broker  (output=mqtt)
             │
             ▼
Enterprise influx-to-sql        ←── NEW: reads InfluxDB v1, maps via sensor_map.json
    │
    ▼
Enterprise TP-API /v1/telemetry/ingest
    │
    ▼
Postgres sensor_readings        ←── queryable via Cloud API /api/v1/data/readings
```

## Key Design Decisions

### 1. InfluxDB Version: v1 (not v2)
The existing core-switch writes to InfluxDB v1 using the HTTP write API:
```
POST /write?db=edgex
Body: {Table},device={Equipment},reading={Reading},{Tags} value={Value} {Time}
```
We use `influxdb:1.8` in docker-compose.dev.yml to match this exactly.
The enterprise-influx-to-sql queries InfluxDB v1 with standard InfluxQL.

### 2. sensor_map.json — The Bridge
When you add a sensor via the Enterprise API, the cloud generates a `sensor_map.json`:
```json
{
  "UPS_Main.Battery_Voltage": "uuid-sensor-001",
  "UPS_Main.Battery_Runtime": "uuid-sensor-002"
}
```
Format: `{Equipment.Reading: sensor_uuid}` — exactly matching the InfluxDB tags.

This file is written by the Enterprise Conf-Agent into `/opt/qube/sensor_map.json`
when it syncs config. The enterprise-influx-to-sql reads it to resolve sensor UUIDs.

### 3. Auth Systems
- **Qube Lite conf-agent ↔ conf-api**: HMAC with fixed keys `#mit_Agent~client` / `#mit_Portal~server`
- **Enterprise conf-agent ↔ TP-API**: HMAC-SHA256(`qube_id:org_secret`)  
- **User ↔ Enterprise Cloud API**: JWT (HS256, 24h expiry)
- **Qube Lite tp-api ↔ clients**: JWT from `API_Tokens` MySQL table

These are completely separate auth systems running in parallel.

### 4. Database Systems
- **Qube Lite**: MySQL — `Devices`, `Device_Commands`, `Device_Services`, `Service_Versions`, `API_Tokens`
- **Enterprise**: PostgreSQL — `organisations`, `users`, `qubes`, `gateways`, `sensors`, `sensor_templates`, `services`, `service_csv_rows`, `sensor_readings`

They share no tables. They can coexist on separate DBs.

### 5. CSV Generation Path

When a user adds a sensor via Enterprise API:
1. `POST /api/v1/gateways/{id}/sensors` → sensor + CSV rows saved to Postgres
2. Config hash recalculated → stored in `config_state`
3. Enterprise Conf-Agent polls `/v1/sync/state` → hash mismatch
4. Downloads `/v1/sync/config` → gets `docker_compose_yml` + `csv_files` + `sensor_map`
5. Writes: `/opt/qube/configs/{service-name}/config.csv`
6. Writes: `/opt/qube/sensor_map.json`
7. Runs: `docker compose up -d`
8. Gateway container restarts with new CSV
9. Starts polling new device registers / MQTT topics

This is the "zero-touch Qube" flow. No SSH. No manual file edits.

## What You Do NOT Need to Change on the Qube

- `core-switch` — unchanged, works as-is
- The existing `conf-agent` (Qube Lite) — still handles reboot/shutdown/manual commands
- `influx-to-sql` (original) — still works for manual uploads.csv deployments

You ADD the Enterprise Conf-Agent and enterprise-influx-to-sql alongside them.

## Configuration Reference

### Enterprise Conf-Agent (`/opt/qube/.env`)
```
TPAPI_URL=http://<cloud-ip>:8081
QUBE_ID=Q-1001
QUBE_TOKEN=<from POST /api/v1/qubes/claim>
WORK_DIR=/opt/qube
POLL_INTERVAL=30
```

### Enterprise influx-to-sql (`configs.yml`)
```yaml
Service:
  PollInterval: 60
  LookbackMins: 5
  SensorMapPath: /config/sensor_map.json
InfluxDB:
  URL: http://127.0.0.1:8086
  DB: edgex              # must match core-switch InfluxDB DB name
  Tables: [Measurements] # must match Table field from gateway CSVs
TPAPI:
  URL: http://<cloud-ip>:8081
  QubeID: Q-1001
  QubeToken: <same token>
```

## Running Locally (Docker Compose)

```bash
# 1. Start full stack
docker compose -f docker-compose.dev.yml up -d

# 2. Open test UI
open http://localhost:8888

# 3. Register org, claim Q-1001, copy token

# 4. Set token in docker-compose.dev.yml (conf-agent + enterprise-influx-to-sql env)
# Then restart:
docker compose -f docker-compose.dev.yml up -d conf-agent enterprise-influx-to-sql

# 5. Add a gateway + sensor via test UI
# 6. Watch conf-agent logs:
docker compose -f docker-compose.dev.yml logs -f conf-agent

# 7. Simulate sensor data hitting core-switch:
curl -X POST http://localhost:8080/v3/data \
  -H 'Content-Type: application/json' \
  -d '{"Table":"Measurements","Equipment":"Main_Meter","Reading":"active_power_w","Output":"influxdb","Value":"1250.5","Time":0}'
# (use :8080 if coreswitch is running, otherwise data goes directly to enterprise-influx-to-sql test mode)

# 8. Check readings:
TOKEN="..."
SENSOR_ID="..."
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/data/sensors/$SENSOR_ID/latest"
```

## TODO / Phase 3+

- [ ] Modbus TCP gateway container (standardise to read `config.csv` format)
- [ ] SNMP gateway container update (read `config.csv`)  
- [ ] OPC-UA gateway container (same pattern)
- [ ] influx-to-sql integration with existing Qube Lite MySQL (bridge both systems)
- [ ] Grafana dashboard auto-provisioning from `ui_mapping_json`
- [ ] Multi-Qube bulk config push
- [ ] Audit log for all Enterprise API mutations

---

## Docker Swarm vs Docker Compose

### Real Qube (production)
The real Qube runs Docker Swarm with a single node:
```bash
docker swarm init
docker network create --driver overlay --attachable qube-net
docker stack deploy -c docker-compose.yml qube
```

Services are named `qube_<service>` e.g. `qube_core-switch`, `qube_panel-a-modbus`.

The Enterprise conf-agent detects swarm mode automatically:
- Swarm active → `docker stack deploy -c /opt/qube/docker-compose.yml qube`
- Swarm not active (dev/test) → `docker compose up -d`

The generated `docker-compose.yml` uses:
- `networks: qube-net: external: true` — the pre-existing swarm overlay network
- `deploy: replicas: 1` and `restart_policy: condition: any`
- Host path volumes: `/opt/qube/configs/<service>/configs.yml`

### Dev (local testing)
`docker-compose.dev.yml` uses bridge networking, no swarm.
Conf-agent auto-detects no swarm and uses `docker compose up -d`.

### Setup on a new Qube before first conf-agent run
```bash
# One-time setup on a new Qube Enterprise device
docker swarm init
docker network create --driver overlay --attachable qube-net

# Create work directory
mkdir -p /opt/qube/configs
mkdir -p /opt/qube/influxdb-relay

# Set credentials
cat > /opt/qube/.env << ENV
TPAPI_URL=https://cloud.yourcompany.com:8081
QUBE_ID=Q-xxxx
QUBE_TOKEN=<from claiming>
WORK_DIR=/opt/qube
POLL_INTERVAL=30
ENV

# Start enterprise conf-agent (it will deploy everything else automatically)
/usr/local/bin/qube-conf-agent &
```
