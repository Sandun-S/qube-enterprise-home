# Qube Enterprise v2 — UI & API Guide

This document explains how the test UI (`test-ui/index.html`) works, which API calls it makes,
and how the v2 data model fits together.

---

## The v2 Data Model

```
Protocols         Reader Templates       Device Templates
──────────────    ──────────────────     ────────────────────────
modbus_tcp  ──►  Modbus TCP Reader  ──►  Janitza UMG-96RM
  (global)         image_suffix:            protocol: modbus_tcp
                   modbus-reader            sensor_config: {registers}
                   connection_schema:       sensor_params_schema: {unit_id}
                   {host, port, ...}

snmp        ──►  SNMP Reader        ──►  APC Smart-UPS
                                          Vertiv ITA2
```

**Protocol** — defines that modbus_tcp exists. Renders in dropdowns.
**Reader Template** — defines the container image and connection form (superadmin managed).
**Device Template** — defines what a specific device type reports (org or global).
**Reader** — one running container on a Qube (one per protocol/connection endpoint).
**Sensor** — one device instance on a reader. Config_json = template sensor_config + user params.
**Container** — auto-created alongside each reader. Deployed by conf-agent via Docker Swarm.

---

## Test UI — http://localhost:8888

### Config tab

Set the API base URL. Clicking "Check health" calls:
```
GET /health → {"status":"ok","service":"cloud-api","version":"2"}
```

---

### Auth tab

**Register:**
```
POST /api/v1/auth/register
{"org_name":"...","email":"...","password":"..."}
→ {"token":"<jwt>","role":"admin","org_id":"..."}
```

**Login:**
```
POST /api/v1/auth/login
{"email":"...","password":"..."}
→ {"token":"<jwt>","role":"..."}
```

The token is stored in the UI and sent as `Authorization: Bearer <token>` on all subsequent calls.

---

### Qubes tab

**List qubes:**
```
GET /api/v1/qubes
→ [{id, status, location_label, config_version, config_hash, ws_connected, last_seen}]
```

**Claim a qube:**
```
POST /api/v1/qubes/claim
{"register_key":"TEST-Q1001-REG"}
→ {"qube_id":"Q-1001","auth_token":"<hmac>"}
```

The `auth_token` is the HMAC token conf-agent uses for TP-API calls.

**Update location:**
```
PUT /api/v1/qubes/:id
{"location_label":"Server Room A"}
→ {"message":"qube updated"}
```

**Send command:**
```
POST /api/v1/qubes/:id/commands
{"command":"ping","payload":{}}
→ {"command_id":"<uuid>","delivered_via":"websocket"|"queue"}
```

Valid commands (28 total, grouped):

| Category | Commands |
|----------|----------|
| Container/Config | `ping`, `restart_qube`, `reboot`, `shutdown`, `restart_reader`, `stop_container`, `reload_config`, `update_sqlite`, `get_logs`, `list_containers` |
| Network | `reset_ips`, `set_eth`, `set_wifi`, `set_firewall` |
| Identity/System | `get_info`, `set_name`, `set_timezone` |
| Backup/Restore | `backup_data`, `restore_data` |
| Maintenance | `repair_fs`, `backup_image`, `restore_image` |
| Services | `service_add`, `service_rm`, `service_edit` |
| File Transfer | `put_file`, `get_file` |

See TESTING.md §11 for full payload reference per command.

---

### Protocols tab

```
GET /api/v1/protocols
→ [{id, label, description, reader_standard}]
```

---

### Reader Templates tab

```
GET /api/v1/reader-templates
→ [{id, protocol, name, image_suffix, connection_schema, env_defaults}]
```

`connection_schema` is a JSON Schema used to render the reader creation form.

Superadmin CRUD:
```
POST   /api/v1/reader-templates     (superadmin)
PUT    /api/v1/reader-templates/:id (superadmin)
DELETE /api/v1/reader-templates/:id (superadmin)
```

---

### Device Templates tab

```
GET /api/v1/device-templates
→ [{id, protocol, name, manufacturer, model, sensor_config, is_global}]
```

Both org-level and global (superadmin) templates appear here. Global templates are visible
to all orgs but can only be edited by superadmin.

Create:
```
POST /api/v1/device-templates
{
  "name": "Janitza UMG-96RM",
  "protocol": "modbus_tcp",
  "manufacturer": "Janitza",
  "model": "UMG-96RM",
  "sensor_config": {
    "registers": [
      {"name":"active_power_w","address":1294,"type":"float32","unit":"W"}
    ]
  },
  "sensor_params_schema": {
    "type":"object",
    "properties": {"unit_id":{"type":"integer","default":1}}
  }
}
→ {"id":"<uuid>","is_global":false,"message":"..."}
```

Patch sensor_config:
```
PATCH /api/v1/device-templates/:id/config
{"sensor_config": {...updated registers/OIDs...}}
```

---

### Readers tab

Select a Qube, then create readers (one per protocol endpoint):

