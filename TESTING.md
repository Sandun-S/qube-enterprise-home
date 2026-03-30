# Qube Enterprise v2 — Testing Scenarios

Manual curl-based test scenarios for all major API flows.
Run in order — later scenarios depend on IDs from earlier ones.

```bash
BASE="http://localhost:8080"
TPBASE="http://localhost:8081"
```

---

## 0. Health checks

```bash
curl -s $BASE/health | jq .
# {"status":"ok","service":"cloud-api","version":"2","ws_connections":0}

curl -s $TPBASE/health | jq .
# {"status":"ok","service":"tp-api","version":"2"}
```

---

## 1. Authentication

### Register new organisation

```bash
curl -s -X POST $BASE/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Acme Corp","email":"admin@acme.com","password":"secure123"}' | jq .
# Returns: {"token":"<jwt>","user_id":"<uuid>","org_id":"<uuid>","role":"admin"}
TOKEN=<paste token>
```

### Login

```bash
curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":"secure123"}' | jq .
TOKEN=<paste token>
```

### Superadmin login (IoT team)

```bash
curl -s -X POST $BASE/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' | jq .
SA_TOKEN=<paste token>
```

---

## 2. Qubes — claim a device

```bash
# Claim Q-1001 using its factory register key
curl -s -X POST $BASE/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' | jq .
# Returns: {"qube_id":"Q-1001","auth_token":"<hmac>","message":"..."}
QUBE_ID=Q-1001
QUBE_TOKEN=<paste auth_token>

# List your qubes
curl -s $BASE/api/v1/qubes \
  -H "Authorization: Bearer $TOKEN" | jq .

# Get one qube
curl -s $BASE/api/v1/qubes/$QUBE_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Update location
curl -s -X PUT $BASE/api/v1/qubes/$QUBE_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"location_label":"Server Room A, Rack 3"}' | jq .

# List all sensors across all readers for a qube
curl -s $BASE/api/v1/qubes/$QUBE_ID/sensors \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,reader_id}]'

# List containers deployed on a qube
curl -s $BASE/api/v1/qubes/$QUBE_ID/containers \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,image,status}]'
```

---

## 3. TP-API — device self-registration

The conf-agent calls this on boot. You can simulate it:

```bash
# Q-1001 is claimed — returns token
curl -s -X POST $TPBASE/v1/device/register \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' | jq .
# {"status":"claimed","device_id":"Q-1001","qube_token":"<hmac>"}

# Q-1003 is not yet claimed — returns pending
curl -s -X POST $TPBASE/v1/device/register \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1003","register_key":"TEST-Q1003-REG"}' | jq .
# {"status":"pending","retry_secs":60}
```

---

## 4. TP-API — heartbeat and sync state

```bash
# Heartbeat (conf-agent sends this every POLL_INTERVAL seconds)
curl -s -X POST $TPBASE/v1/heartbeat \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"online","mem_free_mb":512,"disk_free_gb":20}' | jq .

# Check current config hash
curl -s $TPBASE/v1/sync/state \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq .
# {"qube_id":"Q-1001","hash":"...","config_version":1,"updated_at":"..."}
```

---

## 5. Protocols

```bash
curl -s $BASE/api/v1/protocols \
  -H "Authorization: Bearer $TOKEN" | jq '[.[].id]'
# ["modbus_tcp","snmp","mqtt","opcua","http"]
```

---

## 6. Reader templates (superadmin manages these)

