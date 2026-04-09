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

> **Note:** For most workflows prefer the [Smart Sensor Creation](#9a-smart-sensor-creation-recommended)
> endpoint which auto-finds or creates the reader. Use the reader endpoints below only when you
> need explicit control or want to pre-create a reader before adding sensors.

### Reader connection config — correct field names per protocol

| Protocol | Required reader fields | Optional reader fields |
|----------|------------------------|------------------------|
| `modbus_tcp` | `host`, `port` | `slave_id` (def 1), `poll_interval_sec` (def 10), `single_read_count` (def 100) |
| `snmp` | _(none — multi-target)_ | `poll_interval_sec` (def 30), `timeout_ms` (def 5000), `retries` (def 2) |
| `mqtt` | `broker_host`, `broker_port` | `username`, `password`, `client_id`, `qos` (def 1) |
| `opcua` | `endpoint` | `security_mode` (def None), `security_policy` (def None), `poll_interval_sec` (def 10) |
| `http` | _(none — multi-target)_ | `poll_interval_sec` (def 30), `timeout_ms` (def 10000) |

```bash
# Modbus TCP reader — one per device/gateway endpoint
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Main PLC Reader\",
    \"protocol\": \"modbus_tcp\",
    \"template_id\": \"$MODBUS_RT_ID\",
    \"config_json\": {
      \"host\":\"192.168.10.1\",
      \"port\":502,
      \"slave_id\":1,
      \"poll_interval_sec\":10,
      \"single_read_count\":100
    }
  }" | jq .
READER_ID=<paste reader_id>

# SNMP reader — one shared container per Qube handles all SNMP devices
SNMP_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="snmp") | .id')

curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"SNMP Reader\",
    \"protocol\": \"snmp\",
    \"template_id\": \"$SNMP_RT_ID\",
    \"config_json\": {\"poll_interval_sec\":30,\"timeout_ms\":5000,\"retries\":2}
  }" | jq .
SNMP_READER_ID=<paste reader_id>

# MQTT reader — one per broker; broker_host + broker_port (not broker_url)
MQTT_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="mqtt") | .id')

curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"MQTT Broker Floor 2\",
    \"protocol\": \"mqtt\",
    \"template_id\": \"$MQTT_RT_ID\",
    \"config_json\": {
      \"broker_host\":\"192.168.1.10\",
      \"broker_port\":1883,
      \"client_id\":\"qube-floor2\",
      \"qos\":1
    }
  }" | jq .
MQTT_READER_ID=<paste reader_id>

# OPC-UA reader — one per server endpoint
OPCUA_RT_ID=$(curl -s $BASE/api/v1/reader-templates \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '.[] | select(.protocol=="opcua") | .id')

curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"SCADA OPC-UA Server\",
    \"protocol\": \"opcua\",
    \"template_id\": \"$OPCUA_RT_ID\",
    \"config_json\": {
      \"endpoint\":\"opc.tcp://192.168.1.20:4840\",
      \"security_mode\":\"None\",
      \"security_policy\":\"None\",
      \"poll_interval_sec\":10
    }
  }" | jq .
OPCUA_READER_ID=<paste reader_id>

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

### Sensor per-device config — correct field names per protocol

| Protocol | Per-device params in `sensor_params_schema` |
|----------|---------------------------------------------|
| `modbus_tcp` | `unit_id` (Slave ID), `register_offset` (optional) |
| `snmp` | `host` (device IP), `port` (def 161), `community` (def public), `snmp_version` (def 2c) |
| `mqtt` | _(no per-device params — `topic` goes in each measurement entry)_ |
| `opcua` | `namespace_index` (optional) |
| `http` | `url` (required), `method`, `auth_type`, `username`, `password`, `bearer_token` |

> **MQTT note:** The MQTT reader reads `topic` from inside each `json_paths` entry.
> If using a device template with a top-level `topic` param (e.g. CCS panels), the reader
> uses that as a fallback for all entries missing their own topic.

```bash
# Modbus sensor using device template
curl -s -X POST $BASE/api/v1/readers/$READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Main Energy Meter\",
    \"template_id\": \"$DT_ID\",
    \"params\": {\"unit_id\":1,\"register_offset\":0},
    \"tags_json\": {\"location\":\"MDB\",\"phase\":\"3P\"},
    \"output\": \"influxdb\",
    \"table_name\": \"Measurements\"
  }" | jq .
SENSOR_ID=<paste sensor_id>

