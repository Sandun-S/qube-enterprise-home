# Qube Enterprise — Testing Scenarios

Full test coverage for every API endpoint.
Run scenarios in order — later scenarios depend on IDs from earlier ones.

```bash
# Set base URL once
BASE="http://localhost:8080"
TPBASE="http://localhost:8081"
```

---

## 0. Health checks

```bash
# Cloud API health
curl -s $BASE/health | jq .
# Expected: {"status":"ok","service":"cloud-api"}

# TP-API health
curl -s $TPBASE/health | jq .
# Expected: {"status":"ok","service":"tp-api"}
```

---

## 1. Authentication

### 1.1 Register new organisation
```bash
curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Acme Corp","email":"admin@acme.com","password":"secret123"}' | jq .
# Expected: token, org_id, user_id, role:"admin"

TOKEN=$(curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Test Org","email":"test@test.com","password":"pass1234"}' | jq -r .token)
echo "TOKEN=$TOKEN"
```

### 1.2 Register — duplicate email
```bash
curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Another","email":"test@test.com","password":"pass1234"}' | jq .
# Expected: 409 {"error":"email already registered"}
```

### 1.3 Register — missing fields
```bash
curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"No Email"}' | jq .
# Expected: 400 {"error":"org_name, email and password required"}
```

### 1.4 Login — valid
```bash
TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"pass1234"}' | jq -r .token)
echo "TOKEN=$TOKEN"
# Expected: token, org_id, role
```

### 1.5 Login — wrong password
```bash
curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"wrongpass"}' | jq .
# Expected: 401 {"error":"invalid credentials"}
```

### 1.6 Login — superadmin (IoT team)
```bash
SA_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' | jq -r .token)
echo "SA_TOKEN=$SA_TOKEN"
# Expected: role:"superadmin"
```

### 1.7 Protected endpoint — no token
```bash
curl -s $BASE/api/v1/qubes | jq .
# Expected: 401 {"error":"missing or invalid Authorization header"}
```

### 1.8 Protected endpoint — expired/invalid token
```bash
curl -s -H "Authorization: Bearer invalidtoken123" \
  $BASE/api/v1/qubes | jq .
# Expected: 401 {"error":"invalid or expired token"}
```

---

## 1b. User Management

> **Key design points:**
> - `POST /api/v1/auth/register` always creates an **admin** user and a new org. Use that to onboard a new customer.
> - To add teammates to an existing org, use `POST /api/v1/users` — **password is optional**, org is **auto-assigned from the calling admin's JWT** (you cannot add users to a different org by mistake).
> - If no password is given, a default temp password is used (`Qube@2024` or whatever `DEFAULT_USER_PASSWORD` env var is set to on the server). The response includes `is_temp_password: true` and `temp_password` so the admin can share it.
> - If a password IS given, it is used directly — `is_temp_password: false` and the password is not echoed back.
> - All users in an org automatically see all Qubes claimed by that org — no per-Qube user setup needed.

---

### Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/users/me` | Any JWT | Get your own profile (role, org, email) |
| `GET` | `/api/v1/users` | Admin+ | List all users in your org |
| `POST` | `/api/v1/users` | Admin+ | Invite a new user to your org |
| `PATCH` | `/api/v1/users/:id` | Admin+ | Change a user's role |
| `DELETE` | `/api/v1/users/:id` | Admin+ | Remove a user from your org |

---

### 1b.1 Get my own profile
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/users/me | jq .
# Expected:
# {
#   "user_id": "uuid",
#   "org_id":  "uuid",
#   "email":   "admin@acme.com",
#   "role":    "admin",
#   "org_name": "Acme Corp"
# }
# Use this to verify what permissions your current token has
```

### 1b.2 List all users in the org (admin only)
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/users | jq .
# Expected: array of {id, email, role, created_at}
# Only shows users in YOUR org — completely isolated from other orgs
```

### 1b.3 Invite a viewer — no password (temp password generated)
```bash
R=$(curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"viewer@acme.com","role":"viewer"}')
echo $R | jq .
# Expected:
# {
#   "user_id":          "uuid",
#   "email":            "viewer@acme.com",
#   "role":             "viewer",
#   "org_id":           "uuid",       ← auto-assigned from admin's JWT
#   "is_temp_password": true,         ← tells frontend to prompt password change
#   "temp_password":    "Qube@2024"   ← admin shares this with the new user
# }

VIEWER_ID=$(echo $R | jq -r .user_id)
VIEWER_TEMP_PASS=$(echo $R | jq -r .temp_password)

# Viewer logs in with the temp password
VIEWER_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"viewer@acme.com\",\"password\":\"$VIEWER_TEMP_PASS\"}" | jq -r .token)
```

### 1b.4 Invite an editor — with explicit password
```bash
R=$(curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"engineer@acme.com","role":"editor","password":"MySecurePass123"}')
echo $R | jq .
# Expected:
# {
#   "user_id":          "uuid",
#   "email":            "engineer@acme.com",
#   "role":             "editor",
#   "org_id":           "uuid",
#   "is_temp_password": false    ← password was provided, not temp
#   // temp_password NOT in response — caller already knows the password
# }

EDITOR_ID=$(echo $R | jq -r .user_id)

# Login as editor
EDITOR_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"engineer@acme.com","password":"MySecurePass123"}' | jq -r .token)
```

### 1b.5 Invite a second admin to the org
```bash
curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"manager@acme.com","role":"admin","password":"ManagerPass123"}' | jq .
# Expected: 201 — second admin can also claim qubes and manage users
# Note: only superadmin can create superadmin accounts
```