```bash
# List (all users can read)
curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,protocol,name,image_suffix}]'

MODBUS_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="modbus_tcp") | .id')

# Get a single reader template
curl -s $BASE/api/v1/reader-templates/$MODBUS_RT_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Superadmin: create a reader template
curl -s -X POST $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "protocol": "lorawan",
    "name": "LoRaWAN NS Reader",
    "description": "Connects to a LoRaWAN network server",
    "image_suffix": "lorawan-reader",
    "connection_schema": {
      "type":"object",
      "properties": {
        "ns_host": {"type":"string","title":"Network Server Host"},
        "app_id":  {"type":"string","title":"Application ID"}
      }
    },
    "env_defaults": {"LOG_LEVEL":"info"}
  }' | jq .
RT_ID=<paste id>

# Superadmin: update a reader template
curl -s -X PUT $BASE/api/v1/reader-templates/$RT_ID \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"description":"Updated description","env_defaults":{"LOG_LEVEL":"debug"}}' | jq .

# Superadmin: delete a reader template
curl -s -X DELETE $BASE/api/v1/reader-templates/$RT_ID \
  -H "Authorization: Bearer $SA_TOKEN" | jq .
```

---

## 7. Device templates

```bash
# List device templates (org + global)
curl -s $BASE/api/v1/device-templates \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,protocol,is_global}]'

# Filter by protocol
curl -s "$BASE/api/v1/device-templates?protocol=modbus_tcp" \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name}]'

# Get a single device template
curl -s $BASE/api/v1/device-templates/$DT_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Create an org-level template
curl -s -X POST $BASE/api/v1/device-templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Janitza UMG-96RM",
    "protocol": "modbus_tcp",
    "manufacturer": "Janitza",
    "model": "UMG-96RM",
    "sensor_config": {
      "registers": [
        {"name":"active_power_w","address":1294,"type":"float32","unit":"W"},
        {"name":"voltage_v","address":1290,"type":"float32","unit":"V"},
        {"name":"current_a","address":1302,"type":"float32","unit":"A"}
      ]
    },
    "sensor_params_schema": {
      "type":"object",
      "properties":{
        "unit_id":{"type":"integer","title":"Modbus Unit ID","default":1}
      }
    }
  }' | jq .
DT_ID=<paste id>

# Update device template metadata
curl -s -X PUT $BASE/api/v1/device-templates/$DT_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"description":"Adds frequency reading"}' | jq .

# Patch sensor_config — full replacement
curl -s -X PATCH $BASE/api/v1/device-templates/$DT_ID/config \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sensor_config": {
      "registers": [
        {"name":"active_power_w","address":1294,"type":"float32","unit":"W"},
        {"name":"voltage_v","address":1290,"type":"float32","unit":"V"},
        {"name":"frequency_hz","address":1300,"type":"float32","unit":"Hz"}
      ]
    }
  }' | jq .

# Patch sensor_config — add a single entry
curl -s -X PATCH $BASE/api/v1/device-templates/$DT_ID/config \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"add","entry":{"name":"current_a","address":1302,"type":"float32","unit":"A"}}' | jq .

# Patch sensor_config — update entry at index 0
curl -s -X PATCH $BASE/api/v1/device-templates/$DT_ID/config \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"update","index":0,"entry":{"name":"active_power_w","address":1294,"type":"float32","unit":"W","scale":0.001}}' | jq .

# Patch sensor_config — delete entry at index 2
curl -s -X PATCH $BASE/api/v1/device-templates/$DT_ID/config \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","index":2}' | jq .
```

---

## 8. Readers — create for each protocol

```bash
# Modbus TCP reader
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Main PLC Reader\",
    \"protocol\": \"modbus_tcp\",
    \"template_id\": \"$MODBUS_RT_ID\",
    \"config_json\": {\"host\":\"192.168.10.1\",\"port\":502,\"poll_interval_sec\":20}
  }" | jq .
READER_ID=<paste reader_id>

# SNMP reader
SNMP_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="snmp") | .id')

curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"SNMP Network Reader\",
    \"protocol\": \"snmp\",
    \"template_id\": \"$SNMP_RT_ID\",
    \"config_json\": {\"fetch_interval_sec\":30,\"timeout_sec\":10}
  }" | jq .
SNMP_READER_ID=<paste reader_id>

# MQTT reader
MQTT_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="mqtt") | .id')

curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"MQTT Floor 2\",
    \"protocol\": \"mqtt\",
    \"template_id\": \"$MQTT_RT_ID\",
    \"config_json\": {\"broker\":\"tcp://192.168.1.10:1883\",\"client_id\":\"qube-floor2\",\"poll_interval_sec\":10}
  }" | jq .
MQTT_READER_ID=<paste reader_id>

# Get a single reader
curl -s $BASE/api/v1/readers/$READER_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Update reader config (e.g. change poll interval)
curl -s -X PUT $BASE/api/v1/readers/$READER_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"config_json":{"host":"192.168.10.1","port":502,"poll_interval_sec":10}}' | jq .
# Returns: {"updated":true,"new_hash":"..."}

# List readers
curl -s $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,protocol,sensor_count}]'
```

