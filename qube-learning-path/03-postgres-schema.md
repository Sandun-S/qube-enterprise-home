# 03 — Postgres Schema: What's Stored and Why

The schema is in `cloud/migrations/`. It runs automatically when Postgres starts (migrations are mounted as `docker-entrypoint-initdb.d`).

---

## The tables and their relationships

```
organisations
    └── users (many per org)
    └── qubes (many per org, after claiming)
         └── config_state (one per qube)
         └── qube_commands (many per qube)
         └── gateways (many per qube)
              └── services (one per gateway)
                   └── service_csv_rows (many per service)
                   └── sensors (many per gateway)
                        └── sensor_readings (many per sensor)

sensor_templates (global or org-scoped)
    └── sensors reference templates
```

---

## Table by table

### `organisations`
```sql
CREATE TABLE organisations (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    org_secret  TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
`org_secret` is auto-generated random hex. It's used to compute HMAC tokens for Qubes. Each org's Qubes get tokens derived from this secret — so you can revoke access to all Qubes of an org by changing the secret.

### `users`
```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id        UUID NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('superadmin', 'admin', 'editor', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
Password stored as bcrypt hash using pgcrypto `crypt()`. The `ON DELETE CASCADE` means deleting an org deletes all its users automatically.

### `qubes`
```sql
CREATE TABLE qubes (
    id              TEXT PRIMARY KEY,        -- e.g. "Q-1001" or "Qube-1302"
    org_id          UUID REFERENCES organisations(id) ON DELETE SET NULL,
    auth_token_hash TEXT,                    -- HMAC token for TP-API auth
    register_key    TEXT UNIQUE,             -- customer enters this to claim
    maintain_key    TEXT,                    -- IoT team maintenance access
    device_type     TEXT NOT NULL DEFAULT 'arm64',
    status          TEXT NOT NULL DEFAULT 'unclaimed',
    location_label  TEXT NOT NULL DEFAULT '',
    claimed_at      TIMESTAMPTZ,
    last_seen       TIMESTAMPTZ,
    config_version  INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
`id` is TEXT not UUID because real Qube IDs are human-readable like `Qube-1302`. `org_id` is NULL until a customer claims the device — that's how you tell an unclaimed device from a claimed one.

### `config_state`
```sql
CREATE TABLE config_state (
    qube_id      TEXT PRIMARY KEY REFERENCES qubes(id) ON DELETE CASCADE,
    hash         TEXT NOT NULL DEFAULT '',
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
One row per Qube. The `hash` is a SHA-256 of the current config. Every time you add a gateway or sensor, `recomputeConfigHash()` recalculates this. The Qube polls for this hash — if it changes, it downloads the new config.

**Why a separate table?** So you can update the hash without touching the qube record itself, avoiding lock contention.

### `gateways`
```sql
CREATE TABLE gateways (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id       TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,       -- user-given name e.g. "Panel_A"
    protocol      TEXT NOT NULL,       -- modbus_tcp / opcua / snmp / mqtt
    host          TEXT NOT NULL,       -- IP address or URL
    port          INT NOT NULL DEFAULT 0,
    config_json   JSONB NOT NULL DEFAULT '{}', -- protocol-specific settings
    service_image TEXT NOT NULL,       -- Docker image to deploy
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
Each gateway = one Docker container on the Qube. `config_json` stores protocol-specific settings like `{"community":"public","version":"2c"}` for SNMP.

### `services`
```sql
CREATE TABLE services (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    gateway_id  UUID UNIQUE REFERENCES gateways(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,   -- sanitized name e.g. "panel-a" — used in compose
    image       TEXT NOT NULL,   -- Docker image
    port        INT NOT NULL DEFAULT 0,
    env_json    JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
`gateway_id UNIQUE` means exactly one service per gateway. `name` becomes the Docker service name — `docker service ls` shows `qube_panel-a`.

### `sensors`
```sql
CREATE TABLE sensors (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    gateway_id     UUID NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,        -- e.g. "Main_Meter"
    template_id    UUID NOT NULL REFERENCES sensor_templates(id),
    address_params JSONB NOT NULL DEFAULT '{}',  -- e.g. {"unit_id":1}
    tags_json      JSONB NOT NULL DEFAULT '{}',  -- e.g. {"location":"panel_a"}
    status         TEXT NOT NULL DEFAULT 'active',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
A sensor is an instance of a template. `address_params` customises the template for this specific device — the register offset, Modbus unit ID, OPC-UA node override etc.

### `sensor_templates`
```sql
CREATE TABLE sensor_templates (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id            UUID REFERENCES organisations(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,      -- e.g. "Schneider PM5100"
    protocol          TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    config_json       JSONB NOT NULL DEFAULT '{}',   -- register map / OID list / node map
    influx_fields_json JSONB NOT NULL DEFAULT '{}',  -- display labels and units
    ui_mapping_json   JSONB NOT NULL DEFAULT '{}',
    is_global         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
`is_global=TRUE` means the IoT team owns it — visible to all orgs. `is_global=FALSE` means it belongs to one org. `config_json` is the heart — for Modbus it contains the register list:
```json
{
  "registers": [
    {"address": 3000, "register_type": "Holding", "data_type": "uint16",
     "field_key": "active_power_w", "table": "Measurements"}
  ]
}
```

### `service_csv_rows`
```sql
CREATE TABLE service_csv_rows (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    sensor_id  UUID REFERENCES sensors(id) ON DELETE CASCADE,
    csv_type   TEXT NOT NULL CHECK (csv_type IN (
                   'registers', 'devices', 'topics', 'oids', 'nodes')),
    row_data   JSONB NOT NULL DEFAULT '{}',
    row_order  INT NOT NULL DEFAULT 0
);
```
This is the "expanded" version of the template config. When you add a sensor, `generateCSVRows()` reads the template's `config_json` and creates one row here per register/node/OID. When conf-agent syncs, these rows get assembled into the actual CSV file.

**Why store rows individually?** So you can fix one row (wrong register address) without regenerating everything. `PUT /api/v1/sensors/:id/rows/:row_id` updates just one row.

### `sensor_readings`
```sql
CREATE TABLE sensor_readings (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sensor_id  UUID NOT NULL REFERENCES sensors(id) ON DELETE CASCADE,
    field_key  TEXT NOT NULL,      -- e.g. "active_power_w"
    value      DOUBLE PRECISION NOT NULL,
    unit       TEXT NOT NULL DEFAULT '',
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_readings_sensor_time ON sensor_readings(sensor_id, recorded_at DESC);
```
The telemetry table. Each reading is one value for one field of one sensor. The index on `(sensor_id, recorded_at DESC)` makes "latest value" queries fast.

---

## How a sensor reading gets into this table

```
1. User adds sensor via Cloud API
   → service_csv_rows populated
   → config_state hash updated

2. Conf-agent detects hash change (30s poll)
   → downloads sync/config → gets CSV rows + sensor_map
   → writes /opt/qube/configs/panel-a/config.csv
   → writes /opt/qube/sensor_map.json  {"Main_Meter.active_power_w": "uuid-of-sensor"}
   → docker stack deploy

3. modbus-gateway reads config.csv
   → polls Modbus register 3000 every 5s
   → POST /v3/batch to core-switch with measurement=Main_Meter field=active_power_w value=1250.5

4. core-switch → influxdb-relay → InfluxDB v1 (qube-db database)
   Measurements table, device=Main_Meter, reading=active_power_w, value=1250.5

5. enterprise-influx-to-sql queries InfluxDB every 60s
   → finds new rows in Measurements
   → looks up "Main_Meter.active_power_w" in sensor_map.json → gets UUID
   → POST /v1/telemetry/ingest to TP-API

6. TP-API inserts into sensor_readings
   sensor_id=uuid, field_key="active_power_w", value=1250.5

7. Frontend calls GET /api/v1/data/sensors/uuid/latest
   → returns {field_key:"active_power_w", value:1250.5, unit:"W"}
```

---

## Useful queries to run directly on Postgres

```sql
-- See all claimed qubes and their status
SELECT id, org_id, status, last_seen FROM qubes WHERE org_id IS NOT NULL;

-- See all sensors across all qubes for an org
SELECT s.name, g.protocol, g.host, q.id as qube
FROM sensors s
JOIN gateways g ON g.id = s.gateway_id
JOIN qubes q ON q.id = g.qube_id
JOIN organisations o ON o.id = q.org_id
WHERE o.name = 'Your Org';

-- See latest readings for a sensor
SELECT field_key, value, unit, recorded_at
FROM sensor_readings
WHERE sensor_id = 'your-sensor-uuid'
ORDER BY recorded_at DESC
LIMIT 10;

-- Check config hash for a qube
SELECT qube_id, hash, generated_at FROM config_state WHERE qube_id = 'Q-1001';
```