### 1b.6 Viewer cannot claim a qube (forbidden)
```bash
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1003-REG"}' | jq .
# Expected: 403 {"error":"forbidden"} — claim requires admin role
```

### 1b.7 Viewer cannot add a gateway (forbidden)
```bash
curl -s -X POST "$BASE/api/v1/qubes/Q-1001/gateways" \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","protocol":"modbus_tcp","host":"192.168.1.1","port":502}' | jq .
# Expected: 403 — editor or admin role required
```

### 1b.8 Viewer can read data (allowed)
```bash
# Viewer CAN see qubes, sensors, and telemetry data
curl -s -H "Authorization: Bearer $VIEWER_TOKEN" \
  $BASE/api/v1/qubes | jq .
# Expected: 200 — same qubes the admin sees (same org)

curl -s -H "Authorization: Bearer $VIEWER_TOKEN" \
  "$BASE/api/v1/data/sensors/$SENSOR_MODBUS_ID/latest" | jq .
# Expected: 200 — can read sensor readings
```

### 1b.9 Editor can add gateways and sensors (allowed)
```bash
# Editor CAN create gateways
curl -s -X POST "$BASE/api/v1/qubes/Q-1001/gateways" \
  -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Panel_C","protocol":"modbus_tcp","host":"192.168.1.102","port":502}' | jq .
# Expected: 201

# Editor CANNOT claim qubes
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1004-REG"}' | jq .
# Expected: 403
```

### 1b.10 Promote viewer to editor
```bash
curl -s -X PATCH "$BASE/api/v1/users/$VIEWER_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"editor"}' | jq .
# Expected: 200 {"user_id":"...","role":"editor"}
```

### 1b.11 Cannot change own role
```bash
MY_ID=$(curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/users/me | jq -r .user_id)
curl -s -X PATCH "$BASE/api/v1/users/$MY_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"viewer"}' | jq .
# Expected: 400 {"error":"cannot change your own role"}
```

### 1b.12 Invalid role value
```bash
curl -s -X PATCH "$BASE/api/v1/users/$VIEWER_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"superuser"}' | jq .
# Expected: 400 {"error":"role must be viewer, editor, or admin"}
```

### 1b.13 Duplicate email invite
```bash
curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"viewer@acme.com","role":"viewer"}' | jq .
# Expected: 409 {"error":"email already exists in this or another org"}
```

### 1b.14 Remove a user from the org
```bash
curl -s -X DELETE "$BASE/api/v1/users/$EDITOR_ID" \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: 200 {"deleted":"uuid"}

# Removed user can no longer login
curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"engineer@acme.com","password":"MySecurePass123"}' | jq .
# Expected: 401
```

### 1b.15 Non-admin cannot manage users
```bash
# Viewer trying to invite another user
curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"another@acme.com","role":"viewer"}' | jq .
# Expected: 403

# Viewer trying to list users
curl -s -H "Authorization: Bearer $VIEWER_TOKEN" \
  $BASE/api/v1/users | jq .
# Expected: 403
```

> **Server config:** Change the default temp password by setting `DEFAULT_USER_PASSWORD=YourDefault@2024` in the cloud-api environment (docker-compose or Azure VM).

---

## 2. Qubes

> A **Qube** is a physical edge device (Raspberry Pi/ARM). The claim flow is how a customer takes ownership: they enter the `register_key` (printed on the device or in the device data file) and the Qube is linked to their org. After claiming, the device auto-registers via TP-API within 30s. Use `location_label` to group multiple Qubes at the same physical site — frontend uses this to show "Floor A has 3 Qubes".

### 2.1 List qubes — empty
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes | jq .
# Expected: [] (no qubes claimed yet)
```

### 2.2 Claim by register_key — valid
```bash
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' | jq .
# Expected: qube_id:"Q-1001", auth_token, message
```

### 2.3 Claim — already claimed
```bash
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' | jq .
# Expected: 409 {"error":"device is already claimed by an organisation"}
```

### 2.4 Claim — wrong register_key
```bash
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"XXXX-YYYY-ZZZZ"}' | jq .
# Expected: 404 {"error":"device not found — check the registration key"}
```

### 2.5 Claim second device (dev fallback — by qube_id)
```bash
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"qube_id":"Q-1002"}' | jq .
# Expected: qube_id:"Q-1002", auth_token
```

### 2.6 Claim — viewer role (forbidden)
```bash
# First create a viewer user in another terminal
VIEWER_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Viewer Org","email":"viewer@test.com","password":"pass1234"}' | jq -r .token)

curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1003-REG"}' | jq .
# Expected: 403 — note: viewer CAN claim (admin role required, admin is default on register)
# To test forbidden: need a user with viewer role specifically
```

### 2.7 List qubes — after claiming
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes | jq '.[].id'
# Expected: ["Q-1001","Q-1002"]
```

### 2.8 Get qube detail
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq .
# Expected: id, status, config_hash, recent_commands:[]
```

### 2.9 Get qube — not yours
```bash
# Another org's qube — use a fresh registration for different org
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1003 | jq .
# Expected: 404 {"error":"qube not found"}
```

### 2.10 Update qube location label
```bash
curl -s -X PUT $BASE/api/v1/qubes/Q-1001 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"location_label":"Building A - Floor 2"}' | jq .
# Expected: message, new_hash

curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq .location_label
# Expected: "Building A - Floor 2"
```

---

## 3. Commands

> **Commands** let you remotely control a Qube from the cloud. You POST a command, it enters a queue, and within 30s the Qube's conf-agent polls for it, executes it, and reports back. This is the only way to interact with a Qube without SSH. Useful commands: `ping` (network test), `restart_service` (restart a gateway container), `list_containers` (see what's running), `reload_config` (force a config resync), `get_logs` (retrieve container logs).

### 3.1 Send ping command
```bash
CMD_ID=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"ping","payload":{"target":"8.8.8.8"}}' | jq -r .command_id)
echo "CMD_ID=$CMD_ID"
# Expected: command_id, status:"pending", poll_url
```

### 3.2 Poll command result
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/commands/$CMD_ID | jq .
# Expected: status:"pending" then "executed" with result.latency_ms
# Poll every 2s until not pending
```

### 3.3 Send reload_config command
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"reload_config","payload":{}}' | jq .
# Expected: command_id, status:"pending"
```

### 3.4 Send list_containers command
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"list_containers","payload":{}}' | jq .
```

### 3.5 Send restart_service command
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"restart_service","payload":{"service":"panel-a"}}' | jq .
```

### 3.6 Send get_logs command
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"get_logs","payload":{"service":"panel-a","lines":50}}' | jq .
```

### 3.7 Unknown command — rejected
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"format_disk","payload":{}}' | jq .
# Expected: 400 {"error":"unknown command"}
```

### 3.8 Command to another org's qube
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1003/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"ping","payload":{}}' | jq .
# Expected: 404 {"error":"qube not found"}
```

---

## 4. Templates (device catalog)

> **Templates** are the device catalog — a template describes one type of device (e.g. "Schneider PM5100") with all its registers/OIDs/nodes. `is_global=true` means the IoT team owns it and all orgs can use it. `is_global=false` means your org created it for a custom device. When you add a sensor to a gateway, you pick a template — the template's register map becomes the CSV file on the Qube. Adding a new protocol type like BACnet would mean adding a new template with that protocol and extending the gateway image for it.

### 4.1 List all templates — global only at first
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/templates | jq '[.[] | {name,protocol,is_global}]'
# Expected: global templates from migration 003
# Schneider PM5100, Generic OPC-UA, GXT RT UPS, Generic MQTT JSON Sensor, APC UPS Battery
```

### 4.2 Filter by protocol
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=modbus_tcp" | jq '.[].name'
# Expected: only modbus templates

curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=snmp" | jq '.[].name'
# Expected: only snmp templates
```

### 4.3 Get template detail
```bash
TMPL_ID=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=modbus_tcp" | jq -r '.[0].id')
echo "TMPL_ID=$TMPL_ID"

curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/templates/$TMPL_ID | jq .
# Expected: full config_json with registers array, influx_fields_json
```

### 4.4 Preview template CSV output
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates/$TMPL_ID/preview?address_params=%7B%22unit_id%22%3A1%7D" | jq .
# Expected: protocol, csv_type:"registers", row_count, rows array

# Unencoded for readability:
# address_params={"unit_id":1}
```

### 4.5 Create org template — Modbus
```bash
ORG_TMPL_ID=$(curl -s -X POST $BASE/api/v1/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ABB B23 Energy Meter",
    "protocol": "modbus_tcp",
    "description": "ABB B23 three-phase energy meter",
    "config_json": {
      "registers": [
        {"address":0,  "register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"},
        {"address":40, "register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"active_power_w","table":"Measurements"},
        {"address":76, "register_type":"Holding","data_type":"float32","count":2,"scale":0.001,"field_key":"energy_kwh","table":"Measurements"}
      ]
    },
    "influx_fields_json": {
      "voltage_l1":    {"display_label":"Voltage L1","unit":"V"},
      "active_power_w":{"display_label":"Active Power","unit":"W"},
      "energy_kwh":    {"display_label":"Energy","unit":"kWh"}
    }
  }' | jq -r .id)
echo "ORG_TMPL_ID=$ORG_TMPL_ID"
# Expected: id, is_global:false
```

### 4.6 Create org template — SNMP
```bash
curl -s -X POST $BASE/api/v1/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Custom UPS Monitor",
    "protocol": "snmp",
    "description": "Custom UPS",
    "config_json": {
      "oids": [
        {"oid":"1.3.6.1.4.1.318.1.1.1.2.2.1.0","field_key":"battery_pct","type":"gauge"},
        {"oid":"1.3.6.1.4.1.318.1.1.1.4.2.1.0","field_key":"output_v","type":"gauge"}
      ]
    },
    "influx_fields_json": {
      "battery_pct":{"display_label":"Battery","unit":"%"},
      "output_v":   {"display_label":"Output Voltage","unit":"V"}
    }
  }' | jq -r .id)
```

### 4.7 Create org template — OPC-UA
```bash
curl -s -X POST $BASE/api/v1/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Custom OPC-UA Sensor",
    "protocol": "opcua",
    "config_json": {
      "nodes": [
        {"node_id":"ns=2;points/Temperature","field_key":"temperature","data_type":"float","table":"Measurements"},
        {"node_id":"ns=2;points/Pressure","field_key":"pressure","data_type":"float","table":"Measurements"}
      ]
    }
  }' | jq .
```

### 4.8 Create org template — MQTT
```bash
curl -s -X POST $BASE/api/v1/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Custom MQTT Sensor",
    "protocol": "mqtt",
    "config_json": {
      "topic_pattern": "{base_topic}/{topic_suffix}",
      "readings": [
        {"json_path":"$.temp","field_key":"temperature","unit":"C"},
        {"json_path":"$.hum","field_key":"humidity","unit":"%"}
      ]
    }
  }' | jq .
