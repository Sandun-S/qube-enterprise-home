# Qube Enterprise v2 — Developer Guide

> **Who this is for:** Frontend and integration developers building the management UI.
> Covers every API endpoint, every workflow, and exactly how to wire the UI to the backend —
> with real request/response examples taken directly from the Go source.

---

## Table of Contents

1. [System Mental Model](#1-system-mental-model)
2. [Authentication & Session](#2-authentication--session)
3. [Template System — How It Really Works](#3-template-system--how-it-really-works)
   - 3.1 The Two Template Types
   - 3.2 How sensor_config Auto-Populates
   - 3.3 The Merge: What Actually Gets Stored
   - 3.4 Displaying Defaults + Letting Users Edit
4. [Adding a Sensor (Full Lifecycle)](#4-adding-a-sensor-full-lifecycle)
   - 4.1 Step-by-Step API Calls
   - 4.2 Endpoint vs Multi-Target Protocols
   - 4.3 Reader Reuse Logic
5. [Device Catalog — Browsing & Filtering Templates](#5-device-catalog--browsing--filtering-templates)
6. [Superadmin Workflows](#6-superadmin-workflows)
   - 6.1 Adding a New Protocol
   - 6.2 Adding a New Reader Template
   - 6.3 Adding a New Global Device Template
   - 6.4 Editing a Global Template (Add/Remove Registers)
   - 6.5 Registry Configuration
7. [Fleet Management API Guide](#7-fleet-management-api-guide)
   - 7.1 List & Claim Qubes
   - 7.2 View Qube Details
   - 7.3 Update Qube Settings
8. [Reader Management API Guide](#8-reader-management-api-guide)
9. [Sensor Management API Guide](#9-sensor-management-api-guide)
10. [Telemetry API Guide](#10-telemetry-api-guide)
11. [Commands API Guide](#11-commands-api-guide)
12. [User Management API Guide](#12-user-management-api-guide)
13. [Protocol-Specific Form Reference](#13-protocol-specific-form-reference)
    - All 8 protocols — connection_schema + sensor_params_schema + sensor_config fields
14. [Config Sync — How Changes Flow to the Qube](#14-config-sync--how-changes-flow-to-the-qube)
15. [Status Monitoring](#15-status-monitoring)
16. [Complete API Quick Reference](#16-complete-api-quick-reference)

---

## 1. System Mental Model

Before touching any API, understand these relationships:

```
Organisation
  └── Qubes (edge gateway devices — Raspberry Pi / Kadas)
        └── Readers (one per protocol endpoint — e.g. "Modbus at 192.168.1.10")
              └── Sensors (one per device — e.g. "PM5100 in Rack A")
                    └── sensor.config_json (merged: template registers + user params)
```

**Key insight:** A Sensor is NOT just a single measurement point. It represents an entire physical
device and all its measurements at once. A Schneider PM5100 sensor has 6 fields
(active_power_w, voltage_ll_v, current_a, energy_kwh, power_factor, frequency_hz) all stored
in one `config_json`. This is what gets written to the Qube's SQLite database.

**Config flow:**
```
User changes sensor/reader via Cloud API
    → recomputeConfigHash() called automatically
    → config_state.hash updates in PostgreSQL
    → WebSocket push to Qube: {"type":"config_update","qube_id":"Q-1001","hash":"..."}
    → conf-agent fetches new config: GET /v1/sync/config
    → writes SQLite → restarts affected containers
    → reader container reads new SQLite → uses new config
```

Every create/update/delete of a reader or sensor triggers this chain automatically.
The UI just needs to show "Syncing..." after mutations.

---

## 2. Authentication & Session

### Register a new organisation

```http
POST http://localhost:8080/api/v1/auth/register
Content-Type: application/json

{
  "org_name": "Acme Corp",
  "email": "admin@acme.com",
  "password": "SecurePass123"
}
```

**Response `201 Created`:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "role": "admin",
  "org_id": "9f1a2b3c-..."
}
```

### Login

```http
POST http://localhost:8080/api/v1/auth/login
Content-Type: application/json

{
  "email": "admin@acme.com",
  "password": "SecurePass123"
}
```

**Response `200 OK`:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "role": "admin"
}
```

### Using the token

All subsequent requests need:
```
Authorization: Bearer <token>
```

Store the token in `localStorage`. On every page load, check if a token exists and
decode the payload to get `role`, `org_id`, `user_id`:

```js
function decodeJWT(token) {
    const payload = JSON.parse(atob(token.split('.')[1]));
    // payload: { user_id, org_id, role, exp }
    return payload;
}

// Check expiry
function isTokenExpired(token) {
    const { exp } = decodeJWT(token);
    return Date.now() / 1000 > exp;
}
```

### Get current user

```http
GET http://localhost:8080/api/v1/users/me
Authorization: Bearer <token>
```

**Response:**
```json
{
  "id": "uuid",
  "email": "admin@acme.com",
  "role": "admin",
  "org_id": "uuid"
}
```

---

## 3. Template System — How It Really Works

This is the most important section. Read it completely before building any sensor UI.

### 3.1 The Two Template Types

| Template Type | Purpose | Who Creates | Scope |
|---------------|---------|-------------|-------|
| **Reader Template** | How to run the container (Docker image, connection form) | Superadmin only | Global |
| **Device Template** | What data to collect (registers, OIDs, nodes, etc.) | Superadmin (global) or any Editor (org) | Global or Org |

**Reader Template example** (Modbus TCP):
```json
{
  "id": "abc-123",
  "protocol": "modbus_tcp",
  "name": "Modbus TCP Reader",
  "image_suffix": "modbus-reader",
  "connection_schema": {
    "type": "object",
    "properties": {
      "host":               {"type": "string",  "title": "Device IP Address", "format": "ipv4"},
      "port":               {"type": "integer", "title": "Port",              "default": 502},
      "poll_interval_sec":  {"type": "integer", "title": "Poll Interval (s)", "default": 20},
      "timeout_ms":         {"type": "integer", "title": "Timeout (ms)",      "default": 3000},
      "slave_id":           {"type": "integer", "title": "Default Slave ID",  "default": 1},
      "single_read_count":  {"type": "integer", "title": "Max Registers/Read","default": 100}
    },
    "required": ["host", "port"]
  },
  "env_defaults": {"LOG_LEVEL": "info"}
}
```

**Device Template example** (Schneider PM5100):
```json
{
  "id": "def-456",
  "protocol": "modbus_tcp",
  "name": "Schneider PM5100",
  "manufacturer": "Schneider Electric",
  "model": "PM5100",
  "is_global": true,
  "sensor_config": {
    "registers": [
      {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
      {"field_key": "voltage_ll_v",   "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0, "unit": "V"},
      {"field_key": "current_a",      "register_type": "Holding", "address": 3054, "data_type": "float32", "scale": 1.0, "unit": "A"},
      {"field_key": "energy_kwh",     "register_type": "Holding", "address": 3204, "data_type": "float32", "scale": 1.0, "unit": "kWh"},
      {"field_key": "power_factor",   "register_type": "Holding", "address": 3110, "data_type": "float32", "scale": 1.0, "unit": ""},
      {"field_key": "frequency_hz",   "register_type": "Holding", "address": 3060, "data_type": "float32", "scale": 1.0, "unit": "Hz"}
    ]
  },
  "sensor_params_schema": {
    "type": "object",
    "properties": {
      "unit_id":         {"type": "integer", "title": "Modbus Unit ID",         "default": 1,   "minimum": 1, "maximum": 247},
      "register_offset": {"type": "integer", "title": "Register Address Offset", "default": 0}
    },
    "required": ["unit_id"]
  }
}
```

### 3.2 How sensor_config Auto-Populates

When a user selects a device template, the UI MUST:

**Step A — Show the template's existing fields (pre-populated, read-only display):**

The `sensor_config` already has all the measurement definitions baked in. The user does NOT
need to enter these. Just display them so the user knows what will be measured:

```
This template will measure:
  ✓ active_power_w   (Holding reg 3000, float32, unit: W)
  ✓ voltage_ll_v     (Holding reg 3020, float32, unit: V)
  ✓ current_a        (Holding reg 3054, float32, unit: A)
  ✓ energy_kwh       (Holding reg 3204, float32, unit: kWh)
  ✓ power_factor     (Holding reg 3110, float32, unit: -)
  ✓ frequency_hz     (Holding reg 3060, float32, unit: Hz)
```

**Step B — Show the sensor_params_schema form (user MUST fill in):**

These are the device-specific parameters that differ per physical instance.
Render a form from `sensor_params_schema.properties`:

```
Unit ID *           [  1  ]    (integer, min 1, max 247)
Register Offset     [  0  ]    (integer, optional)
```

Show defaults where provided (`"default": 1` → pre-fill with 1).
Fields in `required` array get a `*` asterisk and fail validation if empty.

**Step C — Let user edit the template fields (optional, advanced mode):**

For power users, allow inline editing of the `sensor_config` fields. For example, they may
want to change `address: 3000` to `address: 3000` with an offset, or add a new register.
This should be an "Advanced / Edit fields" toggle — collapsed by default.

```js
// When user selects a device template, load it
async function loadDeviceTemplate(templateId) {
    const tmpl = await api.get(`/api/v1/device-templates/${templateId}`);
    
    // 1. Display the sensor_config fields (read-only table)
    renderSensorConfigPreview(tmpl.sensor_config, tmpl.protocol);
    
    // 2. Render the params form from sensor_params_schema
    renderSchemaForm(tmpl.sensor_params_schema, container);
    
    // 3. Store template for later merge
    state.selectedTemplate = tmpl;
}
```

### 3.3 The Merge: What Actually Gets Stored

When you call `POST /api/v1/readers/{id}/sensors`, the backend does:

```go
// From cloud/internal/api/sensors.go — mergeSensorConfig()
func mergeSensorConfig(tmplConfigRaw []byte, userParams any) map[string]any {
    var tmplConfig map[string]any           // e.g. {"registers": [...6 items...]}
    json.Unmarshal(tmplConfigRaw, &tmplConfig)

    params, _ := userParams.(map[string]any) // e.g. {"unit_id": 3, "register_offset": 0}

    // Merge: user params land alongside the registers in the same object
    for k, v := range params {
        tmplConfig[k] = v
    }
    return tmplConfig
    // Result: {"registers":[...], "unit_id": 3, "register_offset": 0}
}
```

**The final `sensor.config_json` stored in PostgreSQL and synced to SQLite:**
```json
{
  "registers": [
    {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
    {"field_key": "voltage_ll_v",   "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0, "unit": "V"},
    {"field_key": "current_a",      "register_type": "Holding", "address": 3054, "data_type": "float32", "scale": 1.0, "unit": "A"},
    {"field_key": "energy_kwh",     "register_type": "Holding", "address": 3204, "data_type": "float32", "scale": 1.0, "unit": "kWh"},
    {"field_key": "power_factor",   "register_type": "Holding", "address": 3110, "data_type": "float32", "scale": 1.0, "unit": ""},
    {"field_key": "frequency_hz",   "register_type": "Holding", "address": 3060, "data_type": "float32", "scale": 1.0, "unit": "Hz"}
  ],
  "unit_id": 3,
  "register_offset": 0
}
```

The modbus-reader sees this and knows: poll unit 3 at these 6 registers.

### 3.4 Displaying Defaults + Letting Users Edit

```js
// Render a form from sensor_params_schema with defaults pre-filled
function renderParamsForm(schema, existingValues = {}) {
    const fields = [];
    
    for (const [key, prop] of Object.entries(schema.properties || {})) {
        const currentValue = existingValues[key] ?? prop.default ?? '';
        const isRequired = (schema.required || []).includes(key);
        
        fields.push({
            key,
            title: prop.title || key,
            type: prop.type,           // "integer", "string"
            format: prop.format,       // "ipv4", "password", "uri"
            enum: prop.enum,           // for dropdowns
            default: prop.default,
            minimum: prop.minimum,
            maximum: prop.maximum,
            description: prop.description,
            required: isRequired,
            currentValue,
        });
    }
    
    return fields; // render these as input elements
}

// When user clicks "Submit", collect the params:
function collectParams(formEl, schema) {
    const result = {};
    for (const [key, prop] of Object.entries(schema.properties || {})) {
        const el = formEl.querySelector(`[name="${key}"]`);
        if (!el) continue;
        if (prop.type === 'integer') result[key] = parseInt(el.value, 10);
        else if (prop.type === 'number') result[key] = parseFloat(el.value);
        else result[key] = el.value;
    }
    return result;
}
```

**Important:** The user fills in `params`. The UI sends only the params to the API — NOT the
full sensor_config. The server merges them:

```js
// CORRECT: send template_id + params
const body = {
    name: "PM5100 Rack A",
    template_id: "def-456",          // device template UUID
    params: { unit_id: 3 },          // from sensor_params_schema form
    output: "influxdb",
    tags_json: { name: "PM5100_RackA", location: "ServerRoom" },
    table_name: "Measurements"
};

// WRONG: don't try to pre-merge and send the full config_json yourself
// Let the server do the merge
```

---

## 4. Adding a Sensor (Full Lifecycle)

### 4.1 Step-by-Step API Calls

This is the complete sequence for adding a Schneider PM5100 on Qube Q-1001 via Modbus TCP.

#### Step 1 — Load available protocols for selection UI

```http
GET /api/v1/protocols
Authorization: Bearer <token>
```

**Response:**
```json
[
  {"id": "modbus_tcp", "label": "Modbus TCP",  "description": "...", "reader_standard": "endpoint"},
  {"id": "snmp",       "label": "SNMP",         "description": "...", "reader_standard": "multi_target"},
  {"id": "mqtt",       "label": "MQTT",         "description": "...", "reader_standard": "endpoint"},
  {"id": "opcua",      "label": "OPC-UA",        "description": "...", "reader_standard": "endpoint"},
  {"id": "http",       "label": "HTTP/REST",     "description": "...", "reader_standard": "multi_target"}
]
```

→ Render as protocol selection cards. User clicks "Modbus TCP".

#### Step 2 — Load device templates for selected protocol

```http
GET /api/v1/device-templates?protocol=modbus_tcp
Authorization: Bearer <token>
```

**Response (abridged):**
```json
[
  {
    "id": "def-456",
    "protocol": "modbus_tcp",
    "name": "Schneider PM5100",
    "manufacturer": "Schneider Electric",
    "model": "PM5100",
    "is_global": true,
    "sensor_config": {
      "registers": [
        {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
        ...
      ]
    },
    "sensor_params_schema": {
      "type": "object",
      "properties": {
        "unit_id":         {"type": "integer", "title": "Modbus Unit ID", "default": 1},
        "register_offset": {"type": "integer", "title": "Register Offset",  "default": 0}
      },
      "required": ["unit_id"]
    }
  },
  ...more templates
]
```

→ Render as template cards. User clicks "Schneider PM5100".
→ Display the 6 registers (pre-populated, read-only preview).
→ Show sensor_params_schema form: Unit ID field pre-filled with default `1`.

#### Step 3 — Load reader template for connection form

```http
GET /api/v1/reader-templates?protocol=modbus_tcp
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "rt-789",
    "protocol": "modbus_tcp",
    "name": "Modbus TCP Reader",
    "image_suffix": "modbus-reader",
    "connection_schema": {
      "type": "object",
      "properties": {
        "host": {"type": "string", "title": "Device IP Address"},
        "port": {"type": "integer", "title": "Port", "default": 502},
        "poll_interval_sec": {"type": "integer", "title": "Poll Interval (s)", "default": 20},
        "timeout_ms": {"type": "integer", "title": "Timeout (ms)", "default": 3000}
      },
      "required": ["host", "port"]
    }
  }
]
```

→ Render `connection_schema` as a form. User enters `host = 192.168.1.10`, port defaults to 502.

#### Step 4 — Check if a reader already exists for this endpoint

```http
GET /api/v1/qubes/Q-1001/readers
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "rd-111",
    "name": "Rack Panel A",
    "protocol": "modbus_tcp",
    "config_json": {"host": "192.168.1.10", "port": 502, "poll_interval_sec": 20},
    "status": "active",
    "sensor_count": 2
  }
]
```

→ The UI checks: does any reader have `protocol=modbus_tcp` AND `config_json.host=192.168.1.10`?
→ If yes: offer "Reuse existing reader: Rack Panel A (192.168.1.10)" — saves a container.
→ If no: create a new reader (Step 5a). If yes: skip to Step 5b.

```js
function findExistingReader(readers, protocol, connectionConfig) {
    // For endpoint protocols: match by protocol + host
    return readers.find(r =>
        r.protocol === protocol &&
        r.config_json?.host === connectionConfig.host &&
        r.config_json?.port === connectionConfig.port
    );
}

function findMultiTargetReader(readers, protocol) {
    // For multi_target protocols: match by protocol only (one per Qube)
    return readers.find(r => r.protocol === protocol);
}
```

#### Step 5a — Create new reader (if no existing reader)

```http
POST /api/v1/qubes/Q-1001/readers
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Rack Panel A",
  "protocol": "modbus_tcp",
  "template_id": "rt-789",
  "config_json": {
    "host": "192.168.1.10",
    "port": 502,
    "poll_interval_sec": 20,
    "timeout_ms": 3000
  }
}
```

**Response `201 Created`:**
```json
{
  "reader_id": "rd-new-aaa",
  "container_id": "ct-bbb",
  "service_name": "rack-panel-a",
  "image": "ghcr.io/sandun-s/qube-enterprise-home/modbus-reader:arm64.latest",
  "new_hash": "sha256:abcdef...",
  "message": "Reader created. Conf-Agent will deploy within the next sync."
}
```

→ Note: `new_hash` changes — conf-agent will see this and pull new config.
→ A container entry was also auto-created in the `containers` table.

#### Step 5b — Use existing reader

```js
const readerId = "rd-111"; // from Step 4
```

#### Step 6 — Create the sensor

```http
POST /api/v1/readers/rd-111/sensors
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "PM5100 Rack A Main Breaker",
  "template_id": "def-456",
  "params": {
    "unit_id": 3,
    "register_offset": 0
  },
  "output": "influxdb",
  "tags_json": {
    "name": "PM5100_RackA",
    "location": "Server Room",
    "phase": "three-phase"
  },
  "table_name": "Measurements"
}
```

**Response `201 Created`:**
```json
{
  "sensor_id": "s-ccc",
  "new_hash": "sha256:ghijkl...",
  "message": "Sensor created. Config will sync to Qube SQLite."
}
```

**What happened on the server:**
1. Looked up device template `def-456` — verified protocol matches reader's protocol
2. Ran `mergeSensorConfig(template.sensor_config, params)`
3. Stored the merged result in `sensors.config_json`
4. Called `recomputeConfigHash()` → new hash → WebSocket push to Q-1001

The Qube's conf-agent will receive the push and update SQLite within seconds.

### 4.2 Endpoint vs Multi-Target Protocols

**Endpoint protocols** (Modbus TCP, OPC-UA, MQTT, LoRaWAN, DNP3):
- One reader container = one connection endpoint
- Multiple sensors share a reader IF they use the same host/port/broker
- Different endpoint → new reader + new container
- `connection_schema` fields: host, port, broker, endpoint URL etc.

**Multi-target protocols** (SNMP, HTTP, BACnet):
- One reader container handles ALL targets on the Qube
- There is exactly ONE reader per protocol per Qube
- Each sensor has its OWN target address in `params`
- `connection_schema` fields: intervals, timeouts (no host — host is per-sensor)
- `sensor_params_schema` fields: host, url, device_instance (per-sensor target)

**The server handles all this automatically** via the smart sensor endpoint (section 4.4).
For the UI, you only need to:
- Show a connection form for endpoint protocols (so user can enter host/port/broker)
- For multi_target: show a status indicator "Shared SNMP container — no connection setup needed"

### 4.3 Reader Reuse Logic (Server-Side)

The smart endpoint (`POST /api/v1/qubes/:id/sensors`) does all reader resolution server-side:

```
For multi_target protocols (SNMP, HTTP):
  → Looks for existing active reader on this Qube with matching protocol
  → If found: reuses it
  → If not found: creates one automatically with reader template defaults

For endpoint protocols (Modbus TCP, MQTT, OPC-UA):
  → Computes endpoint fingerprint from reader_config:
      Modbus: "modbus://host:port"
      MQTT:   "mqtt://broker_host:broker_port"
      OPC-UA: the full endpoint URL
  → Scans existing readers for matching fingerprint
  → If match found: reuses that reader (no new container)
  → If no match: creates new reader + container
```

The UI does NOT need to implement reader matching logic — just send `reader_config` in the
request body and let the server decide.

### 4.4 Smart Sensor Creation (Recommended Endpoint)

**Use this instead of manual reader creation for the onboarding wizard.**

```http
POST /api/v1/qubes/Q-1001/sensors
Authorization: Bearer <editor_token>
Content-Type: application/json
```

**Request body:**
```json
{
  "name": "PM5100 Rack A Main Breaker",
  "template_id": "def-456",
  "params": {
    "unit_id": 3,
    "register_offset": 0
  },
  "reader_config": {
    "host": "192.168.1.10",
    "port": 502,
    "poll_interval_sec": 20,
    "timeout_ms": 3000
  },
  "reader_name": "Rack Panel A",
  "output": "influxdb",
  "tags_json": {"name": "PM5100_RackA", "location": "Server Room"},
  "table_name": "Measurements"
}
```

**Fields:**
| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Sensor display name |
| `template_id` | Yes | Device template UUID |
| `params` | No | Per-sensor params from `sensor_params_schema` |
| `reader_config` | Endpoint protocols only | Connection details (host/port/broker/endpoint) |
| `reader_name` | No | Label for a newly created reader (auto-named if blank) |
| `output` | No | `"influxdb"` (default) / `"live"` / `"influxdb,live"` |
| `tags_json` | No | InfluxDB/TimescaleDB tags |
| `table_name` | No | Defaults to `"Measurements"` |

**Response `201 Created`:**
```json
{
  "sensor_id": "s-ccc",
  "reader_id": "rd-111",
  "new_hash": "sha256:ghijkl...",
  "message": "Sensor created. Config will sync to Qube SQLite."
}
```

**SNMP example** (multi_target — no reader_config needed):
```json
{
  "name": "APC UPS DataCenter",
  "template_id": "<apc_ups_template_uuid>",
  "params": {
    "host": "10.0.1.50",
    "community": "public",
    "version": "2c"
  },
  "output": "influxdb",
  "tags_json": {"location": "DataCenter", "device_type": "ups"}
}
```

**MQTT example** (endpoint — broker connection in reader_config):
```json
{
  "name": "Shelly EM Power Monitor",
  "template_id": "<shelly_em_template_uuid>",
  "params": {
    "topic": "shellies/shellyem-ABC123/emeter/0/status"
  },
  "reader_config": {
    "broker_host": "192.168.1.100",
    "broker_port": 1883
  },
  "reader_name": "Local MQTT Broker",
  "output": "influxdb,live"
}
```

**The old two-step flow** (create reader first, then sensor) still works via
`POST /api/v1/qubes/{id}/readers` + `POST /api/v1/readers/{id}/sensors` — but the
smart endpoint is preferred for the onboarding wizard.

---

## 5. Device Catalog — Browsing & Filtering Templates

The "Device Catalog" is the list of all device templates available to the user.
This includes both global (superadmin-created) templates and the org's own custom templates.

### Fetch all templates (catalog home)

```http
GET /api/v1/device-templates
Authorization: Bearer <token>
```

Returns global + org templates, sorted: global first, then by protocol, then by name.

### Fetch by protocol (catalog filtered)

```http
GET /api/v1/device-templates?protocol=modbus_tcp
GET /api/v1/device-templates?protocol=snmp
GET /api/v1/device-templates?protocol=mqtt
```

### Get a single template (detail view)

```http
GET /api/v1/device-templates/def-456
Authorization: Bearer <token>
```

Returns the full template including `sensor_config` (registers/OIDs/nodes) and
`sensor_params_schema` (what user fills in).

### Catalog display logic

```js
async function loadCatalog(protocolFilter = null) {
    const url = protocolFilter
        ? `/api/v1/device-templates?protocol=${protocolFilter}`
        : '/api/v1/device-templates';
    const templates = await api.get(url);
    
    // Separate global from org-owned
    const global = templates.filter(t => t.is_global);
    const myOrg  = templates.filter(t => !t.is_global);
    
    // For each template, count the fields
    return templates.map(t => ({
        ...t,
        fieldCount: countSensorFields(t.sensor_config, t.protocol),
        isGlobal: t.is_global,
        displayName: t.manufacturer
            ? `${t.manufacturer} ${t.model}`.trim()
            : t.name,
    }));
}

function countSensorFields(sensorConfig, protocol) {
    const key = protocolArrayKey(protocol);
    const arr = sensorConfig?.[key];
    return Array.isArray(arr) ? arr.length : 0;
}

function protocolArrayKey(protocol) {
    const map = {
        modbus_tcp: 'registers',
        snmp:       'oids',
        mqtt:       'json_paths',
        opcua:      'nodes',
        http:       'json_paths',
        bacnet:     'objects',
        lorawan:    'readings',
        dnp3:       'points',
    };
    return map[protocol] || 'entries';
}
```

### Display sensor_config fields (what will be measured)

When the user views or selects a template, show all the fields from `sensor_config`:

```js
function renderSensorConfigFields(sensorConfig, protocol) {
    const key = protocolArrayKey(protocol);
    const fields = sensorConfig[key] || [];
    
    // Returns a table row per field
    return fields.map(f => {
        switch (protocol) {
            case 'modbus_tcp':
                return {
                    field: f.field_key,
                    detail: `${f.register_type} reg ${f.address} (${f.data_type} × ${f.scale})`,
                    unit: f.unit
                };
            case 'snmp':
                return { field: f.field_key, detail: f.oid, unit: f.unit };
            case 'opcua':
                return { field: f.field_key, detail: f.node_id, unit: f.unit };
            case 'mqtt':
            case 'http':
                return { field: f.field_key, detail: f.json_path, unit: f.unit };
            case 'bacnet':
                return { field: f.field_key, detail: `${f.object_type}[${f.object_instance}]`, unit: f.unit };
            case 'lorawan':
                return { field: f.field_key, detail: `payload field: ${f.field}`, unit: f.unit };
            case 'dnp3':
                return { field: f.field_key, detail: `Group ${f.group}, Var ${f.variation}, Idx ${f.index}`, unit: f.unit };
        }
    });
}
```

---

## 6. Superadmin Workflows

### 6.1 Adding a New Protocol

New protocols are added via the database, not the API. There is currently no REST endpoint
for creating protocols. The procedure is:

1. Write SQL (see `UI_DEVELOPMENT_PLAN.md` Section 1 for BACnet/LoRaWAN/DNP3 examples)
2. Execute via migration or admin SQL tool
3. The new protocol immediately appears in `GET /api/v1/protocols`

**The protocol row format:**
```sql
INSERT INTO protocols (id, label, description, reader_standard) VALUES
('lorawan', 'LoRaWAN', 'Long-range IoT via LoRaWAN network server', 'endpoint');
```

After this, the superadmin must also create a reader template (Step 6.2) and device
templates (Step 6.3) for the new protocol.

### 6.2 Adding a New Reader Template

Reader templates are created via the API (superadmin only):

```http
POST /api/v1/reader-templates
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "protocol": "lorawan",
  "name": "LoRaWAN NS Reader",
  "description": "Connects to Chirpstack or TTN network server",
  "image_suffix": "lorawan-reader",
  "connection_schema": {
    "type": "object",
    "properties": {
      "ns_host": {
        "type": "string",
        "title": "Network Server Host"
      },
      "ns_port": {
        "type": "integer",
        "title": "Port",
        "default": 1700
      },
      "app_id": {
        "type": "string",
        "title": "Application ID"
      },
      "api_key": {
        "type": "string",
        "title": "API Key",
        "format": "password"
      }
    },
    "required": ["ns_host", "app_id"]
  },
  "env_defaults": {
    "LOG_LEVEL": "info"
  }
}
```

**Response `201 Created`:**
```json
{
  "id": "rt-lorawan-001",
  "message": "reader template created"
}
```

**After creating a reader template:**
- It appears in `GET /api/v1/reader-templates?protocol=lorawan`
- The `connection_schema` auto-renders as a form when a user creates a LoRaWAN reader
- No other code changes needed in the UI

**Update a reader template:**
```http
PUT /api/v1/reader-templates/rt-lorawan-001
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "name": "LoRaWAN NS Reader v2",
  "description": "Updated description",
  "image_suffix": "lorawan-reader",
  "connection_schema": { ... },
  "env_defaults": { "LOG_LEVEL": "debug" }
}
```

**Delete a reader template:**
```http
DELETE /api/v1/reader-templates/rt-lorawan-001
Authorization: Bearer <superadmin_token>
```

### 6.3 Adding a New Global Device Template

Global device templates are visible to all organisations. Only superadmins can create them.

```http
POST /api/v1/device-templates
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "protocol": "lorawan",
  "name": "Dragino LHT65 Temp/Humidity",
  "manufacturer": "Dragino",
  "model": "LHT65",
  "description": "Indoor/outdoor temperature and humidity sensor with external probe",
  "sensor_config": {
    "readings": [
      {"field_key": "temperature_c",  "field": "TempC_SHT", "unit": "C"},
      {"field_key": "humidity_pct",   "field": "Hum_SHT",   "unit": "%"},
      {"field_key": "ext_temp_c",     "field": "TempC_DS",  "unit": "C"},
      {"field_key": "battery_v",      "field": "BatV",      "unit": "V"}
    ]
  },
  "sensor_params_schema": {
    "type": "object",
    "properties": {
      "dev_eui": {
        "type": "string",
        "title": "Device EUI (16 hex chars)",
        "description": "The unique device identifier from the device label"
      }
    },
    "required": ["dev_eui"]
  }
}
```

**Response `201 Created`:**
```json
{
  "id": "dt-lorawan-lht65",
  "is_global": true,
  "message": "device template created"
}
```

**Note:** The server automatically sets `is_global = true` when the request comes from a
superadmin token. No need to send `is_global` in the request body.

**Important code note:** After adding the template, any user (any org) can now see it via
`GET /api/v1/device-templates?protocol=lorawan` because the query includes
`WHERE (is_global=TRUE OR org_id=$1)`.

**Creating org-specific templates (Editor+ role):**
Same endpoint, same request body, but with an Editor/Admin token.
The server sets `is_global = false` and assigns the org's ID.
Only that org will see the template.

### 6.4 Editing a Global Template (Add/Remove Registers)

There are two ways to edit a device template's sensor_config:

#### Method A — Full replacement (PUT)

Replaces the entire template. Use when you want to change multiple fields at once.

```http
PUT /api/v1/device-templates/def-456
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "name": "Schneider PM5100",
  "manufacturer": "Schneider Electric",
  "model": "PM5100",
  "description": "Updated: 3-phase power meter",
  "sensor_config": {
    "registers": [
      {"field_key": "active_power_w",  "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
      {"field_key": "voltage_ll_v",    "register_type": "Holding", "address": 3020, "data_type": "float32", "scale": 1.0, "unit": "V"},
      {"field_key": "current_a",       "register_type": "Holding", "address": 3054, "data_type": "float32", "scale": 1.0, "unit": "A"},
      {"field_key": "energy_kwh",      "register_type": "Holding", "address": 3204, "data_type": "float32", "scale": 1.0, "unit": "kWh"},
      {"field_key": "power_factor",    "register_type": "Holding", "address": 3110, "data_type": "float32", "scale": 1.0, "unit": ""},
      {"field_key": "frequency_hz",    "register_type": "Holding", "address": 3060, "data_type": "float32", "scale": 1.0, "unit": "Hz"},
      {"field_key": "reactive_power_var", "register_type": "Holding", "address": 3096, "data_type": "float32", "scale": 1.0, "unit": "VAr"}
    ]
  },
  "sensor_params_schema": {
    "type": "object",
    "properties": {
      "unit_id": {"type": "integer", "title": "Modbus Unit ID", "default": 1, "minimum": 1, "maximum": 247},
      "register_offset": {"type": "integer", "title": "Register Offset", "default": 0}
    },
    "required": ["unit_id"]
  }
}
```

#### Method B — Fine-grained PATCH (recommended for field editors)

Use this when the user adds, edits, or deletes a single register/OID/node from a table UI.

**Add a new register:**
```http
PATCH /api/v1/device-templates/def-456/config
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "action": "add",
  "entry": {
    "field_key": "reactive_power_var",
    "register_type": "Holding",
    "address": 3096,
    "data_type": "float32",
    "scale": 1.0,
    "unit": "VAr"
  }
}
```

**Response:**
```json
{
  "updated": true,
  "total_entries": 7,
  "sensor_config": {
    "registers": [...all 7 registers...]
  }
}
```

**Update an existing register (by index):**
```http
PATCH /api/v1/device-templates/def-456/config
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "action": "update",
  "index": 2,
  "entry": {
    "field_key": "current_a",
    "register_type": "Holding",
    "address": 3054,
    "data_type": "float32",
    "scale": 0.001,
    "unit": "kA"
  }
}
```

**Delete a register (by index):**
```http
PATCH /api/v1/device-templates/def-456/config
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "action": "delete",
  "index": 5
}
```

**Full sensor_config replacement via PATCH:**
```http
PATCH /api/v1/device-templates/def-456/config
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "sensor_config": {
    "registers": [...complete replacement array...]
  }
}
```

### 6.5 Registry Configuration

The registry controls which Docker images are pulled when deploying reader containers.

**View current registry:**
```http
GET /api/v1/admin/registry
Authorization: Bearer <superadmin_token>
```

**Response:**
```json
{
  "mode": "gitlab",
  "github_base": "ghcr.io/sandun-s/qube-enterprise-home",
  "gitlab_base": "registry.gitlab.com/iot-team4/product",
  "images": {
    "img_conf_agent":    "registry.gitlab.com/.../enterprise-conf-agent:arm64.latest",
    "img_influx_sql":    "registry.gitlab.com/.../enterprise-influx-to-sql:arm64.latest",
    "img_modbus_reader": "registry.gitlab.com/.../modbus-reader:arm64.latest",
    "img_snmp_reader":   "registry.gitlab.com/.../snmp-reader:arm64.latest",
    "img_mqtt_reader":   "registry.gitlab.com/.../mqtt-reader:arm64.latest",
    "img_opcua_reader":  "registry.gitlab.com/.../opcua-reader:arm64.latest",
    "img_http_reader":   "registry.gitlab.com/.../http-reader:arm64.latest"
  }
}
```

**Update to GitHub mode:**
```http
PUT /api/v1/admin/registry
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "mode": "github",
  "github_base": "ghcr.io/your-org/qube-enterprise"
}
```

**Update individual image (e.g. add a new lorawan-reader image):**
```http
PUT /api/v1/admin/registry
Authorization: Bearer <superadmin_token>
Content-Type: application/json

{
  "mode": "gitlab",
  "gitlab_base": "registry.gitlab.com/iot-team4/product",
  "images": {
    "img_conf_agent":     "registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest",
    "img_modbus_reader":  "registry.gitlab.com/iot-team4/product/modbus-reader:arm64.latest",
    "img_snmp_reader":    "registry.gitlab.com/iot-team4/product/snmp-reader:arm64.latest",
    "img_mqtt_reader":    "registry.gitlab.com/iot-team4/product/mqtt-reader:arm64.latest",
    "img_opcua_reader":   "registry.gitlab.com/iot-team4/product/opcua-reader:arm64.latest",
    "img_http_reader":    "registry.gitlab.com/iot-team4/product/http-reader:arm64.latest",
    "img_lorawan_reader": "registry.gitlab.com/iot-team4/product/lorawan-reader:arm64.latest",
    "img_bacnet_reader":  "registry.gitlab.com/iot-team4/product/bacnet-reader:arm64.latest",
    "img_dnp3_reader":    "registry.gitlab.com/iot-team4/product/dnp3-reader:arm64.latest"
  }
}
```

**Image resolution logic** (from `readers.go:resolveReaderImage()`):
- Mode `github`: `<github_base>/<image_suffix>:arm64.latest`
- Mode `gitlab`: use `images["img_<image_suffix>"]` if set, else `<gitlab_base>/<image_suffix>:arm64.latest`
- The `image_suffix` comes from the reader template (e.g. `"modbus-reader"`)

### 6.6 Qube Management — List & Unclaim All Devices

As superadmin you can view every claimed Qube across **all organisations** and unclaim any of
them (returns the device to the unclaimed pool).

**List all claimed Qubes (cross-org):**
```http
GET /api/v1/admin/qubes
Authorization: Bearer <superadmin_token>
```

**Response:**
```json
[
  {
    "id": "Q-1001",
    "status": "online",
    "location_label": "Server Room A",
    "ws_connected": true,
    "claimed_at": "2026-04-07T09:00:00Z",
    "last_seen": "2026-04-09T08:31:00Z",
    "org_id": "uuid-of-org",
    "org_name": "Acme Corp"
  }
]
```

**Unclaim a Qube:**
```http
POST /api/v1/qubes/{id}/unclaim
Authorization: Bearer <superadmin_token>
```

**Response `200 OK`:**
```json
{
  "qube_id": "Q-1001",
  "message": "Device Q-1001 has been unclaimed and is available for re-claiming."
}
```

**What unclaim does:**
1. Deletes all sensors, readers, containers, and queued commands for the Qube
2. Resets `config_state` hash to empty
3. Clears `org_id`, `auth_token_hash`, `claimed_at`, `status` → `"unclaimed"`

**Error cases:**
- `404` — Qube ID does not exist
- `409` — Qube is not currently claimed
- `403` — Caller is not superadmin

**UI:** Available in the **Qube Management** page (sidebar, superadmin only). Each row shows
org name alongside the device and has an **Unclaim** button.

---

## 7. Fleet Management API Guide

### 7.1 List & Claim Qubes

**List all Qubes in your org:**
```http
GET /api/v1/qubes
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "Q-1001",
    "status": "online",
    "location_label": "Server Room A",
    "config_version": 12,
    "config_hash": "sha256:abc...",
    "ws_connected": true,
    "last_seen": "2026-04-07T08:32:15Z"
  },
  {
    "id": "Q-1002",
    "status": "offline",
    "location_label": "Warehouse B",
    "config_version": 4,
    "config_hash": "sha256:def...",
    "ws_connected": false,
    "last_seen": "2026-04-06T14:20:00Z"
  }
]
```

**UI display logic:**
```js
function getQubeStatus(qube) {
    if (!qube.last_seen) return 'unclaimed';
    const minutesSinceSeen = (Date.now() - new Date(qube.last_seen)) / 60000;
    if (qube.ws_connected) return 'online';
    if (minutesSinceSeen < 5) return 'online';    // recent heartbeat
    if (minutesSinceSeen < 30) return 'degraded';
    return 'offline';
}
```

**Claim a Qube (Admin+):**
```http
POST /api/v1/qubes/claim
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "register_key": "TEST-Q1001-REG"
}
```

**Response:**
```json
{
  "qube_id": "Q-1001",
  "auth_token": "hmac-auth-token-string"
}
```

The `auth_token` is the Qube's HMAC token for TP-API calls. The UI doesn't use it directly
(the Qube gets it during self-registration), but store it for reference.

### 7.2 View Qube Details

```http
GET /api/v1/qubes/Q-1001
Authorization: Bearer <token>
```

**Response:**
```json
{
  "id": "Q-1001",
  "status": "online",
  "location_label": "Server Room A",
  "poll_interval_sec": 30,
  "config_version": 12,
  "config_hash": "sha256:abc...",
  "ws_connected": true,
  "last_seen": "2026-04-07T08:32:15Z",
  "recent_commands": [
    {
      "id": "cmd-001",
      "command": "ping",
      "status": "acked",
      "sent_at": "2026-04-07T08:30:00Z",
      "acked_at": "2026-04-07T08:30:02Z"
    }
  ]
}
```

**List sensors on a Qube:**
```http
GET /api/v1/qubes/Q-1001/sensors
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "s-ccc",
    "name": "PM5100 Rack A Main Breaker",
    "reader_id": "rd-111",
    "reader_name": "Rack Panel A",
    "protocol": "modbus_tcp",
    "template_name": "Schneider PM5100",
    "output": "influxdb",
    "status": "active",
    "created_at": "2026-04-01T10:00:00Z"
  }
]
```

**Note:** This endpoint does NOT return `config_json`. To see the full sensor config,
use `GET /api/v1/readers/{reader_id}/sensors` which includes `config_json`.

**List containers on a Qube:**
```http
GET /api/v1/qubes/Q-1001/containers
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "ct-bbb",
    "name": "rack-panel-a",
    "image": "ghcr.io/.../modbus-reader:arm64.latest",
    "status": "running",
    "reader_id": "rd-111"
  },
  {
    "id": "ct-infra-001",
    "name": "core-switch",
    "image": "registry.../core-switch:arm64.latest",
    "status": "running",
    "reader_id": null
  }
]
```

### 7.3 Update Qube Settings

```http
PUT /api/v1/qubes/Q-1001
Authorization: Bearer <editor_token>
Content-Type: application/json

{
  "location_label": "Server Room A - Rack 3",
  "poll_interval_sec": 60
}
```

**Response:**
```json
{
  "updated": true
}
```

### 7.4 Unclaim a Qube (Superadmin)

See **Section 6.6** for full details. Short reference:

```http
POST /api/v1/qubes/{id}/unclaim
Authorization: Bearer <superadmin_token>
```

Cascades: deletes sensors → readers → containers → commands, resets config_state, returns
device to unclaimed pool.

---

## 8. Reader Management API Guide

### Get full reader details (including config)

```http
GET /api/v1/readers/rd-111
Authorization: Bearer <token>
```

**Response:**
```json
{
  "id": "rd-111",
  "name": "Rack Panel A",
  "protocol": "modbus_tcp",
  "config_json": {
    "host": "192.168.1.10",
    "port": 502,
    "poll_interval_sec": 20,
    "timeout_ms": 3000
  },
  "status": "active",
  "version": 3,
  "created_at": "2026-03-01T10:00:00Z",
  "updated_at": "2026-04-01T12:00:00Z"
}
```

### List all sensors on a reader (with full config_json)

```http
GET /api/v1/readers/rd-111/sensors
Authorization: Bearer <token>
```

**Response:**
```json
[
  {
    "id": "s-ccc",
    "name": "PM5100 Rack A Main Breaker",
    "template_id": "def-456",
    "template_name": "Schneider PM5100",
    "config_json": {
      "registers": [
        {"field_key": "active_power_w", "register_type": "Holding", "address": 3000, "data_type": "float32", "scale": 1.0, "unit": "W"},
        ...
      ],
      "unit_id": 3,
      "register_offset": 0
    },
    "tags_json": {"name": "PM5100_RackA", "location": "Server Room"},
    "output": "influxdb",
    "table_name": "Measurements",
    "status": "active",
    "version": 1,
    "created_at": "2026-04-01T10:00:00Z"
  }
]
```

**The `config_json` here is the MERGED result** (template registers + user params).
This is what the reader container on the Qube uses.

### Update a reader's connection config

```http
PUT /api/v1/readers/rd-111
Authorization: Bearer <editor_token>
Content-Type: application/json

{
  "name": "Rack Panel A (updated)",
  "config_json": {
    "host": "192.168.1.11",
    "port": 502,
    "poll_interval_sec": 15,
    "timeout_ms": 3000
  }
}
```

**Response:**
```json
{
  "message": "reader updated — conf-agent will sync",
  "new_hash": "sha256:newxyz..."
}
```

You can send just the fields you want to change. If you send `config_json`, it replaces the
entire connection config. If you only send `name`, only the name changes.

### Delete a reader (cascades to sensors and container)

```http
DELETE /api/v1/readers/rd-111
Authorization: Bearer <editor_token>
```

**Response:**
```json
{
  "deleted": true,
  "new_hash": "sha256:afterdelete...",
  "message": "Reader deleted. Container will be removed on next sync."
}
```

**Warning:** Deletes all sensors under this reader AND removes the container from Docker.
Always show a confirmation dialog with the sensor count.

---

## 9. Sensor Management API Guide

### Update a sensor

```http
PUT /api/v1/sensors/s-ccc
Authorization: Bearer <editor_token>
Content-Type: application/json

{
  "name": "PM5100 Rack A (Main Breaker)",
  "tags_json": {
    "name": "PM5100_RackA",
    "location": "Server Room",
    "circuit": "main_breaker",
    "panel": "MDB-01"
  },
  "output": "influxdb,live",
  "table_name": "EnergyMeasurements",
  "status": "active"
}
```

**Updatable fields:**
- `name` — display name
- `config_json` — full replacement of the merged config (advanced: use with care)
- `tags_json` — InfluxDB tags object or string
- `output` — `"influxdb"` | `"live"` | `"influxdb,live"`
- `table_name` — InfluxDB measurement name (defaults to `"Measurements"`)
- `status` — `"active"` | `"disabled"`

**Response:**
```json
{
  "message": "sensor updated",
  "new_hash": "sha256:updated..."
}
```

**Note on `status`:** The code accepts `"active"` and `"disabled"` only (from `sensors.go`
line 228: `if *req.Status == "active" || *req.Status == "disabled"`).
The plan mentioned `"inactive"` — use `"disabled"` instead.

### Deactivate a sensor (without deleting)

```http
PUT /api/v1/sensors/s-ccc
Authorization: Bearer <editor_token>
Content-Type: application/json

{
  "status": "disabled"
}
```

The reader will skip disabled sensors in SQLite.

### Delete a sensor

```http
DELETE /api/v1/sensors/s-ccc
Authorization: Bearer <editor_token>
```

**Response:**
```json
{
  "deleted": true,
  "new_hash": "sha256:afterdelete..."
}
```

---

## 10. Telemetry API Guide

### Get latest readings for a sensor

```http
GET /api/v1/data/sensors/s-ccc/latest
Authorization: Bearer <token>
```

**Response:**
```json
{
  "sensor_id": "s-ccc",
  "sensor_name": "PM5100 Rack A Main Breaker",
  "fields": [
    {"field_key": "active_power_w", "value": 1245.5, "unit": "W",   "time": "2026-04-07T08:32:10Z"},
    {"field_key": "voltage_ll_v",   "value": 398.2,  "unit": "V",   "time": "2026-04-07T08:32:10Z"},
    {"field_key": "current_a",      "value": 3.14,   "unit": "A",   "time": "2026-04-07T08:32:10Z"},
    {"field_key": "energy_kwh",     "value": 4521.0, "unit": "kWh", "time": "2026-04-07T08:32:10Z"}
  ]
}
```

Use this to populate a "latest readings" card for each sensor.

### Query historical readings

```http
GET /api/v1/data/readings?sensor_id=s-ccc&field=active_power_w&from=2026-04-07T00:00:00Z&to=2026-04-07T08:00:00Z
Authorization: Bearer <token>
```

**Parameters:**
- `sensor_id` — sensor UUID (required)
- `field` — specific field_key to query (optional, returns all fields if omitted)
- `from` — ISO8601 start time (optional)
- `to` — ISO8601 end time (optional)

**Response:**
```json
[
  {"time": "2026-04-07T00:00:00Z", "sensor_id": "s-ccc", "field_key": "active_power_w", "value": 980.0,  "unit": "W"},
  {"time": "2026-04-07T00:20:00Z", "sensor_id": "s-ccc", "field_key": "active_power_w", "value": 1050.0, "unit": "W"},
  {"time": "2026-04-07T00:40:00Z", "sensor_id": "s-ccc", "field_key": "active_power_w", "value": 1245.5, "unit": "W"}
]
```

### Build a time-series chart

```js
async function loadSensorHistory(sensorId, fieldKey, hoursBack = 24) {
    const to   = new Date().toISOString();
    const from = new Date(Date.now() - hoursBack * 3600000).toISOString();
    
    const url = `/api/v1/data/readings?sensor_id=${sensorId}&field=${fieldKey}&from=${from}&to=${to}`;
    const rows = await api.get(url);
    
    // Chart.js data format
    return {
        labels: rows.map(r => new Date(r.time).toLocaleTimeString()),
        datasets: [{
            label: `${fieldKey} (${rows[0]?.unit || ''})`,
            data: rows.map(r => r.value),
            borderColor: '#2196F3',
            fill: false,
        }]
    };
}
```

### WebSocket — Live sensor data

Connect to the dashboard WebSocket for real-time data:

```js
const ws = new WebSocket(`ws://localhost:8080/ws/dashboard`);

// After connection, authenticate
ws.onopen = () => {
    // Some implementations expect auth in query param:
    // ws://localhost:8080/ws/dashboard?token=<jwt>
    // Or as first message — check implementation
};

ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    
    // Types of messages you may receive:
    switch (msg.type) {
        case 'sensor_reading':
            // {type, qube_id, sensor_id, field_key, value, unit, time}
            updateLiveDisplay(msg);
            break;
        case 'config_update':
            // {type, qube_id, hash}
            showSyncBadge(msg.qube_id);
            break;
        case 'qube_connected':
            // {type, qube_id}
            updateQubeOnlineStatus(msg.qube_id, true);
            break;
        case 'qube_disconnected':
            // {type, qube_id}
            updateQubeOnlineStatus(msg.qube_id, false);
            break;
    }
};

// Auto-reconnect on disconnect
ws.onclose = () => setTimeout(() => connectDashboardWS(), 3000);
ws.onerror = (e) => console.error('WS error:', e);
```

---

## 11. Commands API Guide

Commands let you remotely control Qubes, their containers, network, filesystem, and device settings.
The enterprise conf-agent handles all command types — enterprise container commands and the full
legacy device management command set (network config, firewall, backup, maintenance mode, etc.).

### How it works

```
POST /api/v1/qubes/:id/commands
  → inserts into qube_commands table
  → if Qube WebSocket connected: pushed immediately via WS "command" message
  → if offline: stays in queue, conf-agent picks it up on next /v1/commands/poll cycle
  → conf-agent executes command (script or Go logic)
  → conf-agent POSTs result to /v1/commands/:id/ack
  → GET /api/v1/commands/:id shows final status + result
```

### Send a command

```http
POST /api/v1/qubes/Q-1001/commands
Authorization: Bearer <editor_token>
Content-Type: application/json

{
  "command": "ping",
  "payload": {"target": "8.8.8.8"}
}
```

**Response `202 Accepted`:**
```json
{
  "command_id": "cmd-xyz",
  "status": "pending",
  "delivery": "websocket",
  "poll_url": "/api/v1/commands/cmd-xyz"
}
```

`delivery` is either:
- `"websocket"` — command was pushed immediately to the Qube's live WS connection
- `"queued"` — Qube is offline; command queued for next HTTP poll cycle

### Check command status

```http
GET /api/v1/commands/cmd-xyz
Authorization: Bearer <token>
```

```json
{
  "id": "cmd-xyz",
  "qube_id": "Q-1001",
  "command": "ping",
  "status": "executed",
  "result": {"output": "PING 8.8.8.8...", "latency_ms": 12.3, "target": "8.8.8.8"},
  "created_at": "2026-04-07T08:30:00Z",
  "executed_at": "2026-04-07T08:30:02Z"
}
```

`status` values: `pending` → `sent` → `executed` | `failed` | `timeout`

---

### Command Reference

#### Enterprise — containers & config

| Command | Payload | Notes |
|---------|---------|-------|
| `ping` | `{"target":"8.8.8.8"}` | Returns latency_ms in result |
| `restart_qube` | `{}` | Reboots device OS. Also accepted as `reboot`. |
| `shutdown` | `{}` | Safely shuts down device |
| `restart_reader` | `{"reader_id":"<uuid>"}` or `{"service":"name"}` | Restarts one reader container |
| `stop_container` | `{"service":"qube_name"}` | Stops any Docker service |
| `reload_config` | `{}` | Clears local hash → forces resync on next cycle |
| `update_sqlite` | `{}` | Alias for reload_config |
| `get_logs` | `{"service":"name","lines":100}` | Omit service for all compose logs |
| `list_containers` | `{}` | Returns running container names + status |

#### Device management — network

| Command | Payload | Notes |
|---------|---------|-------|
| `reset_ips` | `{}` | Resets all interfaces to DHCP, reconnects to qube-net WiFi |
| `set_eth` DHCP | `{"interface":"eth0","mode":"auto"}` | |
| `set_eth` static | `{"interface":"eth0","mode":"static","address":"192.168.1.10/24","gateway":"192.168.1.1","dns":"8.8.8.8"}` | |
| `set_wifi` DHCP | `{"interface":"wlan0","mode":"auto","ssid":"MyWifi","password":"secret","key_mgmt":"psk"}` | key_mgmt: psk, sae, eap, none |
| `set_wifi` static | `{"interface":"wlan0","mode":"static","address":"192.168.1.20/24","gateway":"192.168.1.1","dns":"8.8.8.8","ssid":"MyWifi","password":"secret","key_mgmt":"psk"}` | |
| `set_firewall` | `{"rules":"tcp:10.0.0.0/8:1883,tcp:0:8080"}` | Format: `proto:net-or-0:port-or-0`, comma-separated |

#### Device management — identity & info

| Command | Payload | Notes |
|---------|---------|-------|
| `get_info` | `{}` | Returns eth_mac, eth_ipv4, wlan_mac, wlan_ipv4, wlan_ssid, open_ports |
| `set_name` | `{"name":"qube-factory-a"}` | Updates avahi hostname + /boot/mit.txt devicename |
| `set_timezone` | `{"timezone":"Asia/Colombo"}` | Use IANA tz names; reboot recommended after |

#### Data backup / restore

| Command | Payload | Notes |
|---------|---------|-------|
| `backup_data` | `{"type":"cifs","path":"\\\\192.168.1.1\\share","user":"u","pass":"p"}` | rsync /data to CIFS share |
| `backup_data` | `{"type":"nfs","path":"192.168.1.1:/nfs-share"}` | rsync /data to NFS share |
| `restore_data` | same as backup_data | rsync from share to /data |

#### Maintenance mode operations

These cause the device to reboot into maintenance mode, perform the operation,
then reboot back. The device will be **offline for several minutes**.

| Command | Payload | Notes |
|---------|---------|-------|
| `repair_fs` | `{}` | e2fsck on /data and firmware partitions |
| `backup_image` | `{}` | dd block-level image → backup partition |
| `restore_image` | `{}` | dd block-level restore from backup partition |

#### Service management

| Command | Payload | Notes |
|---------|---------|-------|
| `service_add` | `{"name":"svc","type":"modbus","version":"1.2.3","ports":"8090"}` | Installs from pre-staged /mit/qube/install/ package |
| `service_rm` | `{"name":"svc"}` | Stops and removes Docker service + config |
| `service_edit` | `{"name":"svc","ports":"8090,8091"}` | Updates config files and/or ports |

#### File transfer

| Command | Payload | Notes |
|---------|---------|-------|
| `put_file` | `{"path":"/config/myfile.cfg","data":"<base64>"}` | Writes to /mit + path on device |
| `get_file` | `{"path":"/config/myfile.cfg"}` | Returns `{"data":"<base64>","size":N}` |

Path must not contain `..`, `$`, `\`, or `;`.

#### Discovery commands

| Command | Payload | Notes |
|---------|---------|-------|
| `mqtt_discover` | `{"broker_host":"192.168.1.100","broker_port":1883,"topic":"#","duration_sec":30}` | Subscribes to wildcard topic for N seconds. Result contains discovered topics + parsed JSON fields. Use to build MQTT device templates. |
| `snmp_walk` | `{"host":"10.0.1.50","community":"public","version":"2c","oid":".1.3.6.1"}` | Walks OID subtree. Result contains OID→value map. Use to find OIDs for SNMP device templates. |

**MQTT discovery result shape:**
```json
{
  "topics_found": 12,
  "entries": [
    {"topic": "shellies/em-ABC/emeter/0/status", "fields": ["power", "voltage", "current", "pf"]},
    {"topic": "sensors/room1/temp", "fields": ["value", "unit"]}
  ]
}
```

**SNMP walk result shape:**
```json
{
  "oids_found": 48,
  "entries": [
    {"oid": ".1.3.6.1.2.1.1.1.0", "type": "OctetString", "value": "APC Smart-UPS 1500"},
    {"oid": ".1.3.6.1.4.1.318.1.1.1.2.2.1.0", "type": "Integer", "value": "100"}
  ]
}
```

> **Note:** conf-agent must implement `mqtt_discover` and `snmp_walk` handlers in `agent/agent.go`
> for these commands to produce results. Until then, the command is sent and queued, but the
> result will be empty or contain a "not implemented" message.

---

### UI implementation — send command

```js
async function sendCommand(qubeId, command, payload = {}) {
  const res = await api.post(`/api/v1/qubes/${qubeId}/commands`, { command, payload });
  // res.command_id, res.delivery, res.status
  return res;
}

// Poll for result
async function waitForResult(commandId, timeoutMs = 60000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const cmd = await api.get(`/api/v1/commands/${commandId}`);
    if (cmd.status === 'executed' || cmd.status === 'failed') return cmd;
    await sleep(2000);
  }
  throw new Error('Command timed out');
}
```

### Dynamic payload forms by command type

```js
const commandPayloadFields = {
  ping:            [{ key: 'target', type: 'text', default: '8.8.8.8', label: 'Target Host' }],
  restart_reader:  [{ key: 'reader_id', type: 'text', label: 'Reader ID' }],
  stop_container:  [{ key: 'service', type: 'text', label: 'Service Name' }],
  get_logs:        [{ key: 'service', type: 'text', label: 'Service (blank=all)' },
                   { key: 'lines', type: 'number', default: 100, label: 'Lines' }],
  set_eth:         [{ key: 'interface', type: 'text', default: 'eth0' },
                   { key: 'mode', type: 'select', options: ['auto','static'] },
                   { key: 'address', type: 'text', label: 'IP/Prefix (static only)', conditional: 'mode=static' },
                   { key: 'gateway', type: 'text', conditional: 'mode=static' },
                   { key: 'dns', type: 'text', conditional: 'mode=static' }],
  set_wifi:        [{ key: 'interface', type: 'text', default: 'wlan0' },
                   { key: 'mode', type: 'select', options: ['auto','static'] },
                   { key: 'ssid', type: 'text' }, { key: 'password', type: 'password' },
                   { key: 'key_mgmt', type: 'select', options: ['psk','sae','eap','none'], default: 'psk' },
                   { key: 'address', type: 'text', conditional: 'mode=static' },
                   { key: 'gateway', type: 'text', conditional: 'mode=static' },
                   { key: 'dns', type: 'text', conditional: 'mode=static' }],
  set_firewall:    [{ key: 'rules', type: 'text', label: 'Rules (proto:net:port, comma-sep)', placeholder: 'tcp:10.0.0.0/8:1883,tcp:0:8080' }],
  set_name:        [{ key: 'name', type: 'text', label: 'New Hostname' }],
  set_timezone:    [{ key: 'timezone', type: 'text', label: 'Timezone', placeholder: 'Asia/Colombo' }],
  backup_data:     [{ key: 'type', type: 'select', options: ['cifs','nfs'] },
                   { key: 'path', type: 'text' }, { key: 'user', type: 'text', conditional: 'type=cifs' },
                   { key: 'pass', type: 'password', conditional: 'type=cifs' }],
  restore_data:    'same as backup_data',
  service_add:     [{ key: 'name', type: 'text' }, { key: 'type', type: 'text' },
                   { key: 'version', type: 'text' }, { key: 'ports', type: 'text', label: 'Ports (comma-sep)' }],
  service_rm:      [{ key: 'name', type: 'text' }],
  service_edit:    [{ key: 'name', type: 'text' }, { key: 'ports', type: 'text' }],
  put_file:        [{ key: 'path', type: 'text', label: 'Device path (e.g. /config/file.cfg)' },
                   { key: 'data', type: 'file', label: 'File (base64 encoded on upload)' }],
  get_file:        [{ key: 'path', type: 'text', label: 'Device path' }],
  mqtt_discover:   [{ key: 'broker_host', type: 'text', label: 'Broker Host' },
                   { key: 'broker_port', type: 'number', default: 1883, label: 'Broker Port' },
                   { key: 'topic', type: 'text', default: '#', label: 'Wildcard Topic' },
                   { key: 'duration_sec', type: 'number', default: 30, label: 'Duration (s)' }],
  snmp_walk:       [{ key: 'host', type: 'text', label: 'Device IP' },
                   { key: 'community', type: 'text', default: 'public', label: 'Community' },
                   { key: 'version', type: 'select', options: ['1','2c','3'], default: '2c', label: 'SNMP Version' },
                   { key: 'oid', type: 'text', default: '.1.3.6.1', label: 'Root OID' }],
  // no payload needed:
  restart_qube: [], reboot: [], shutdown: [], reload_config: [], update_sqlite: [],
  list_containers: [], reset_ips: [], get_info: [], repair_fs: [], backup_image: [], restore_image: [],
};
```

---

## 12. User Management API Guide

### List users in org

```http
GET /api/v1/users
Authorization: Bearer <admin_token>
```

**Response:**
```json
[
  {
    "id": "usr-001",
    "email": "alice@acme.com",
    "role": "editor",
    "created_at": "2026-01-15T09:00:00Z"
  },
  {
    "id": "usr-002",
    "email": "bob@acme.com",
    "role": "viewer",
    "created_at": "2026-02-20T14:00:00Z"
  }
]
```

### Invite a new user

```http
POST /api/v1/users
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "email": "carol@acme.com",
  "role": "editor"
}
```

**Response:**
```json
{
  "id": "usr-003",
  "message": "User created"
}
```

**Note:** There is no email invite system. The password must be shared out-of-band.
The user logs in with the email and an initial password set by the admin.

**Valid roles:** `viewer`, `editor`, `admin`
(Superadmin role cannot be assigned via this endpoint)

### Update a user's role

```http
PATCH /api/v1/users/usr-002
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "role": "editor"
}
```

### Remove a user

```http
DELETE /api/v1/users/usr-002
Authorization: Bearer <admin_token>
```

---

## 13. Protocol-Specific Form Reference

This section documents the exact `connection_schema` and `sensor_params_schema` for each
protocol, so the UI knows what to render.

---

### Modbus TCP (`modbus_tcp`) — `endpoint`

**Connection form** (rendered from `connection_schema`, one per reader):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| host | string (ipv4) | — | Required. Device IP address |
| port | integer | 502 | Required. 1–65535 |
| poll_interval_sec | integer | 20 | 1–3600 |
| timeout_ms | integer | 3000 | Modbus request timeout (ms) |
| slave_id | integer | 1 | Default Modbus slave ID (can be overridden per sensor via unit_id) |
| single_read_count | integer | 100 | Max registers per single read request |

**Sensor params form** (rendered from `sensor_params_schema`, one per sensor):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| unit_id | integer | 1 | Required. Modbus slave ID, 1–247 |
| register_offset | integer | 0 | Optional. Added to all register addresses |

**sensor_config fields** (stored in template, shown read-only to user):

| Column | Values |
|--------|--------|
| field_key | Measurement name (e.g. active_power_w) |
| register_type | Holding / Input / Coil / Discrete |
| address | Register address (0-based) |
| data_type | uint16 / int16 / uint32 / int32 / float32 |
| scale | Multiply raw value by this |
| unit | Physical unit (W, V, A, kWh, Hz, etc.) |

---

### SNMP (`snmp`) — `multi_target`

**Connection form** (one per Qube, all SNMP sensors share it):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| poll_interval_sec | integer | 15 | 5–3600 |
| timeout_ms | integer | 5000 | Per-device SNMP timeout (ms) |
| retries | integer | 2 | Retry count per device, 1–10 |

> **Note:** For multi_target protocols the connection form is optional — if you use
> `POST /api/v1/qubes/:id/sensors` without a `reader_config`, the shared SNMP reader
> is auto-created with these defaults. No manual reader creation needed.

**Sensor params form** (different target per sensor):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| host | string (ipv4) | — | Required. Device IP address |
| port | integer | 161 | SNMP port (161 standard) |
| community | string | "public" | SNMP community string |
| version | string enum | "2c" | Options: "1", "2c", "3" |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| oid | Full OID string (e.g. .1.3.6.1.4.1.318...) |
| unit | Physical unit |

---

### MQTT (`mqtt`) — `endpoint`

**Connection form** (one per broker — stored in reader `config_json`):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| broker_host | string | — | Required. Hostname or IP (e.g. 192.168.1.100) |
| broker_port | integer | 1883 | Required |
| username | string | — | Optional |
| password | string (password) | — | Optional |
| client_id | string | — | Optional. Auto-generated if blank |
| qos | integer | 1 | QoS level: 0, 1, or 2 |

**Sensor params form** (one per physical device/topic):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| topic | string | — | Required. MQTT topic to subscribe (supports wildcards: `shellies/+/emeter/0/status`) |

> **Important:** `topic` is a per-sensor param, NOT a connection-level field. Each sensor
> subscribes to its own topic. The `json_paths` entries in `sensor_config` define which JSON
> fields to extract. The reader uses a top-level `topic` fallback from the sensor params if
> individual json_paths entries don't specify their own topic.

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| json_path | JSONPath expression (e.g. $.power) |
| topic | (optional) overrides the sensor-level topic for this specific field |
| unit | Physical unit |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| json_path | JSONPath expression (e.g. $.temperature) |
| unit | Physical unit |

---

### OPC-UA (`opcua`) — `endpoint`

**Connection form** (one per OPC-UA server):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| endpoint | string | — | Required. e.g. opc.tcp://192.168.1.18:4840 |
| security_mode | string enum | "None" | Options: None, Sign, SignAndEncrypt |
| security_policy | string | "None" | Security policy URI (e.g. Basic256Sha256) |
| poll_interval_sec | integer | 10 | 1–3600 |

**Sensor params form:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| namespace_index | integer | 2 | OPC-UA namespace index |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| node_id | OPC-UA NodeId (e.g. ns=2;i=1001) |
| type | float / int / bool / string |
| unit | Physical unit |

---

### HTTP/REST (`http`) — `multi_target`

**Connection form** (one per Qube, all HTTP sensors share it):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| poll_interval_sec | integer | 30 | 5–3600 |
| timeout_ms | integer | 10000 | Per-request timeout (ms) |

> **Note:** Like SNMP, the HTTP shared reader is auto-created when you use
> `POST /api/v1/qubes/:id/sensors`. No manual reader creation needed.

**Sensor params form** (per endpoint — each sensor polls its own URL):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| url | string (uri) | — | Required. Full URL to poll |
| method | string enum | "GET" | Options: GET, POST |
| auth_type | string enum | "none" | none / basic / bearer / api_key |
| username | string | — | For basic auth |
| password | string (password) | — | For basic auth |
| bearer_token | string (password) | — | For bearer auth |
| headers_json | string | — | JSON object of custom headers |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| json_path | JSONPath expression (e.g. $.readings.value) |
| unit | Physical unit |

---

### BACnet/IP (`bacnet`) — `multi_target` *(new)*

**Connection form** (one per Qube):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| local_port | integer | 47808 | UDP port for BACnet/IP |
| poll_interval_sec | integer | 30 | 5–3600 |
| timeout_ms | integer | 3000 | Request timeout |
| broadcast_addr | string (ipv4) | — | Subnet broadcast for discovery |

**Sensor params form:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| ip_address | string (ipv4) | — | Required. Device IP |
| device_instance | integer | — | Required. BACnet device instance |
| property_id | string | "presentValue" | BACnet property to read |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| object_type | analogInput / analogOutput / analogValue / binaryInput / binaryOutput / multiStateInput |
| object_instance | BACnet object instance number |
| unit | Physical unit |

---

### LoRaWAN (`lorawan`) — `endpoint` *(new)*

**Connection form** (one per NS application):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| ns_host | string | — | Required. Network Server hostname |
| ns_port | integer | 1700 | 1–65535 |
| app_id | string | — | Required. Application ID |
| api_key | string (password) | — | NS API key |
| mqtt_broker | string | — | Optional. Chirpstack MQTT broker |

**Sensor params form:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| dev_eui | string | — | Required. 16-char hex Device EUI |
| app_eui | string | — | Optional. Application EUI (TTN) |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| field | Raw payload field name from NS uplink |
| unit | Physical unit |

---

### DNP3 (`dnp3`) — `endpoint` *(new)*

**Connection form** (one per outstation):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| host | string (ipv4) | — | Required. Outstation IP |
| port | integer | 20000 | 1–65535 |
| master_address | integer | 1 | DNP3 master address, 0–65519 |
| outstation_address | integer | 10 | Required. DNP3 outstation address |
| poll_interval_sec | integer | 10 | 1–3600 |

**Sensor params form:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| outstation_address | integer | 10 | DNP3 outstation address |

**sensor_config fields:**

| Column | Values |
|--------|--------|
| field_key | Measurement name |
| group | DNP3 object group (30=Analog In, 1=Binary In, 20=Counter) |
| variation | DNP3 variation (1, 2, 5, etc.) |
| index | Point index within the group |
| unit | Physical unit |

---

## 14. Config Sync — How Changes Flow to the Qube

Understanding this prevents confusion about why changes don't appear immediately.

### The sync chain

```
Cloud API mutation (create/update/delete reader or sensor)
    ↓ (automatic, same request)
recomputeConfigHash() called
    ↓
config_state.hash updated in PostgreSQL (qubedb)
    ↓
If Qube has WebSocket connected (ws_connected=true):
    WebSocket hub sends: {"type":"config_update","qube_id":"Q-1001","hash":"new-hash"}
    ↓ (within milliseconds)
    conf-agent receives push
    conf-agent: GET /v1/sync/state → compares hash
    conf-agent: GET /v1/sync/config → downloads full config
    conf-agent: writes SQLite at /opt/qube/data/qube.db
    conf-agent: Docker API: stops affected reader containers
    Docker Swarm: recreates containers
    Reader container: reads new SQLite on startup
    
If Qube is offline (ws_connected=false):
    Change sits in PostgreSQL
    When Qube comes back online:
        conf-agent polls GET /v1/sync/state every POLL_INTERVAL seconds
        Detects hash mismatch → fetches config → applies
```

### What the API response tells you

Every mutation (create/update/delete reader or sensor) returns `new_hash`:
```json
{
  "message": "Sensor created. Config will sync to Qube SQLite.",
  "sensor_id": "s-ccc",
  "new_hash": "sha256:ghijkl..."
}
```

The UI should:
1. Show "Config update in progress..." immediately after mutation
2. Poll `GET /api/v1/qubes/Q-1001` every 5 seconds
3. When `qube.config_hash === new_hash` from the mutation response → show "Synced ✓"
4. If 60 seconds pass with no match → show "Sync pending (Qube may be offline)"

```js
async function waitForSync(qubeId, expectedHash, timeoutMs = 60000) {
    const start = Date.now();
    while (Date.now() - start < timeoutMs) {
        await sleep(5000);
        const qube = await api.get(`/api/v1/qubes/${qubeId}`);
        if (qube.config_hash === expectedHash) return 'synced';
    }
    return 'timeout';
}
```

---

## 15. Status Monitoring

### Sensor data staleness

There is no "sensor status" API that tells you if a sensor is actively sending data.
Derive it from the last telemetry timestamp:

```js
async function getSensorDataFreshness(sensorId) {
    try {
        const latest = await api.get(`/api/v1/data/sensors/${sensorId}/latest`);
        if (!latest.fields?.length) return 'no_data';
        
        const lastTime = Math.max(...latest.fields.map(f => new Date(f.time)));
        const minutesAgo = (Date.now() - lastTime) / 60000;
        
        if (minutesAgo < 2)  return 'fresh';     // green
        if (minutesAgo < 10) return 'recent';    // yellow
        if (minutesAgo < 60) return 'stale';     // orange
        return 'no_recent_data';                  // red
    } catch {
        return 'error';
    }
}
```

### Qube connection status

From `GET /api/v1/qubes`:
- `ws_connected: true` → Online (WebSocket active)
- `ws_connected: false` + `last_seen < 5 min` → Polling mode (Qube alive, WS not connected)
- `ws_connected: false` + `last_seen > 30 min` → Offline
- No `last_seen` → Unclaimed / never registered

### Config sync status

```js
function getSyncStatus(qube) {
    // No way to know the "desired hash" without tracking it from the last mutation
    // Use version number as a proxy:
    return {
        version: qube.config_version,
        hash: qube.config_hash,
        wsConnected: qube.ws_connected,
        lastSeen: qube.last_seen,
    };
}
```

---

## 16. Complete API Quick Reference

All endpoints — method, path, minimum required role, brief description.

### Health
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | None | Service health check |

### Auth
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/auth/register` | None | Create org + admin user |
| POST | `/api/v1/auth/login` | None | Login, get JWT |

### Users
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/users/me` | Any | Current user profile |
| GET | `/api/v1/users` | Admin+ | List org users |
| POST | `/api/v1/users` | Admin+ | Create/invite user |
| PATCH | `/api/v1/users/{id}` | Admin+ | Update user role |
| DELETE | `/api/v1/users/{id}` | Admin+ | Remove user |

### Qubes
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/qubes` | Any | List all Qubes in org |
| POST | `/api/v1/qubes/claim` | Admin+ | Claim Qube by register_key |
| GET | `/api/v1/qubes/{id}` | Any | Qube detail + recent commands |
| PUT | `/api/v1/qubes/{id}` | Editor+ | Update location/poll_interval |
| GET | `/api/v1/qubes/{id}/readers` | Any | List readers on Qube |
| POST | `/api/v1/qubes/{id}/readers` | Editor+ | Create reader + container |
| GET | `/api/v1/qubes/{id}/sensors` | Any | All sensors on Qube (summary) |
| GET | `/api/v1/qubes/{id}/containers` | Any | Docker containers on Qube |
| POST | `/api/v1/qubes/{id}/commands` | Editor+ | Send remote command |

### Readers
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/readers/{id}` | Any | Reader detail + config |
| PUT | `/api/v1/readers/{id}` | Editor+ | Update name/config/status |
| DELETE | `/api/v1/readers/{id}` | Editor+ | Delete reader + sensors + container |
| GET | `/api/v1/readers/{id}/sensors` | Any | Sensors with full config_json |
| POST | `/api/v1/readers/{id}/sensors` | Editor+ | Create sensor (template merge) |

### Sensors
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| PUT | `/api/v1/sensors/{id}` | Editor+ | Update tags/output/status/name |
| DELETE | `/api/v1/sensors/{id}` | Editor+ | Delete sensor |

### Protocols
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/protocols` | Any | List all active protocols |

### Reader Templates
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/reader-templates` | Any | List (optional ?protocol= filter) |
| GET | `/api/v1/reader-templates/{id}` | Any | Single reader template |
| POST | `/api/v1/reader-templates` | Superadmin | Create reader template |
| PUT | `/api/v1/reader-templates/{id}` | Superadmin | Update reader template |
| DELETE | `/api/v1/reader-templates/{id}` | Superadmin | Delete reader template |

### Device Templates
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/device-templates` | Any | List all (global + org), ?protocol= filter |
| GET | `/api/v1/device-templates/{id}` | Any | Single device template |
| POST | `/api/v1/device-templates` | Editor+ | Create (global if superadmin) |
| PUT | `/api/v1/device-templates/{id}` | Editor+ | Full template update |
| PATCH | `/api/v1/device-templates/{id}/config` | Editor+ | Add/update/delete single field |
| DELETE | `/api/v1/device-templates/{id}` | Editor+ | Delete template (SA for global) |

### Telemetry
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/data/sensors/{id}/latest` | Any | Latest readings for sensor |
| GET | `/api/v1/data/readings` | Any | Historical readings with filters |

### Admin (Superadmin only)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/admin/qubes` | Superadmin | List all claimed Qubes across all orgs |
| POST | `/api/v1/qubes/{id}/unclaim` | Superadmin | Unclaim a Qube (clears org, readers, sensors, containers) |
| GET | `/api/v1/admin/registry` | Superadmin | View registry settings |
| PUT | `/api/v1/admin/registry` | Superadmin | Update registry settings |

### WebSocket
| Endpoint | Auth | Description |
|----------|------|-------------|
| `ws://.../ws` | JWT query param | Qube config sync (conf-agent only) |
| `ws://.../ws/dashboard` | JWT Bearer | Live sensor data + qube events |

---

*End of Developer Guide — Qube Enterprise v2*
*See also: `UI_DEVELOPMENT_PLAN.md` for the complete UI build plan*
*Generated: 2026-04-07*