# SNMP sensor — note: host not ip_address
curl -s -X POST $BASE/api/v1/readers/$SNMP_READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Main UPS",
    "params": {
      "host": "192.168.1.100",
      "port": 161,
      "community": "public",
      "snmp_version": "2c",
      "oids": [
        {"oid":".1.3.6.1.4.1.318.1.1.1.2.2.1.0","field_key":"battery_pct","scale":1.0},
        {"oid":".1.3.6.1.4.1.318.1.1.1.2.2.3.0","field_key":"battery_v","scale":0.1}
      ]
    },
    "output": "influxdb"
  }' | jq .

# MQTT sensor — topics go in each json_paths entry, or as a top-level fallback
curl -s -X POST $BASE/api/v1/readers/$MQTT_READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Temperature Sensor Floor 2",
    "params": {
      "json_paths": [
        {"topic":"sensors/floor2/temp","json_path":"$.temperature","field_key":"temperature_c","unit":"C"},
        {"topic":"sensors/floor2/temp","json_path":"$.humidity","field_key":"humidity_pct","unit":"%"}
      ]
    },
    "output": "influxdb,live"
  }' | jq .

# MQTT sensor using CCS template (top-level topic pattern)
curl -s -X POST $BASE/api/v1/readers/$MQTT_READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"CCS PowerRoom PM1\",
    \"template_id\": \"$CCS_DT_ID\",
    \"params\": {\"topic\":\"ccs_data\",\"qos\":0,\"panel_index\":0},
    \"output\": \"influxdb\"
  }" | jq .

# List sensors for a reader
curl -s $BASE/api/v1/readers/$READER_ID/sensors \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,output}]'

# List all sensors across all readers for a qube
curl -s $BASE/api/v1/qubes/$QUBE_ID/sensors \
  -H "Authorization: Bearer $TOKEN" | jq '[.[] | {id,name,reader_id,protocol}]'

# Update sensor tags or output mode
curl -s -X PUT $BASE/api/v1/sensors/$SENSOR_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tags_json":{"location":"MDB","phase":"3P","floor":"1"},"output":"influxdb,live"}' | jq .
# Returns: {"updated":true,"new_hash":"..."}
```

---

## 9a. Smart sensor creation (recommended)

**`POST /api/v1/qubes/:id/sensors`** — automatically finds or creates the right reader container,
then creates the sensor. Preferred over manually managing readers.

### How it works

| Protocol (`reader_standard`) | Reader selection |
|------------------------------|-----------------|
| `snmp`, `http` (`multi_target`) | Uses the single existing reader for this protocol on the Qube. Creates one automatically if none exists. |
| `modbus_tcp`, `mqtt`, `opcua` (`endpoint`) | Computes an endpoint fingerprint. Reuses matching reader. Creates a new reader + container if no match. |

### Modbus — auto reader (same endpoint reused)

```bash
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Energy Meter Rack A\",
    \"template_id\": \"$DT_ID\",
    \"params\": {\"unit_id\":3},
    \"reader_config\": {\"host\":\"192.168.10.1\",\"port\":502,\"slave_id\":1,\"poll_interval_sec\":10},
    \"reader_name\": \"Main PLC Reader\",
    \"output\": \"influxdb\",
    \"table_name\": \"Measurements\"
  }" | jq .
# Returns: {"sensor_id":"...","reader_id":"...","new_hash":"..."}
# If 192.168.10.1:502 reader already exists, it's reused — no new container deployed.
```

### SNMP — auto reader (always shared, no connection config needed)

```bash
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "APC UPS Room 3",
    "template_id": "<snmp-ups-template-id>",
    "params": {
      "host": "192.168.1.101",
      "community": "public",
      "snmp_version": "2c"
    },
    "reader_config": {},
    "output": "influxdb"
  }' | jq .
# Finds existing SNMP reader on this Qube, or creates one. No connection form needed.
```

### MQTT — auto reader (matched by broker_host:broker_port)

```bash
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/sensors \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Floor 3 Temp Sensor",
    "template_id": "<mqtt-template-id>",
    "params": {"topic": "sensors/floor3/env"},
    "reader_config": {"broker_host":"192.168.1.10","broker_port":1883,"qos":1},
    "reader_name": "Main MQTT Broker",
    "output": "influxdb,live"
  }' | jq .
# If a reader already exists for 192.168.1.10:1883, it's reused.
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

Commands are dispatched via WebSocket if the Qube is connected, otherwise queued for HTTP polling.
The conf-agent runs the corresponding script or action when it receives the command.

