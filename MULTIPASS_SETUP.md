# Multipass Local Qube Setup — Qube Enterprise v2

Simulates a full Qube device locally using Multipass.
Cloud backend is the **Azure VM** (`20.197.15.165`) — already running.

One VM needed:
- `qube-device-vm` (2 CPU, 2 GB) — conf-agent binary + Docker Swarm + full edge stack

Registry: `ghcr.io/sandun-s/qube-enterprise-home`

## What gets deployed automatically

When conf-agent first syncs config from Azure, it runs `docker stack deploy` to bring up
the full Qube edge stack. **You do not deploy these manually** — conf-agent does it:

| Service | Image | Role |
|---------|-------|------|
| `influxdb` | `influxdb:1.8` | Edge data store |
| `influxdb-relay` | `ghcr.io/.../influxdb-relay:amd64.latest` | Write buffer (readers → relay → influxdb) |
| `core-switch` | `ghcr.io/.../core-switch:amd64.latest` | Routes reader data to influxdb-relay |
| `enterprise-influx-to-sql` | `ghcr.io/.../enterprise-influx-to-sql:amd64.latest` | Reads influxdb → TP-API → TimescaleDB |
| `<reader containers>` | e.g. `snmp-reader:amd64.latest` | Auto-deployed when you add readers in portal |

Data flow: `reader → core-switch:8585 → influxdb-relay:9096 → influxdb:8086`
Telemetry: `enterprise-influx-to-sql reads influxdb → POST /v1/telemetry/ingest → Azure TimescaleDB`

---

## Prerequisites

```bash
# On your desktop (WSL/macOS/Linux)
multipass version   # must be installed — https://multipass.run
curl --version
jq --version
```

---

## Step 1 — Launch the Qube VM

```bash
multipass launch --name qube-device-vm --cpus 2 --memory 2G --disk 12G 22.04

QUBE_IP=$(multipass info qube-device-vm | grep IPv4 | awk '{print $2}')
echo "Qube VM IP: $QUBE_IP"
```

---

## Step 2 — Copy device identity file

```bash
multipass transfer test/mit.txt qube-device-vm:/tmp/mit.txt
```

> `test/mit.txt` contains `Q-1001` / `TEST-Q1001-REG` — pre-seeded on Azure.

---

## Step 3 — Install Docker + extract conf-agent binary

> Run each block separately so you can see output and catch errors early.

### 3a — Install Docker + add ubuntu to docker group

```bash
multipass exec qube-device-vm -- sudo bash << 'EOF'
set -ex
curl -fsSL https://get.docker.com | sh
apt-get update -qq && apt-get install -y sqlite3
usermod -aG docker ubuntu
EOF
```

> Docker install takes 3–5 minutes. After this step, re-shell to pick up the docker group:

```bash
multipass shell qube-device-vm   # re-open shell so docker group is active
```

### 3b — Extract conf-agent binary + finish setup

> Multipass VMs are x86_64 — use the `amd64.latest` tag.

```bash
multipass exec qube-device-vm -- bash << 'EOF'
set -ex

# Pull and extract binary
docker pull ghcr.io/sandun-s/qube-enterprise-home/conf-agent:amd64.latest
CONTAINER=$(docker create ghcr.io/sandun-s/qube-enterprise-home/conf-agent:amd64.latest)
sudo docker cp $CONTAINER:/app/conf-agent /usr/local/bin/enterprise-conf-agent
docker rm $CONTAINER
sudo chmod +x /usr/local/bin/enterprise-conf-agent
echo "Binary: $(ls -lh /usr/local/bin/enterprise-conf-agent)"

# Device identity
sudo cp /tmp/mit.txt /boot/mit.txt
echo "Identity: $(cat /boot/mit.txt)"

# Docker Swarm + overlay network
docker swarm init 2>/dev/null || true
docker network ls | grep -q qube-net || \
  docker network create --attachable --driver overlay qube-net

# SQLite + working directory on host (bind-mounted by all Docker services)
sudo mkdir -p /opt/qube/data
EOF
```

---

## Step 4 — Set registry to amd64 on Azure (BEFORE starting conf-agent)

**Critical:** Do this before conf-agent starts so the first sync pulls `amd64` images.
If the registry is still on `arm64`, all Qube containers will fail to start on the x86_64 VM.

