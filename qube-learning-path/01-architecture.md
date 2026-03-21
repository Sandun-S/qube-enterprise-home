# 01 — Architecture: What Qube Enterprise Does

## The one-sentence version

Qube Enterprise lets an IoT team manage many edge devices from one cloud dashboard — adding sensors, updating configs, and reading data — without ever SSH-ing into a device.

---

## The problem it solves

Before Enterprise, when you wanted to add a new Modbus sensor to a Qube:
1. SSH into the device
2. Manually edit a CSV file
3. Restart the gateway container
4. Manually configure where to send the data

For 1 device that's annoying. For 100 devices that's impossible.

Enterprise automates all of this. You make one API call from a frontend, and within 30 seconds every affected Qube has the right CSV files and the right containers running.

---

## The layers

```
┌─────────────────────────────────────────────┐
│  FRONTEND / IoT TEAM                        │
│  Makes REST API calls to Cloud API :8080    │
└───────────────┬─────────────────────────────┘
                │ JWT auth
┌───────────────▼─────────────────────────────┐
│  CLOUD API  (cloud/internal/api/)           │
│  Manages orgs, qubes, gateways, sensors     │
│  Stores everything in Postgres              │
└───────────────┬─────────────────────────────┘
                │ writes config hash
┌───────────────▼─────────────────────────────┐
│  TP-API  (cloud/internal/tpapi/)            │
│  Qube-facing only — HMAC auth               │
│  Qubes poll this to get their config        │
└───────────────┬─────────────────────────────┘
                │ HTTP over network
┌───────────────▼─────────────────────────────┐
│  QUBE DEVICE (arm64 Raspberry Pi)           │
│                                             │
│  enterprise-conf-agent                      │
│    reads /boot/mit.txt                      │
│    polls TP-API every 30s                   │
│    writes docker-compose.yml + CSVs         │
│    runs docker stack deploy                 │
│                                             │
│  modbus-gateway / mqtt-gateway / etc        │
│    reads config.csv                         │
│    polls physical device                    │
│    sends data to core-switch                │
│                                             │
│  core-switch → influxdb-relay → InfluxDB    │
│    (existing Qube Lite, unchanged)          │
│                                             │
│  enterprise-influx-to-sql                   │
│    reads InfluxDB                           │
│    maps to sensor_id via sensor_map.json    │
│    POSTs to TP-API /telemetry/ingest        │
└─────────────────────────────────────────────┘
```

---

## Key design decisions and why

**Why two ports (8080 and 8081)?**
The Cloud API (8080) is for humans and frontends — uses JWT. The TP-API (8081) is for Qubes only — uses HMAC. Separating them means you can firewall port 8081 so only Qubes can reach it, while the dashboard uses 8080 normally.

**Why poll instead of push?**
Qubes live behind firewalls and NAT. The cloud can't reach them directly. So Qubes poll the cloud every 30 seconds asking "has my config changed?" The cloud says yes/no based on a hash. This is reliable even on bad network connections.

**Why a hash?**
Instead of syncing all the config data every 30 seconds (expensive), the Qube just fetches a short hash string. If the hash matches what it already has, it does nothing. Only if the hash changes does it download the full config. This keeps traffic minimal.

**Why Docker Swarm instead of plain Docker Compose?**
Swarm gives you `docker service update --force` to restart services, `docker service logs` to get logs remotely, and proper service naming (`qube_panel-a` instead of just `panel-a`). It's also already set up on every Qube from the Qube Lite installation.

**Why InfluxDB in the middle?**
The existing Qube Lite pipeline writes to InfluxDB. Enterprise reads from InfluxDB and forwards to Postgres. This means Enterprise doesn't replace or break anything — it just reads what's already there.

---

## The data flow for one reading

```
Physical meter
  └─ Modbus TCP → modbus-gateway reads register 3000
       └─ POST /v3/batch to core-switch
            └─ core-switch writes to influxdb-relay:9096
                 └─ influxdb-relay fans out to influxdb:8086
                      └─ enterprise-influx-to-sql queries InfluxDB every 60s
                           └─ looks up "Main_Meter.active_power_w" in sensor_map.json
                                └─ gets sensor UUID
                                     └─ POST /v1/telemetry/ingest to TP-API
                                          └─ Postgres sensor_readings table
                                               └─ GET /api/v1/data/sensors/:id/latest
                                                    └─ Frontend dashboard
```

---

## Files to look at first

- `cloud/migrations/001_init.sql` — the database schema tells you everything the system stores
- `cloud/internal/api/router.go` — every API endpoint in one place
- `conf-agent/main.go` — the whole Qube-side automation in one file
- `cloud/internal/tpapi/sync.go` — how the config gets generated and delivered to Qubes