```

### 4.9 Create template — invalid protocol
```bash
curl -s -X POST $BASE/api/v1/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Bad","protocol":"bacnet","config_json":{}}' | jq .
# Expected: 400 {"error":"protocol must be modbus_tcp, mqtt, opcua, or snmp"}
```

### 4.10 Update full template
```bash
curl -s -X PUT $BASE/api/v1/templates/$ORG_TMPL_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ABB B23 Energy Meter v2",
    "description": "Updated description",
    "config_json": {
      "registers": [
        {"address":0,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"},
        {"address":40,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"active_power_w","table":"Measurements"},
        {"address":76,"register_type":"Holding","data_type":"float32","count":2,"scale":0.001,"field_key":"energy_kwh","table":"Measurements"},
        {"address":90,"register_type":"Holding","data_type":"float32","count":2,"scale":0.001,"field_key":"reactive_energy_kvarh","table":"Measurements"}
      ]
    },
    "influx_fields_json": {
      "voltage_l1":          {"display_label":"Voltage L1","unit":"V"},
      "active_power_w":      {"display_label":"Active Power","unit":"W"},
      "energy_kwh":          {"display_label":"Energy","unit":"kWh"},
      "reactive_energy_kvarh":{"display_label":"Reactive Energy","unit":"kVArh"}
    }
  }' | jq .
# Expected: {"updated":true,"id":"..."}
```

### 4.11 Patch single register — add (superadmin only)
```bash
curl -s -X PATCH $BASE/api/v1/templates/$TMPL_ID/registers \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "action": "add",
    "entry": {
      "address":100,"register_type":"Holding","data_type":"uint16",
      "count":1,"scale":0.01,"field_key":"power_factor","table":"Measurements"
    }
  }' | jq '{updated,total_entries}'
# Expected: {"updated":true,"total_entries":N+1}
```

### 4.12 Patch single register — update (superadmin only)
```bash
curl -s -X PATCH $BASE/api/v1/templates/$TMPL_ID/registers \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "action": "update",
    "index": 0,
    "entry": {
      "address":3000,"register_type":"Holding","data_type":"float32",
      "count":2,"scale":0.1,"field_key":"active_power_w","table":"Measurements"
    }
  }' | jq '{updated,total_entries}'
# Expected: {"updated":true,"total_entries":N}
```

### 4.13 Patch — delete register (superadmin only)
```bash
curl -s -X PATCH $BASE/api/v1/templates/$TMPL_ID/registers \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","index":0}' | jq '{updated,total_entries}'
# Expected: total_entries decremented by 1
```

### 4.14 Patch global template — regular user forbidden
```bash
curl -s -X PATCH $BASE/api/v1/templates/$TMPL_ID/registers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"add","entry":{"address":999}}' | jq .
# Expected: 403 {"error":"global templates only editable by superadmin"}
```

### 4.15 Patch — invalid index
```bash
curl -s -X PATCH $BASE/api/v1/templates/$TMPL_ID/registers \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","index":9999}' | jq .
# Expected: 400 {"error":"index out of range"}
```

### 4.16 Delete org template
```bash
curl -s -X DELETE $BASE/api/v1/templates/$ORG_TMPL_ID \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: {"deleted":true}
```

### 4.17 Delete global template — regular user forbidden
```bash
curl -s -X DELETE $BASE/api/v1/templates/$TMPL_ID \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: 403 {"error":"global templates can only be deleted by superadmin"}
```

---

## 5. Gateways

> A **gateway** is a Docker container on the Qube that talks to one physical device or broker. Protocol determines which container image runs. Creating a gateway also creates a `service` record (the Docker service definition). Two gateways of the same protocol on the same Qube = two separate containers with different config files — e.g. `panel-a` polls 192.168.1.100 and `panel-b` polls 192.168.1.101, both running `modbus-gateway` image.

```bash
# Save template IDs for use in sensor tests
MODBUS_TMPL=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=modbus_tcp" | jq -r '.[0].id')
SNMP_TMPL=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=snmp" | jq -r '.[0].id')
OPCUA_TMPL=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=opcua" | jq -r '.[0].id')
MQTT_TMPL=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/templates?protocol=mqtt" | jq -r '.[0].id')
```

### 5.1 List gateways — empty
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001/gateways | jq .
# Expected: []
```

### 5.2 Add Modbus TCP gateway
```bash
GW_MODBUS=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"Panel_A",
    "protocol":"modbus_tcp",
    "host":"192.168.1.100",
    "port":502,
    "config_json":{"unit_id":1,"poll_interval_ms":5000}
  }' | jq -r .gateway_id)
echo "GW_MODBUS=$GW_MODBUS"
# Expected: gateway_id, service_id, new_hash
```

### 5.3 Add second Modbus gateway (different device)
```bash
GW_MODBUS2=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"Panel_B",
    "protocol":"modbus_tcp",
    "host":"192.168.1.101",
    "port":502,
    "config_json":{"unit_id":1,"poll_interval_ms":5000}
  }' | jq -r .gateway_id)
echo "GW_MODBUS2=$GW_MODBUS2"
# Expected: separate gateway_id — two modbus containers will be deployed
```

### 5.4 Add OPC-UA gateway
```bash
GW_OPCUA=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"PlantOPC",
    "protocol":"opcua",
    "host":"opc.tcp://192.168.1.18:52520/OPCUA/Server",
    "port":52520
  }' | jq -r .gateway_id)
echo "GW_OPCUA=$GW_OPCUA"
```

### 5.5 Add SNMP gateway
```bash
GW_SNMP=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"UPS_Room1",
    "protocol":"snmp",
    "host":"192.168.1.200",
    "config_json":{"community":"public","version":"2c"}
  }' | jq -r .gateway_id)
echo "GW_SNMP=$GW_SNMP"
```

