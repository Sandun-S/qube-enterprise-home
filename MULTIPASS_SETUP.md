# Multipass Local Qube Setup — Qube Enterprise v2

Simulates a Qube device locally using Multipass.
Cloud backend is the **Azure VM** (`20.197.15.165`) — already running.

One VM needed:
- `qube-device-vm` (1 CPU, 1 GB) — conf-agent binary + Docker Swarm

Registry: `ghcr.io/sandun-s/qube-enterprise-home`

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
multipass launch --name qube-device-vm --cpus 1 --memory 1G --disk 8G 22.04

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

Pull the conf-agent image from GHCR and extract the binary. No Go needed.

> **Note:** Run each block separately so you can see output and catch errors early.

### 3a — Install Docker + add ubuntu to docker group

```bash
multipass exec qube-device-vm -- sudo bash << 'EOF'
set -ex
curl -fsSL https://get.docker.com | sh
apt-get update -qq && apt-get install -y sqlite3
usermod -aG docker ubuntu
EOF
```

> Docker install takes 3–5 minutes and looks frozen — it isn't. `-ex` prints each command so you can track progress.
> After this, `ubuntu` user can run Docker without `sudo`. Re-shell to pick up the group:

```bash
multipass shell qube-device-vm   # re-open shell so docker group is active
```

### 3b — Extract conf-agent binary + finish setup

> The CI workflow builds conf-agent for both `amd64` (Multipass/x86) and `arm64` (real Qube).
> Multipass VMs are x86_64 so use the `amd64.latest` tag.

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

# SQLite data dir
sudo mkdir -p /opt/qube/data
EOF
```

---

## Step 4 — Configure and start conf-agent

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

---

## Step 5 — Claim Q-1001 on Azure

conf-agent is now running and polling Azure. Claim the device so it can receive config.

```bash
CLOUD="http://20.197.15.165:8080"

# Get a token (register org or use existing)
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

Wait ~30 s for conf-agent to pick up the claim via WebSocket:

```bash
# Should return "online"
curl -s -H "Authorization: Bearer $TOKEN" \
  $CLOUD/api/v1/qubes/Q-1001 | jq .status
```

---

## Step 6 — Configure registry: GitHub + amd64 arch

Multipass VMs are x86_64 so the cloud must send `amd64.latest` image tags to the Qube.
Set both the registry source and the target architecture:

```bash
SA_TOKEN=$(curl -s -X POST http://20.197.15.165:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' | jq -r .token)

# Set GitHub registry + amd64 arch (required for Multipass x86_64 VMs)
curl -s -X PUT http://20.197.15.165:8080/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"amd64"}' | jq .
```

Verify:

```bash
curl -s -H "Authorization: Bearer $SA_TOKEN" \
  http://20.197.15.165:8080/api/v1/admin/registry | jq '{mode,arch,github_base}'
# Expected: {"mode":"github","arch":"amd64","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}
```

Now when you add a reader via the portal, conf-agent pulls e.g.:
`ghcr.io/sandun-s/qube-enterprise-home/snmp-reader:amd64.latest`

> **When switching to real Raspberry Pi Qubes:** set `"arch":"arm64"` to flip back globally.

---

## Useful commands

```bash
# Watch conf-agent logs live
multipass exec qube-device-vm -- journalctl -u enterprise-conf-agent -f

# Inspect SQLite (populated after first config sync)
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db '.tables'
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db 'SELECT id,name,protocol FROM readers;'
multipass exec qube-device-vm -- sqlite3 /opt/qube/data/qube.db 'SELECT id,name FROM sensors;'

# List reader containers deployed by conf-agent
multipass exec qube-device-vm -- docker service ls

# Reader container logs
multipass exec qube-device-vm -- docker service logs qube_snmp-reader

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

| Component | Where | Image |
|---|---|---|
| Cloud API / TP-API / DB | Azure `20.197.15.165` | `cloud-api:amd64.latest` |
| conf-agent | Multipass VM (binary/systemd) | `conf-agent:amd64.latest` → binary extracted |
| Reader containers | Multipass VM (Docker Swarm) | `snmp-reader`, `modbus-reader`, etc. `:amd64.latest` |

All images from: `ghcr.io/sandun-s/qube-enterprise-home/`

Test device: `Q-1001` / `TEST-Q1001-REG`