---

## 9. Sensors — add to readers

```bash
# Modbus sensor using device template
curl -s -X POST $BASE/api/v1/readers/$READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Main Energy Meter\",
    \"template_id\": \"$DT_ID\",
    \"params\": {\"unit_id\":1},
    \"tags_json\": {\"location\":\"MDB\",\"phase\":\"3P\"},
    \"output\": \"influxdb\",
    \"table_name\": \"Measurements\"
  }" | jq .
SENSOR_ID=<paste sensor_id>

# Sensor without template — manual config_json
curl -s -X POST $BASE/api/v1/readers/$READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Panel B Meter",
    "params": {
      "unit_id": 2,
      "registers": [
        {"name":"active_power_w","address":1294,"type":"float32","unit":"W"}
      ]
    },
    "output": "influxdb,live"
  }' | jq .

# SNMP sensor with OIDs
curl -s -X POST $BASE/api/v1/readers/$SNMP_READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Main UPS",
    "params": {
      "ip_address": "192.168.1.100",
      "community": "public",
      "version": "2c"
    },
    "output": "influxdb"
  }' | jq .

# MQTT sensor
curl -s -X POST $BASE/api/v1/readers/$MQTT_READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Temperature Sensor Floor 2",
    "params": {
      "topic": "sensors/floor2/temp",
      "json_path": "$.value"
    },
    "output": "influxdb,live"
  }' | jq .

# List sensors for a reader
curl -s $BASE/api/v1/readers/$READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,output}]'

# Update sensor tags or output mode
curl -s -X PUT $BASE/api/v1/sensors/$SENSOR_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tags_json":{"location":"MDB","phase":"3P","floor":"1"},"output":"influxdb,live"}' | jq .
# Returns: {"updated":true,"new_hash":"..."}
```

---

## 10. Config hash and sync config

```bash
# After adding readers/sensors — hash should have changed
curl -s $TPBASE/v1/sync/state \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq .

# Download full config (what conf-agent does on hash mismatch)
curl -s $TPBASE/v1/sync/config \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq '{
    hash,
    config_version,
    reader_count: (.readers | length),
    container_count: (.containers | length),
    has_compose: (.docker_compose_yml != "")
  }'
```

---

## 11. Commands

```bash
# Send a command (cloud → Qube)
# Returns 202 Accepted — command is queued; delivered via WebSocket if connected, else DB queue
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"ping","payload":{"target":"cloud"}}' | jq .
# Returns: {"command_id":"<uuid>","status":"pending","delivery":"websocket|queued","poll_url":"..."}
CMD_ID=<paste command_id>

# Valid commands: ping, restart_qube, restart_reader, stop_container,
#                reload_config, get_logs, list_containers, update_sqlite

# Get command status
curl -s $BASE/api/v1/commands/$CMD_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Qube polls for pending commands
curl -s -X POST $TPBASE/v1/commands/poll \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq .

# Qube acks a command
curl -s -X POST $TPBASE/v1/commands/$CMD_ID/ack \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"result":"pong","success":true}' | jq .
```

---

## 12. Telemetry ingest and query