### 5.6 Add MQTT gateway
```bash
GW_MQTT=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"MQTTFloor2",
    "protocol":"mqtt",
    "host":"192.168.1.10",
    "port":1883,
    "config_json":{
      "broker_url":"tcp://192.168.1.10:1883",
      "base_topic":"factory/floor2"
    }
  }' | jq -r .gateway_id)
echo "GW_MQTT=$GW_MQTT"
```

### 5.7 Add gateway — invalid protocol
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Bad","protocol":"bacnet","host":"1.2.3.4"}' | jq .
# Expected: 400 {"error":"protocol must be modbus_tcp, mqtt, opcua, or snmp"}
```

### 5.8 Add gateway — missing name
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"protocol":"modbus_tcp","host":"192.168.1.100"}' | jq .
# Expected: 400 {"error":"name and protocol are required"}
```

### 5.9 Add gateway — qube not yours
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1003/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","protocol":"modbus_tcp","host":"1.2.3.4"}' | jq .
# Expected: 404 {"error":"qube not found"}
```

### 5.10 List gateways — all 5 showing
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001/gateways | jq '[.[] | {name,protocol,sensor_count}]'
# Expected: panel-a (modbus), panel-b (modbus), plantopc (opcua), ups-room1 (snmp), mqttfloor2 (mqtt)
```

### 5.11 Delete gateway
```bash
curl -s -X DELETE $BASE/api/v1/gateways/$GW_MODBUS2 \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: {"deleted":true,"new_hash":"...","message":"..."}

# Verify it's gone
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001/gateways | jq length
# Expected: 4 (was 5)
```

### 5.12 Delete gateway — not yours
```bash
curl -s -X DELETE $BASE/api/v1/gateways/00000000-0000-0000-0000-000000000000 \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: 404 {"error":"gateway not found"}
```

---

## 6. Sensors

> A **sensor** is an instance of a template attached to a gateway. Creating a sensor auto-generates `service_csv_rows` in Postgres — one row per register/node/OID from the template. These rows become the `config.csv` file on the Qube. `address_params` lets you customise per-sensor (e.g. Modbus unit ID, OPC-UA node override). `tags_json` adds metadata that flows through to InfluxDB tags and Postgres readings.

### 6.1 List sensors — empty gateway
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/gateways/$GW_MODBUS/sensors | jq .
# Expected: []
```

### 6.2 Add Modbus sensor — from global template
```bash
SENSOR_MODBUS=$(curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Main_Meter\",
    \"template_id\": \"$MODBUS_TMPL\",
    \"address_params\": {\"unit_id\": 1, \"register_offset\": 0},
    \"tags_json\": {\"location\": \"panel_a\", \"building\": \"HQ\"}
  }" | jq -r .sensor_id)
echo "SENSOR_MODBUS=$SENSOR_MODBUS"
# Expected: sensor_id, csv_rows:6, new_hash
```

### 6.3 Add second Modbus sensor — same gateway, different unit_id
```bash
SENSOR_MODBUS2=$(curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Sub_Meter_1\",
    \"template_id\": \"$MODBUS_TMPL\",
    \"address_params\": {\"unit_id\": 2},
    \"tags_json\": {\"location\": \"panel_a\", \"circuit\": \"sub1\"}
  }" | jq -r .sensor_id)
echo "SENSOR_MODBUS2=$SENSOR_MODBUS2"
# Expected: sensor_id, csv_rows:6
```

### 6.4 Add OPC-UA sensor
```bash
SENSOR_OPCUA=$(curl -s -X POST "$BASE/api/v1/gateways/$GW_OPCUA/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Pasteuriser_1\",
    \"template_id\": \"$OPCUA_TMPL\",
    \"address_params\": {\"freq_sec\": 15},
    \"tags_json\": {\"line\": \"line1\"}
  }" | jq -r .sensor_id)
echo "SENSOR_OPCUA=$SENSOR_OPCUA"
```

### 6.5 Add SNMP sensor
```bash
SENSOR_SNMP=$(curl -s -X POST "$BASE/api/v1/gateways/$GW_SNMP/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"UPS_Main\",
    \"template_id\": \"$SNMP_TMPL\",
    \"address_params\": {\"community\": \"public\"},
    \"tags_json\": {\"location\": \"server_room\"}
  }" | jq -r .sensor_id)
echo "SENSOR_SNMP=$SENSOR_SNMP"
```

### 6.6 Add MQTT sensor
```bash
SENSOR_MQTT=$(curl -s -X POST "$BASE/api/v1/gateways/$GW_MQTT/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Env_Sensor_01\",
    \"template_id\": \"$MQTT_TMPL\",
    \"address_params\": {\"topic_suffix\": \"env_01\"},
    \"tags_json\": {\"floor\": \"2\"}
  }" | jq -r .sensor_id)
echo "SENSOR_MQTT=$SENSOR_MQTT"
```

### 6.7 Add sensor — protocol mismatch
```bash
curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"Bad\",\"template_id\":\"$SNMP_TMPL\",\"address_params\":{}}" | jq .
# Expected: 400 {"error":"template protocol (snmp) does not match gateway protocol (modbus_tcp)"}
```

### 6.8 Add sensor — template not found
```bash
curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Bad","template_id":"00000000-0000-0000-0000-000000000000","address_params":{}}' | jq .
# Expected: 404 {"error":"template not found"}
```

### 6.9 List sensors for one gateway
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/gateways/$GW_MODBUS/sensors | jq '[.[] | {name,template_name}]'
# Expected: Main_Meter, Sub_Meter_1
```