```bash
CLOUD="http://20.197.15.165:8080"

SA_TOKEN=$(curl -s -X POST $CLOUD/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' | jq -r .token)

echo $SA_TOKEN   # must not be empty

curl -s -X PUT $CLOUD/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"amd64"}' | jq .

# Verify
curl -s -H "Authorization: Bearer $SA_TOKEN" \
  $CLOUD/api/v1/admin/registry | jq '{mode,arch,github_base}'
# Expected: {"mode":"github","arch":"amd64","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}
```

> When switching to real Raspberry Pi Qubes: set `"arch":"arm64"` to flip back globally.

---

## Step 5 — Configure and start conf-agent

```bash
multipass exec qube-device-vm -- sudo bash << 'EOF'
set -e

# Env file pointing to Azure cloud
cat > /opt/qube/.env << ENV
CLOUD_WS_URL=ws://20.197.15.165:8080/ws
TPAPI_URL=http://20.197.15.165:8081
SQLITE_PATH=/opt/qube/data/qube.db
WORK_DIR=/opt/qube
POLL_INTERVAL=15
ENV

# Systemd service
cat > /etc/systemd/system/enterprise-conf-agent.service << SVC
[Unit]
Description=Qube Enterprise Conf-Agent v2
After=network-online.target docker.service
Requires=docker.service
[Service]
Type=simple
User=root
WorkingDirectory=/opt/qube
EnvironmentFile=/opt/qube/.env
ExecStart=/usr/local/bin/enterprise-conf-agent
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=enterprise-conf-agent
[Install]
WantedBy=multi-user.target
SVC

systemctl daemon-reload
systemctl enable enterprise-conf-agent.service
systemctl start enterprise-conf-agent.service
sleep 2
systemctl is-active enterprise-conf-agent.service && echo "conf-agent running" || echo "FAILED"
EOF
```

conf-agent will now:
1. Read `/boot/mit.txt` → `POST /v1/device/register` → polls until device is claimed
2. Save `QUBE_TOKEN` to `/opt/qube/.env`

---

## Step 6 — Claim Q-1001 on Azure

conf-agent is running and polling Azure. Claim it so it can proceed:

```bash
CLOUD="http://20.197.15.165:8080"

TOKEN=$(curl -s -X POST $CLOUD/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Local Test","email":"local@test.local","password":"testpass123"}' \
  | jq -r .token)

echo $TOKEN   # must not be empty

# Claim Q-1001
curl -s -X POST $CLOUD/api/v1/qubes/claim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' | jq .
```

Once claimed, conf-agent immediately gets its `QUBE_TOKEN` and proceeds to:
1. Sync config → receive `docker-compose.yml` from Azure
2. `docker stack deploy -c docker-compose.yml qube` — deploys the full edge stack

Watch it happen:

```bash
multipass exec qube-device-vm -- journalctl -u enterprise-conf-agent -f
# Look for:
#   [register] Device claimed! Token received.
#   [agent] Config changed (new hash) — applying update
#   [docker] stack deploy OK
```

---

## Step 7 — Verify full stack is running

```bash
# All Swarm services should be 1/1
multipass exec qube-device-vm -- docker service ls
# Expected:
# qube_influxdb                  replicated   1/1   influxdb:1.8
# qube_influxdb-relay            replicated   1/1   ghcr.io/.../influxdb-relay:amd64.latest
# qube_core-switch               replicated   1/1   ghcr.io/.../core-switch:amd64.latest
# qube_enterprise-influx-to-sql  replicated   1/1   ghcr.io/.../enterprise-influx-to-sql:amd64.latest

# SQLite populated
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db '.tables'

# Qube shows online on Azure
curl -s -H "Authorization: Bearer $TOKEN" \
  $CLOUD/api/v1/qubes/Q-1001 | jq .status
# "online"
```

---

## Step 8 — Add a reader and test full data pipeline

### Add a reader via portal (or curl)

```bash
READER_ID=$(curl -s -X POST $CLOUD/api/v1/qubes/Q-1001/readers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test SNMP Reader",
    "protocol": "snmp",
    "template_id": "'$(curl -s $CLOUD/api/v1/reader-templates -H "Authorization: Bearer $TOKEN" | jq -r '.[] | select(.protocol=="snmp") | .id')'",
    "config_json": {"fetch_interval_sec": 30, "timeout_sec": 10}
  }' | jq -r .reader_id)
echo "Reader: $READER_ID"
```

