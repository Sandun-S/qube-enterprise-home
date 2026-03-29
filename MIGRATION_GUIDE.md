# Qube Enterprise v1 → v2 Migration Guide

This guide covers what changed between v1 and v2 and how to migrate existing deployments,
database data, and integrations.

---

## Summary of breaking changes

| Area | v1 | v2 | Breaking? |
|------|----|----|-----------|
| Config delivery | CSV files via HTTP | JSON via WebSocket + SQLite | Yes — conf-agent must be updated |
| API — gateways | `/api/v1/qubes/:id/gateways` | `/api/v1/qubes/:id/readers` | Yes — endpoint renamed |
| API — templates | `/api/v1/templates` | `/api/v1/device-templates` + `/api/v1/reader-templates` | Yes — split into two |
| API — sensor rows | `service_csv_rows` table, `/api/v1/gateways/:id/rows` | Removed — config lives in SQLite | Yes |
| TP-API — sync config | Returns CSV files as base64 | Returns JSON (readers, sensors, containers, compose) | Yes |
| Telemetry — format | Custom JSON `{sensor_id, field_key, value}` | SenML `{readings:[{sensor_id,field_key,value,unit,time}]}` | Yes |
| Database — telemetry | `sensor_readings` in `qubedb` (Postgres) | `sensor_readings` hypertable in `qubedata` (TimescaleDB) | Yes |
| Auth — TP-API | `HMAC(qubeID, qubeToken)` where qubeToken stored in qubes | `HMAC(qubeID, orgSecret)` where orgSecret from org | Yes — token changes |
| MQTT broker | Internal per-Qube MQTT broker container | Removed | Yes — conf-agent no longer deploys it |
| configs.yml | YAML connection config generated per gateway | Removed — JSON stored in SQLite | Yes |
| sensor_map.json | JSON file on Qube filesystem | Entries in SQLite `sensors` table | Yes — no more sensor_map.json |

---

## Step 1: Database migration

### Management database (qubedb)

Run the v2 migrations against your existing `qubedb`. These create new tables alongside old ones.

```bash
psql $DATABASE_URL -f cloud/migrations/001_init.sql
psql $DATABASE_URL -f cloud/migrations/002_global_data.sql
```

**v1 → v2 data migration script:**

```sql
-- Migrate gateways → readers
-- (run this after 001_init.sql creates the readers table)

INSERT INTO readers (id, qube_id, name, protocol, config_json, status, created_at, updated_at)
SELECT
    id,
    qube_id,
    name,
    protocol,
    jsonb_build_object(
        'host', host,
        'port', port,
        'poll_interval_sec', poll_interval_sec
    ) AS config_json,
    status,
    created_at,
    updated_at
FROM gateways;

-- Migrate sensors (update reader_id from gateway_id)
-- v2 sensors table uses reader_id instead of gateway_id
-- config_json is built from the old CSV row data
INSERT INTO sensors (id, reader_id, name, config_json, output, table_name, status, created_at)
SELECT
    s.id,
    s.gateway_id AS reader_id,
    s.name,
    s.config_json,
    'influxdb' AS output,
    'Measurements' AS table_name,
    s.status,
    s.created_at
FROM sensors_v1 s;  -- rename your old sensors table first

-- Auto-create containers for each reader
INSERT INTO containers (qube_id, reader_id, name, image)
SELECT
    r.qube_id,
    r.id AS reader_id,
    LOWER(REGEXP_REPLACE(r.name, '[^a-z0-9]', '-', 'g')) AS name,
    r.protocol || '-reader:arm64.latest' AS image
FROM readers r;

-- Recompute config hashes
-- Run this from the cloud-api process (it calls recomputeConfigHash internally)
-- Or trigger manually: UPDATE config_state SET hash='', config_version=config_version+1
UPDATE config_state SET hash='', config_version=config_version+1;
```

### Telemetry database (qubedata)

v2 uses a **separate** TimescaleDB database for telemetry. The v1 `sensor_readings` table
in `qubedb` must be migrated to the new hypertable in `qubedata`.

```bash
# Create qubedata database
psql postgres://qubeadmin:qubepass@localhost:5432/postgres \
  -c "CREATE DATABASE qubedata;"

# Run TimescaleDB init
psql postgres://qubeadmin:qubepass@localhost:5432/qubedata \
  -f cloud/migrations-telemetry/001_timescale_init.sql

# Migrate old readings
pg_dump -t sensor_readings \
  postgres://qubeadmin:qubepass@localhost:5432/qubedb \
  | psql postgres://qubeadmin:qubepass@localhost:5432/qubedata
```

---

## Step 2: Update cloud-api

Deploy the v2 `cloud-api` binary with the new environment variables:

```bash
# New required variable
TELEMETRY_DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedata?sslmode=disable

# Existing (unchanged)
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
JWT_SECRET=<same as before>
QUBE_IMAGE_REGISTRY=<same as before>
```

The v2 cloud-api starts a WebSocket server at `/ws` on port 8080. Ensure port 8080 is reachable
from Qubes (not just from the frontend).

---

## Step 3: Update conf-agent on every Qube

The v2 conf-agent uses WebSocket instead of polling, and writes SQLite instead of CSV files.

```bash
# On each Qube:
docker service update --image ghcr.io/sandun-s/qube-enterprise-home/enterprise-conf-agent:arm64.latest qube_conf-agent

# New environment variables needed:
CLOUD_WS_URL=ws://cloud.yourcompany.com:8080/ws   # new — WebSocket URL
SQLITE_PATH=/opt/qube/data/qube.db                 # new — SQLite path
# Keep:
TPAPI_URL=http://cloud.yourcompany.com:8081         # still used as fallback
WORK_DIR=/opt/qube
POLL_INTERVAL=30
```