### 6.10 List all sensors for qube
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001/sensors | jq '[.[] | {name,protocol,gateway_name}]'
# Expected: all 5 sensors across all gateways
```

### 6.11 Delete sensor
```bash
curl -s -X DELETE $BASE/api/v1/sensors/$SENSOR_MODBUS2 \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: {"deleted":true,"new_hash":"..."}
```

---

## 7. Sensor CSV rows

### 7.1 View all rows for a sensor
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/sensors/$SENSOR_MODBUS/rows | jq .
# Expected: rows array, each with id, csv_type:"registers", row_data, row_order
# Verify row_data has: Equipment, Reading, RegType, Address, Type, Output, Table, Tags
```

### 7.2 Update a row — fix wrong address
```bash
# Get a row ID first
ROW_ID=$(curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/sensors/$SENSOR_MODBUS/rows | jq -r '.rows[0].id')

curl -s -X PUT "$BASE/api/v1/sensors/$SENSOR_MODBUS/rows/$ROW_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "row_data": {
      "Equipment": "Main_Meter",
      "Reading":   "active_power_w",
      "RegType":   "Holding",
      "Address":   3002,
      "Type":      "float32",
      "Output":    "influxdb",
      "Table":     "Measurements",
      "Tags":      "location=panel_a,building=HQ"
    }
  }' | jq .
# Expected: {"updated":true,"new_hash":"...","message":"..."}
```

### 7.3 Add extra row — new reading not in template
```bash
curl -s -X POST "$BASE/api/v1/sensors/$SENSOR_MODBUS/rows" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "row_data": {
      "Equipment": "Main_Meter",
      "Reading":   "reactive_power_var",
      "RegType":   "Holding",
      "Address":   3060,
      "Type":      "float32",
      "Output":    "influxdb",
      "Table":     "Measurements",
      "Tags":      "location=panel_a"
    }
  }' | jq .
# Expected: {"row_id":"...","new_hash":"...","message":"..."}
```

### 7.4 Delete a row
```bash
NEW_ROW_ID=$(curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/sensors/$SENSOR_MODBUS/rows | jq -r '.rows[-1].id')

curl -s -X DELETE "$BASE/api/v1/sensors/$SENSOR_MODBUS/rows/$NEW_ROW_ID" \
  -H "Authorization: Bearer $TOKEN" | jq .
# Expected: {"deleted":true,"new_hash":"..."}
```

### 7.5 Update row — wrong sensor_id
```bash
curl -s -X PUT "$BASE/api/v1/sensors/$SENSOR_MODBUS/rows/00000000-0000-0000-0000-000000000000" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"row_data":{"Address":999}}' | jq .
# Expected: 404 {"error":"row not found"}
```

---

## 8. TP-API (Qube-facing — debug scenarios)

```bash
# Use the QUBE_TOKEN from the claim step
QUBE_TOKEN="<auth_token from step 2.2>"
QUBE_ID="Q-1001"
```

### 8.1 Device self-register — device not yet claimed
```bash
# Use a fresh unclaimed device
curl -s -X POST $TPBASE/v1/device/register \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1003","register_key":"TEST-Q1003-REG"}' | jq .
# Expected: 202 {"status":"pending","retry_secs":60,...}
```

### 8.2 Device self-register — device already claimed
```bash
# After Q-1001 was claimed in step 2.2
curl -s -X POST $TPBASE/v1/device/register \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' | jq .
# Expected: 200 {"status":"claimed","qube_token":"..."}
```

### 8.3 Device self-register — wrong register_key
```bash
curl -s -X POST $TPBASE/v1/device/register \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1001","register_key":"WRONG-KEY-HERE"}' | jq .
# Expected: 401 {"error":"device not found or invalid register key"}
```

### 8.4 Heartbeat
```bash
curl -s -X POST $TPBASE/v1/heartbeat \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' | jq .
# Expected: {"acknowledged":true,"server_time":"...","qube_id":"Q-1001"}

# Verify qube is now online
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq .status
# Expected: "online"
```

### 8.5 Heartbeat — invalid token
```bash
curl -s -X POST $TPBASE/v1/heartbeat \
  -H "X-Qube-ID: Q-1001" \
  -H "Authorization: Bearer wrongtoken" \
  -H "Content-Type: application/json" \
  -d '{}' | jq .
# Expected: 401 {"error":"invalid qube token"}
```

### 8.6 Heartbeat — missing headers
```bash
curl -s -X POST $TPBASE/v1/heartbeat \
  -H "Content-Type: application/json" \
  -d '{}' | jq .
# Expected: 401 {"error":"X-Qube-ID and Authorization headers required"}
```

### 8.7 Sync state — check hash
```bash
curl -s \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  $TPBASE/v1/sync/state | jq .
# Expected: {"qube_id":"Q-1001","hash":"<sha256>","updated_at":"..."}
# Hash changes every time a gateway/sensor is added/deleted
```

### 8.8 Sync config — download full config
```bash
curl -s \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  $TPBASE/v1/sync/config | jq '{
    hash,
    csv_files: (.csv_files | keys),
    sensor_map_size: (.sensor_map | length),
    compose_services: (.docker_compose_yml | split("\n") | map(select(startswith("  ") and endswith(":"))) | length)
  }'
# Expected:
#   hash: matches sync/state hash
#   csv_files: ["configs/panel-a/config.csv","configs/panel-a/configs.yml",...]
#   sensor_map_size: number of Equipment.Reading keys
#   compose_services: count of service blocks in compose YAML
```

### 8.9 Sync config — verify CSV content is correct format
```bash
curl -s \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  $TPBASE/v1/sync/config | jq -r '.csv_files["configs/panel-a/config.csv"]'
# Expected (Modbus format):
# #Equipment,Reading,RegType,Address,type,Output,Table,Tags
# Main_Meter,active_power_w,Holding,3000,float32,influxdb,Measurements,location=panel_a...
```