```
POST /api/v1/qubes/:id/readers
{
  "name": "Main PLC Reader",
  "protocol": "modbus_tcp",
  "template_id": "<reader_template_uuid>",
  "config_json": {"host":"192.168.10.1","port":502,"poll_interval_sec":20}
}
→ {"reader_id":"<uuid>","container_id":"<uuid>","service_name":"main-plc-reader","new_hash":"..."}
```

A container row is auto-created. conf-agent deploys it on next sync.

List readers:
```
GET /api/v1/qubes/:id/readers
→ [{id, name, protocol, config_json, status, sensor_count, version}]
```

Update reader:
```
PUT /api/v1/readers/:reader_id
{"config_json":{"host":"192.168.10.2","port":502}}
→ {"message":"reader updated","new_hash":"..."}
```

Delete (cascades sensors + container):
```
DELETE /api/v1/readers/:reader_id
→ {"deleted":true,"new_hash":"..."}
```

---

### Sensors tab

Select a reader, then add sensors:

```
POST /api/v1/readers/:reader_id/sensors
{
  "name": "Main Energy Meter",
  "template_id": "<device_template_uuid>",  // optional
  "params": {"unit_id": 1},                 // merged with template sensor_config
  "tags_json": {"location":"MDB","phase":"3P"},
  "output": "influxdb",                     // "influxdb" | "live" | "influxdb,live"
  "table_name": "Measurements"              // InfluxDB measurement name
}
→ {"sensor_id":"<uuid>","new_hash":"...","message":"..."}
```

When `template_id` is provided, `sensor_config` from the template is merged with `params`:
- Template registers/OIDs define **what** to read
- User `params` define **which device** (unit_id, ip_address, community, etc.)

List sensors:
```
GET /api/v1/readers/:reader_id/sensors
→ [{id, name, template_id, template_name, config_json, tags_json, output, table_name, status}]

GET /api/v1/qubes/:id/sensors   // all sensors across all readers on a qube
```

Update sensor:
```
PUT /api/v1/sensors/:sensor_id
{"tags_json":{...}, "output":"influxdb,live"}
→ {"message":"sensor updated","new_hash":"..."}
```

---

### Containers tab

Containers are auto-managed (created with reader, removed with reader). View only:

```
GET /api/v1/qubes/:id/containers
→ [{id, reader_id, name, image, env_json, status}]
```

The `image` field shows the fully resolved container image (from registry settings + reader template).

---

### Sync tab (TP-API simulation)

**Sync state:**
```
GET /v1/sync/state
Headers: X-Qube-ID, Authorization: Bearer <qube_token>
→ {"qube_id":"Q-1001","hash":"...","config_version":5,"updated_at":"..."}
```

**Sync config** (what conf-agent downloads when hash changes):
```
GET /v1/sync/config
→ {
    "hash": "...",
    "config_version": 5,
    "docker_compose_yml": "version: \"3.8\"\nservices:\n...",
    "readers": [{id, name, protocol, config_json, sensors:[...]}],
    "containers": [{id, name, image, env_json}],
    "coreswitch_settings": {"outputs":{"influxdb":true,"live":false}},
    "telemetry_settings": [...]
  }
```

The `docker_compose_yml` is deployed by conf-agent via `docker stack deploy`.
The `readers` + `sensors` arrays are written to SQLite by conf-agent.

---

### Telemetry tab

**Ingest (simulates enterprise-influx-to-sql):**
```
POST /v1/telemetry/ingest
Headers: X-Qube-ID, Authorization: Bearer <qube_token>
{
  "readings": [
    {"time":"2026-03-29T10:00:00Z","sensor_id":"<uuid>","field_key":"active_power_w","value":1250.5,"unit":"W"}
  ]
}
→ {"inserted":1,"failed":0,"total":1}
```

**Query (Cloud API):**
```
GET /api/v1/data/sensors/:id/latest
→ {"sensor_id":"...","sensor_name":"...","fields":[{field_key,value,unit,time}]}

GET /api/v1/data/readings?sensor_id=<uuid>&field=active_power_w&from=2026-03-28T00:00:00Z
→ {"sensor_id":"...","count":24,"readings":[{time,field_key,value,unit}]}
```

---

### Registry tab (superadmin only)

Controls which container registry reader images are pulled from:

```
GET /api/v1/admin/registry
→ {"mode":"github","github_base":"ghcr.io/sandun-s/...","gitlab_base":"...","images":{...}}

PUT /api/v1/admin/registry
{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}
→ {"updated":2,"settings":{...}}
```

Modes: `github` (uses github_base + image_suffix), `gitlab` (per-image overrides), `local` (bare image_suffix).

---

## Auth flows summary

| Who | Header | Where |
|-----|--------|-------|
| Frontend / dev | `Authorization: Bearer <jwt>` | Port 8080 |
| conf-agent (WS) | `?token=<qube_token>` on WS URL | Port 8080 /ws |
| conf-agent (HTTP) | `X-Qube-ID` + `Authorization: Bearer <qube_token>` | Port 8081 |
| influx-to-sql | `X-Qube-ID` + `Authorization: Bearer <qube_token>` | Port 8081 |