```bash
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Ingest readings (enterprise-influx-to-sql calls this)
curl -s -X POST $TPBASE/v1/telemetry/ingest \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"readings\":[
    {\"time\":\"$NOW\",\"sensor_id\":\"$SENSOR_ID\",\"field_key\":\"active_power_w\",\"value\":1250.5,\"unit\":\"W\",\"tags\":{\"location\":\"MDB\"}},
    {\"time\":\"$NOW\",\"sensor_id\":\"$SENSOR_ID\",\"field_key\":\"voltage_v\",\"value\":231.2,\"unit\":\"V\"},
    {\"time\":\"$NOW\",\"sensor_id\":\"$SENSOR_ID\",\"field_key\":\"current_a\",\"value\":5.4,\"unit\":\"A\"}
  ]}" | jq .
# {"inserted":3,"failed":0,"total":3}

# Query latest readings
curl -s "$BASE/api/v1/data/sensors/$SENSOR_ID/latest" \
  -H "Authorization: Bearer $TOKEN" | jq .

# Query time-range readings
curl -s "$BASE/api/v1/data/readings?sensor_id=$SENSOR_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '{count, readings_sample: .readings[:2]}'

# Filter by field
curl -s "$BASE/api/v1/data/readings?sensor_id=$SENSOR_ID&field=active_power_w" \
  -H "Authorization: Bearer $TOKEN" | jq '.readings[0]'
```

---

## 13. User management

```bash
# Invite user to your org
curl -s -X POST $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"operator@acme.com","password":"pass123","role":"editor"}' | jq .
USER_ID=<paste user_id>

# List users
curl -s $BASE/api/v1/users \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {user_id,email,role}]'

# Update role
curl -s -X PATCH $BASE/api/v1/users/$USER_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"viewer"}' | jq .

# Remove user
curl -s -X DELETE $BASE/api/v1/users/$USER_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# My profile
curl -s $BASE/api/v1/users/me \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## 14. Registry settings (superadmin)

```bash
# View
curl -s $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" | jq .

# Switch to GitHub registry
curl -s -X PUT $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}' | jq .

# Switch to GitLab (production)
curl -s -X PUT $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"gitlab","gitlab_base":"registry.gitlab.com/iot-team4/product"}' | jq .
```

---

## 15. Simulate full data pipeline (dev)

```bash
# 1. Seed InfluxDB with fake gateway data
docker compose -f docker-compose.dev.yml run --rm influx-seeder

# 2. Verify InfluxDB has data
curl -s 'http://localhost:8086/query?q=SHOW+MEASUREMENTS&db=edgex' | jq .

# 3. Check enterprise-influx-to-sql logs — should see "forwarded X readings"
docker compose -f docker-compose.dev.yml logs enterprise-influx-to-sql

# 4. Query cloud API for readings
curl -s "$BASE/api/v1/data/sensors/$SENSOR_ID/latest" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## 16. Delete operations

```bash
# Delete sensor (hash changes)
curl -s -X DELETE $BASE/api/v1/sensors/$SENSOR_ID \
  -H "Authorization: Bearer $TOKEN" | jq .
# {"deleted":true,"new_hash":"..."}

# Delete reader (cascades sensors + container)
curl -s -X DELETE $BASE/api/v1/readers/$READER_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Delete device template
curl -s -X DELETE $BASE/api/v1/device-templates/$DT_ID \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Common issues

**TP-API returns 401:** Token computed with wrong orgSecret. Re-claim the device or
call `/v1/device/register` again to get a fresh token.

**Sync config returns empty readers:** No readers added yet, or config_state has no entry
for this qube. Check: `SELECT * FROM config_state WHERE qube_id='Q-1001';`

**TimescaleDB: no data in query:** Check qubedata database created:
`psql -U qubeadmin qubedata -c "\dt"` — should show `sensor_readings`.

**Reader container not starting:** Check conf-agent logs for `docker stack deploy` output.
Reader needs `READER_ID` env var — set by conf-agent from containers table.

**WebSocket not connecting:** Port 8080 must be reachable from Qube. Check firewall.
conf-agent falls back to HTTP polling automatically after 30s.