### 8.10 Sync config — verify sensor_map
```bash
curl -s \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  $TPBASE/v1/sync/config | jq .sensor_map
# Expected: {"Main_Meter.active_power_w":"<sensor-uuid>","Main_Meter.voltage_ll_v":"<sensor-uuid>",...}
```

### 8.11 Poll commands
```bash
curl -s -X POST $TPBASE/v1/commands/poll \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' | jq .
# Expected: {"commands":[...]} — list of pending commands

# Send a command from Cloud API first, then poll
CMD_ID=$(curl -s -X POST $BASE/api/v1/qubes/Q-1001/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"ping","payload":{"target":"8.8.8.8"}}' | jq -r .command_id)

curl -s -X POST $TPBASE/v1/commands/poll \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.commands[0].command'
# Expected: "ping"
```

### 8.12 Acknowledge command
```bash
curl -s -X POST "$TPBASE/v1/commands/$CMD_ID/ack" \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"executed","result":{"output":"PING 8.8.8.8: 64 bytes, time=12ms","latency_ms":12}}' | jq .
# Expected: {"acknowledged":true,"status":"executed"}

# Verify via Cloud API
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/commands/$CMD_ID | jq '{status,result}'
# Expected: status:"executed", result with latency_ms
```

### 8.13 Telemetry ingest — batch of readings
```bash
curl -s -X POST $TPBASE/v1/telemetry/ingest \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"readings\": [
      {\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"sensor_id\":\"$SENSOR_MODBUS\",\"field_key\":\"active_power_w\",\"value\":1250.5,\"unit\":\"W\"},
      {\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"sensor_id\":\"$SENSOR_MODBUS\",\"field_key\":\"voltage_ll_v\",\"value\":231.2,\"unit\":\"V\"},
      {\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"sensor_id\":\"$SENSOR_MODBUS\",\"field_key\":\"current_a\",\"value\":5.4,\"unit\":\"A\"},
      {\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"sensor_id\":\"$SENSOR_MODBUS\",\"field_key\":\"energy_kwh\",\"value\":12045.3,\"unit\":\"kWh\"}
    ]
  }" | jq .
# Expected: {"inserted":4,"failed":0,"total":4}
```

### 8.14 Telemetry ingest — too large batch
```bash
# Build a batch of 5001 readings
python3 -c "
import json, sys
readings = [{'sensor_id':'$SENSOR_MODBUS','field_key':'v','value':1.0,'unit':'V'} for _ in range(5001)]
print(json.dumps({'readings':readings}))" | \
curl -s -X POST $TPBASE/v1/telemetry/ingest \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d @- | jq .
# Expected: 400 {"error":"batch too large — max 5000 readings per request"}
```

### 8.15 Telemetry ingest — empty batch
```bash
curl -s -X POST $TPBASE/v1/telemetry/ingest \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"readings":[]}' | jq .
# Expected: {"inserted":0}
```

---

## 9. Telemetry data queries

### 9.1 Latest values — after ingest from 8.13
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/sensors/$SENSOR_MODBUS/latest" | jq .
# Expected: sensor_name:"Main_Meter", fields:[{field_key,value,unit,time}]
# Should show active_power_w:1250.5, voltage_ll_v:231.2, etc.
```

### 9.2 Latest values — sensor with no data
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/sensors/$SENSOR_OPCUA/latest" | jq .
# Expected: fields:[] (no data ingested for this sensor yet)
```

### 9.3 Historical readings — last 24h
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/readings?sensor_id=$SENSOR_MODBUS" | jq '{count,from,to}'
# Expected: count:4 (from ingest in 8.13)
```

### 9.4 Historical readings — filter by field
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/readings?sensor_id=$SENSOR_MODBUS&field=active_power_w" | jq .readings
# Expected: only active_power_w readings
```

### 9.5 Historical readings — custom time range
```bash
FROM=$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || \
       date -u -v-2H +%Y-%m-%dT%H:%M:%SZ)  # macOS / Linux
TO=$(date -u +%Y-%m-%dT%H:%M:%SZ)

curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/readings?sensor_id=$SENSOR_MODBUS&from=${FROM}&to=${TO}" | jq .count
# Expected: 4 (all readings within last 2 hours)
```

### 9.6 Historical readings — missing sensor_id
```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/readings" | jq .
# Expected: 400 {"error":"sensor_id is required"}
```

### 9.7 Latest values — another org's sensor
```bash
# Use a token from a different org
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/sensors/00000000-0000-0000-0000-000000000000/latest" | jq .
# Expected: 404 {"error":"sensor not found"}
```

---

## 10. Conf-agent self-registration flow (end-to-end)

```bash
# Watch conf-agent logs during these steps
docker compose -f docker-compose.dev.yml logs -f conf-agent &

# 1. Verify conf-agent reads mit.txt and calls /v1/device/register
# → Should see: [agent] Device identity from mit.txt: id=Q-1001 reg=TEST-Q1001-REG
# → Should see: [register] Polling for claim status...

# 2. Claim the device (if not already done)
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' | jq .

# 3. Within 60s conf-agent should:
# → [register] Device claimed! Token received.
# → [register] Token saved to /opt/qube/.env
# → [heartbeat] sent ok
# → [sync] remote=<hash>... local=(none)
# → [sync] hash mismatch — downloading config
# → [apply] wrote docker-compose.yml
# → [apply] wrote csv: configs/panel-a/config.csv
# → [apply] wrote sensor_map.json

# 4. Verify files were written
ls -la dev-qube-workdir/
ls -la dev-qube-workdir/configs/
cat dev-qube-workdir/sensor_map.json | jq .

# 5. Check qube is online
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq .status
# Expected: "online"
```