conf-agent picks up the new config → deploys snmp-reader container automatically:

```bash
multipass exec qube-device-vm -- docker service ls
# qube_snmp-reader   replicated   1/1   ghcr.io/.../snmp-reader:amd64.latest

multipass exec qube-device-vm -- docker service logs qube_snmp-reader
```

### Verify telemetry pipeline

```bash
# Check InfluxDB has data (if reader has reachable devices)
multipass exec qube-device-vm -- \
  curl -s 'http://localhost:8086/query?q=SHOW+MEASUREMENTS&db=edgex' | python3 -m json.tool

# Check enterprise-influx-to-sql is forwarding
multipass exec qube-device-vm -- docker service logs qube_enterprise-influx-to-sql
# Look for: "forwarded X readings"

# Query from Azure cloud
SENSOR_ID=<paste sensor id>
curl -s "$CLOUD/api/v1/data/sensors/$SENSOR_ID/latest" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Useful commands

```bash
# Watch conf-agent logs live
multipass exec qube-device-vm -- journalctl -u enterprise-conf-agent -f

# List all Swarm services + health
multipass exec qube-device-vm -- docker service ls

# Service logs
multipass exec qube-device-vm -- docker service logs qube_core-switch
multipass exec qube-device-vm -- docker service logs qube_influxdb-relay
multipass exec qube-device-vm -- docker service logs qube_enterprise-influx-to-sql
multipass exec qube-device-vm -- docker service logs qube_snmp-reader

# Inspect SQLite (populated after first config sync)
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db '.tables'
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db 'SELECT id,name,protocol FROM readers;'
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db 'SELECT id,name FROM sensors;'

# Check QUBE_TOKEN (saved by conf-agent after registration)
multipass exec qube-device-vm -- cat /opt/qube/.env

# Shell into the VM
multipass shell qube-device-vm

# Restart conf-agent (e.g. after binary update)
multipass exec qube-device-vm -- sudo systemctl restart enterprise-conf-agent

# Update conf-agent binary to latest
multipass exec qube-device-vm -- bash << 'EOF'
set -ex
docker pull ghcr.io/sandun-s/qube-enterprise-home/conf-agent:amd64.latest
CONTAINER=$(docker create ghcr.io/sandun-s/qube-enterprise-home/conf-agent:amd64.latest)
sudo docker cp $CONTAINER:/app/conf-agent /usr/local/bin/enterprise-conf-agent
docker rm $CONTAINER
sudo systemctl restart enterprise-conf-agent
ls -lh /usr/local/bin/enterprise-conf-agent
EOF

# Force re-deploy full stack (delete local hash so conf-agent re-syncs)
multipass exec qube-device-vm -- sudo rm -f /opt/qube/.config_hash
multipass exec qube-device-vm -- sudo systemctl restart enterprise-conf-agent
```

---

## Cleanup

```bash
multipass stop   qube-device-vm
multipass delete qube-device-vm
multipass purge
```

---

## Summary

| Component | Where | Image / Binary |
|-----------|-------|----------------|
| Cloud API / TP-API / DB | Azure `20.197.15.165` | `cloud-api:amd64.latest` |
| conf-agent | Multipass VM (binary + systemd) | `conf-agent:amd64.latest` → binary extracted |
| influxdb | Multipass VM (Docker Swarm, auto-deployed) | `influxdb:1.8` |
| influxdb-relay | Multipass VM (Docker Swarm, auto-deployed) | `influxdb-relay:amd64.latest` |
| core-switch | Multipass VM (Docker Swarm, auto-deployed) | `core-switch:amd64.latest` |
| enterprise-influx-to-sql | Multipass VM (Docker Swarm, auto-deployed) | `enterprise-influx-to-sql:amd64.latest` |
| Reader containers | Multipass VM (Docker Swarm, auto-deployed on demand) | `snmp-reader`, `modbus-reader`, etc. `:amd64.latest` |

All images from: `ghcr.io/sandun-s/qube-enterprise-home/`

Test device: `Q-1001` / `TEST-Q1001-REG`

Data flow on Qube:
```
reader → core-switch:8585 → influxdb-relay:9096 → influxdb:8086
enterprise-influx-to-sql reads influxdb → POST /v1/telemetry/ingest → Azure TimescaleDB
```