The QUBE_TOKEN changes in v2 because the HMAC derivation changed:
- **v1**: `HMAC(qubeID, qubeToken)` where `qubeToken` was stored in the `qubes` table
- **v2**: `HMAC(qubeID, orgSecret)` where `orgSecret` is the organisation's shared secret

**Qubes will need to re-register** after migration. This happens automatically:
1. Deploy v2 conf-agent
2. conf-agent calls `POST /v1/device/register` on first boot
3. Gets new token (HMAC with orgSecret)
4. Saves to local `.env` and begins WebSocket sync

---

## Step 4: Update enterprise-influx-to-sql

```bash
# New environment variables
SQLITE_PATH=/opt/qube/data/qube.db   # reads sensor_map from SQLite, not sensor_map.json
# Keep:
TPAPI_URL=http://cloud:8081
QUBE_ID=Q-1001
QUBE_TOKEN=<updated token from step 3>
INFLUX_URL=http://127.0.0.1:8086
INFLUX_DB=edgex
```

The `sensor_map.json` file is no longer needed. The service reads sensor UUID mappings
directly from the SQLite database written by conf-agent.

---

## Step 5: Remove v1 containers from Qube stacks

v1 had `mqtt-broker` (mosquitto) in the Qube stack. Remove it:

```bash
# On each Qube:
docker service rm qube_mqtt-broker 2>/dev/null || true
```

Reader containers are now managed by conf-agent automatically based on the
readers configured in the cloud portal. The old gateway containers (`modbus-gateway`,
`snmp-gateway`, etc.) will be replaced by their v2 equivalents (`modbus-reader`, etc.)
during the first conf-agent sync.

---

## Step 6: Update any API integrations

If you have scripts or frontend code using the v1 Cloud API:

| v1 endpoint | v2 endpoint |
|-------------|-------------|
| `GET /api/v1/qubes/:id/gateways` | `GET /api/v1/qubes/:id/readers` |
| `POST /api/v1/qubes/:id/gateways` | `POST /api/v1/qubes/:id/readers` |
| `PUT /api/v1/gateways/:id` | `PUT /api/v1/readers/:reader_id` |
| `DELETE /api/v1/gateways/:id` | `DELETE /api/v1/readers/:reader_id` |
| `GET /api/v1/gateways/:id/sensors` | `GET /api/v1/readers/:reader_id/sensors` |
| `POST /api/v1/gateways/:id/sensors` | `POST /api/v1/readers/:reader_id/sensors` |
| `GET /api/v1/templates` | `GET /api/v1/device-templates` |
| `POST /api/v1/templates` | `POST /api/v1/device-templates` |
| `GET /api/v1/gateways/:id/rows` | Removed — use sync/config |
| `GET /api/v1/hash/:qube_id` | Replaced by `GET /v1/sync/state` (TP-API) |

**New v2 endpoints (no v1 equivalent):**
- `GET /api/v1/reader-templates` — reader container templates (IoT team manages)
- `GET /api/v1/qubes/:id/containers` — list auto-managed containers
- `GET /api/v1/admin/registry` / `PUT` — container registry settings
- `GET /ws` — Qube WebSocket connection
- `GET /ws/dashboard` — Dashboard WebSocket (real-time monitoring)

---

## Step 7: Telemetry ingestion format

The TP-API telemetry endpoint format changed from v1 to v2:

**v1 format:**
```json
{
  "readings": [
    {"sensor_id": "uuid", "field_key": "active_power_w", "value": 1250.5}
  ]
}
```

**v2 format (SenML-inspired):**
```json
{
  "readings": [
    {
      "time": "2026-03-29T10:00:00Z",
      "sensor_id": "uuid",
      "field_key": "active_power_w",
      "value": 1250.5,
      "unit": "W",
      "tags": {"location": "MDB"}
    }
  ]
}
```

Key additions: `time` (ISO8601, optional — defaults to now), `unit`, `tags`.
Max batch size: 5000 readings per request.

---

## Rollback plan

If v2 migration fails:

1. Keep v1 cloud-api running on a separate port or VM until migration is verified
2. v1 and v2 share the same `qubedb` management database (v2 adds new tables, doesn't drop v1 ones)
3. v1 conf-agents continue to work against v1 cloud-api
4. Roll back by redeploying v1 images and pointing `TPAPI_URL` back to the v1 instance

The only hard cutover is the timescale init (step 1 telemetry migration). Complete that
step during a maintenance window.

---

## Frequently asked questions

**Q: Do all Qubes need to be online during migration?**
A: No. Qubes will re-register and re-sync automatically when they next connect. Offline Qubes
are unaffected until they come back online.

**Q: Will old sensor readings be lost?**
A: Only if you skip the telemetry migration (step 1). Migrate `sensor_readings` from `qubedb`
to `qubedata` before switching over.

**Q: Do I need to re-add all readers and sensors?**
A: No. Run the SQL migration script in step 1 to migrate `gateways → readers` and
`sensors` with the new schema. Config hashes are reset, triggering a full re-sync.

**Q: What happens to the sensor_map.json file?**
A: It is no longer used. The mapping is maintained in SQLite's `sensors` table.
The old file can be deleted from Qube devices after migration.

**Q: Can v1 conf-agent talk to v2 cloud-api?**
A: No. The sync protocol changed (JSON vs CSV). Update conf-agent as part of the migration.