```bash
# Send a command (cloud → Qube) — returns 202 immediately
curl -s -X POST $BASE/api/v1/qubes/$QUBE_ID/commands \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command":"ping","payload":{"target":"8.8.8.8"}}' | jq .
# {"command_id":"<uuid>","status":"pending","delivery":"websocket|queued","poll_url":"..."}
CMD_ID=<paste command_id>

# Get command result / status
curl -s $BASE/api/v1/commands/$CMD_ID \
  -H "Authorization: Bearer $TOKEN" | jq .

# Qube polls for pending commands (conf-agent HTTP fallback)
curl -s -X POST $TPBASE/v1/commands/poll \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq .

# Qube acks a command after execution
curl -s -X POST $TPBASE/v1/commands/$CMD_ID/ack \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"executed","result":{"output":"..."}}' | jq .
```

### All valid commands and their payloads

#### Enterprise — containers & config

```bash
# ping — test connectivity to a host
curl ... -d '{"command":"ping","payload":{"target":"8.8.8.8"}}'

# restart_qube / reboot — reboot the device OS
curl ... -d '{"command":"restart_qube","payload":{}}'

# restart_reader — restart a specific reader container
curl ... -d '{"command":"restart_reader","payload":{"reader_id":"<reader-uuid>"}}'
# or by service name:
curl ... -d '{"command":"restart_reader","payload":{"service":"modbus-reader"}}'

# stop_container — stop a named Docker service
curl ... -d '{"command":"stop_container","payload":{"service":"qube_modbus-reader"}}'

# reload_config / update_sqlite — clear local hash, force resync on next cycle
curl ... -d '{"command":"reload_config","payload":{}}'

# get_logs — get container logs
curl ... -d '{"command":"get_logs","payload":{"service":"modbus-reader","lines":200}}'
# omit service to get all compose logs

# list_containers — list running Docker containers
curl ... -d '{"command":"list_containers","payload":{}}'
```

#### Device management — network

```bash
# reset_ips — reset all network interfaces to DHCP, reconnect to qube-net WiFi
curl ... -d '{"command":"reset_ips","payload":{}}'

# set_eth — configure ethernet interface
# DHCP:
curl ... -d '{"command":"set_eth","payload":{"interface":"eth0","mode":"auto"}}'
# Static:
curl ... -d '{"command":"set_eth","payload":{"interface":"eth0","mode":"static","address":"192.168.1.10/24","gateway":"192.168.1.1","dns":"8.8.8.8"}}'

# set_wifi — configure WiFi interface
# DHCP:
curl ... -d '{"command":"set_wifi","payload":{"interface":"wlan0","mode":"auto","ssid":"MyWifi","password":"secret","key_mgmt":"psk"}}'
# Static:
curl ... -d '{"command":"set_wifi","payload":{"interface":"wlan0","mode":"static","address":"192.168.1.20/24","gateway":"192.168.1.1","dns":"8.8.8.8","ssid":"MyWifi","password":"secret","key_mgmt":"psk"}}'
# key_mgmt options: psk, sae, eap, none

# set_firewall — configure iptables rules
# Rules: comma-separated <proto>:<net-or-0>:<port-or-0>
curl ... -d '{"command":"set_firewall","payload":{"rules":"tcp:10.0.0.0/8:1883,tcp:122.255.48.0/24:0,tcp:0:8080"}}'
```

#### Device management — identity & system

```bash
# shutdown — safe device shutdown
curl ... -d '{"command":"shutdown","payload":{}}'

# get_info — get network info (IPs, MACs, SSID, open ports)
curl ... -d '{"command":"get_info","payload":{}}'
# result: {"eth_mac":"aa:bb:...","eth_ipv4":"192.168.1.10/24","wlan_ssid":"MyWifi",...}

# set_name — set device hostname (updates avahi + mit.txt)
curl ... -d '{"command":"set_name","payload":{"name":"qube-factory-a"}}'

# set_timezone — set device timezone
curl ... -d '{"command":"set_timezone","payload":{"timezone":"Asia/Colombo"}}'
# use: timedatectl list-timezones for valid values
```

#### Data backup / restore

```bash
# backup_data — rsync /data to a CIFS or NFS share
curl ... -d '{"command":"backup_data","payload":{"type":"cifs","path":"\\\\192.168.1.1\\backup","user":"admin","pass":"secret"}}'
curl ... -d '{"command":"backup_data","payload":{"type":"nfs","path":"192.168.1.1:/nfs-backup"}}'

# restore_data — restore /data from a CIFS or NFS share
curl ... -d '{"command":"restore_data","payload":{"type":"cifs","path":"\\\\192.168.1.1\\backup","user":"admin","pass":"secret"}}'
```