---

## 11. Full pipeline test (inject data → verify readings)

```bash
# 1. Run the influx seeder to inject test data matching sensor names
docker compose -f docker-compose.dev.yml run --rm influx-seeder

# 2. Wait ~60s for enterprise-influx-to-sql to run
docker compose -f docker-compose.dev.yml logs -f enterprise-influx-to-sql &
sleep 70

# 3. Query latest readings — should now have data
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/sensors/$SENSOR_MODBUS/latest" | jq .
# Expected: fields with real values from InfluxDB

# 4. Verify multi-sensor isolation — sensors from different qubes don't mix
# Claim Q-1002 with another org, add sensor, ingest data, verify Q-1001 readings unchanged
```

---

## 12. Multi-qube isolation test

```bash
# Register a second org
TOKEN2=$(curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Other Corp","email":"admin@other.com","password":"pass1234"}' | jq -r .token)

# Claim Q-1002 for second org
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN2" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1002-REG"}' | jq .

# Second org cannot see first org's qube
curl -s -H "Authorization: Bearer $TOKEN2" \
  $BASE/api/v1/qubes/Q-1001 | jq .
# Expected: 404 {"error":"qube not found"}

# Second org cannot see first org's sensors
curl -s -H "Authorization: Bearer $TOKEN2" \
  "$BASE/api/v1/data/sensors/$SENSOR_MODBUS/latest" | jq .
# Expected: 404 {"error":"sensor not found"}

# First org cannot see second org's qube
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1002 | jq .
# Expected: 404 {"error":"qube not found"}
```

---

## 13. Error and edge cases

### 13.1 Very long sensor name
```bash
curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$(python3 -c "print('A'*300)")\",\"template_id\":\"$MODBUS_TMPL\",\"address_params\":{}}" | jq .
# Should either succeed or return a clean error — not crash
```

### 13.2 Gateway name that becomes a service name
```bash
curl -s -X POST $BASE/api/v1/qubes/Q-1001/gateways \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Panel A #2 (Main)","protocol":"modbus_tcp","host":"192.168.1.200","port":502}' | jq .service_name
# Expected: "panel-a--2--main" (sanitized to lowercase, special chars → dash)
```

### 13.3 Ingest readings for sensor from wrong qube
```bash
# Use Q-1002's token to ingest readings for Q-1001's sensor
QUBE2_TOKEN="<Q-1002 auth_token from claim>"
curl -s -X POST $TPBASE/v1/telemetry/ingest \
  -H "X-Qube-ID: Q-1002" \
  -H "Authorization: Bearer $QUBE2_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"readings\":[{\"sensor_id\":\"$SENSOR_MODBUS\",\"field_key\":\"v\",\"value\":1.0}]}" | jq .
# Note: This will insert the row but with qube_id=Q-1002
# The data will NOT appear when querying via TOKEN (org1) because sensor ownership check
```

### 13.4 Concurrent requests — hash consistency
```bash
# Add 5 sensors simultaneously
for i in 1 2 3 4 5; do
  curl -s -X POST "$BASE/api/v1/gateways/$GW_MODBUS/sensors" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"Concurrent_$i\",\"template_id\":\"$MODBUS_TMPL\",\"address_params\":{\"unit_id\":$i}}" &
done
wait

# All should succeed and config hash should reflect all sensors
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq '{config_hash,sensor_count:.recent_commands|length}'
```

---

## Summary checklist

Run these after testing all scenarios to confirm everything is working:

```bash
echo "=== Final state check ==="

echo "Qubes claimed:"
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/v1/qubes | jq length

echo "Gateways on Q-1001:"
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/v1/qubes/Q-1001/gateways | jq length

echo "Sensors on Q-1001:"
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/v1/qubes/Q-1001/sensors | jq length

echo "Templates available:"
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/v1/templates | jq length

echo "Latest readings for Main_Meter:"
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/data/sensors/$SENSOR_MODBUS/latest" | jq '.fields | length'

echo "Qube online status:"
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/v1/qubes/Q-1001 | jq .status

echo "Config hash (should match between cloud and agent):"
curl -s -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/qubes/Q-1001 | jq .config_hash
curl -s -H "X-Qube-ID: Q-1001" -H "Authorization: Bearer $QUBE_TOKEN" \
  $TPBASE/v1/sync/state | jq .hash
# Both hashes must be identical
```

---

## 24. Registry Configuration (superadmin only)

```bash
SUPER_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' | jq -r .token)

# Check current registry settings
curl -s -H "Authorization: Bearer $SUPER_TOKEN" \
  $BASE/api/v1/admin/registry | jq .
# Expected: mode, github_base, gitlab_base, images, resolved

# Switch to GitHub single-repo mode (testing)
curl -s -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}' \
  $BASE/api/v1/admin/registry | jq .
# Expected: resolved.conf_agent = "ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest"

# Switch to GitLab separate-repo mode (production)
curl -s -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"gitlab","gitlab_base":"registry.gitlab.com/iot-team4/product"}' \
  $BASE/api/v1/admin/registry | jq .
# Expected: resolved.conf_agent = "registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest"

# Override a single image (custom mode useful for testing a new image)
curl -s -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"custom","img_modbus":"my-registry.com/my-modbus:v2.1.0"}' \
  $BASE/api/v1/admin/registry | jq .

# Non-superadmin cannot access registry config
curl -s -X GET -H "Authorization: Bearer $TOKEN" \
  $BASE/api/v1/admin/registry
# Expected: 403 Forbidden
```