#### Maintenance mode operations (device reboots, performs op, reboots back)

```bash
# repair_fs — e2fsck filesystem repair (device goes offline temporarily)
curl ... -d '{"command":"repair_fs","payload":{}}'

# backup_image — dd block-level image backup to backup partition
curl ... -d '{"command":"backup_image","payload":{}}'

# restore_image — dd block-level image restore from backup partition
curl ... -d '{"command":"restore_image","payload":{}}'
```

#### Service management (v1 legacy Docker services)

```bash
# service_add — install a Docker service from a pre-staged package
curl ... -d '{"command":"service_add","payload":{"name":"myservice","type":"modbus","version":"1.2.3","ports":"8090"}}'

# service_rm — remove a Docker service
curl ... -d '{"command":"service_rm","payload":{"name":"myservice"}}'

# service_edit — update service config files or ports
curl ... -d '{"command":"service_edit","payload":{"name":"myservice","ports":"8090,8091"}}'
```

#### File transfer

```bash
# put_file — push a file to the device at /mit<path>
FILE_B64=$(base64 -w0 myfile.cfg)
curl ... -d "{\"command\":\"put_file\",\"payload\":{\"path\":\"/config/myfile.cfg\",\"data\":\"$FILE_B64\"}}"

# get_file — pull a file from the device at /mit<path>
curl ... -d '{"command":"get_file","payload":{"path":"/config/myfile.cfg"}}'
# result: {"path":"/mit/config/myfile.cfg","data":"<base64>","size":1234}
```

#### Device discovery (new — requires conf-agent handler)

```bash
# mqtt_discover — subscribe to broker for N seconds, returns received field mappings
# Qube subscribes to broker_host:broker_port on topic filter for duration_sec,
# then acks with all unique topic+JSON-path pairs found.
curl ... -d '{
  "command": "mqtt_discover",
  "payload": {
    "broker_host": "192.168.1.10",
    "broker_port": 1883,
    "topic": "#",
    "username": "",
    "password": "",
    "duration_sec": 30
  }
}'
# ack result: {"messages":[{"topic":"...","payload":"{...}"},...]}

# snmp_walk — run an SNMP walk on a device, returns all OID→value pairs
curl ... -d '{
  "command": "snmp_walk",
  "payload": {
    "host": "192.168.1.20",
    "port": 161,
    "community": "public",
    "version": "2c",
    "root_oid": ".1.3.6.1"
  }
}'
# ack result: {"oids":[{"oid":".1.3.6.1.2.1...","type":"INTEGER","value":"100"},...]}
```

> **Note:** `mqtt_discover` and `snmp_walk` are registered command types in the cloud API.
> The conf-agent ExecCommand handlers for these are not yet implemented — results come via
> the standard ack mechanism once implemented.

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

# Switch to GitHub registry — arm64 (real Qube: Raspberry Pi / Kadas)
curl -s -X PUT $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"arm64"}' | jq .

# Switch to GitHub registry — amd64 (Multipass x86_64 dev VMs)
curl -s -X PUT $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"amd64"}' | jq .

# Switch to GitLab (production — arm64 by default)
curl -s -X PUT $BASE/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"gitlab","gitlab_base":"registry.gitlab.com/iot-team4/product","arch":"arm64"}' | jq .
```

> `arch` controls the image tag suffix sent to all Qubes (`amd64.latest` vs `arm64.latest`).
> Default is `arm64`. Switch any time — takes effect on next conf-agent sync.

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

**MQTT reader produces no data:** Two common causes:
1. Reader config uses `broker_url` key — updated readers expect `broker_host`+`broker_port`.
   Fix: update reader config_json to use the split fields.
2. Sensor's `json_paths` entries have no `topic` field AND no top-level `topic` in the sensor
   config — reader skips all entries. Fix: ensure `topic` is either in each json_paths entry
   OR set as a top-level key via `params: {topic: "..."}` when adding the sensor.

**SNMP devices not polled:** Reader skips sensors where `host` is empty. Common if old sensors
used `ip_address` instead of `host`. Fix: update sensors via PUT with correct `host` field.
The reader now accepts both, but the canonical key is `host`.

**Smart sensor creation fails with "reader not found":** The `POST /api/v1/qubes/:id/sensors`
endpoint requires `template_id` in the body. Missing this returns 400.

**Modbus reader ignores slave_id set in sensor params:** `slave_id` is a reader-level setting
(one reader = one device). It is read from `readers.config_json`, not from individual sensors.
To poll multiple Modbus slaves, create multiple readers (one per slave_id).
