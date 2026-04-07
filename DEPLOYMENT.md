# Qube Enterprise v2 — Deployment Guide

## Prerequisites

- Cloud VM: Ubuntu 22.04+, 2 vCPU, 4 GB RAM, 40 GB disk
- Docker Engine + Docker Compose v2
- Qube device: Raspberry Pi 4 or Kadas, Docker Swarm initialized

---

## Cloud VM setup

### 1. Install Docker
```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER && newgrp docker
```

### 2. Deploy the stack

```bash
git clone <repo-url> qube-enterprise
cd qube-enterprise

# Set secrets
export JWT_SECRET=$(openssl rand -hex 32)
export DB_PASS=qubepass   # change in production

# Start all services
docker compose -f docker-compose.dev.yml up -d --build
```

For production, use the production compose file (see `scripts/setup-cloud.sh`).

### 3. Environment variables — cloud-api

```bash
DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedb?sslmode=disable
TELEMETRY_DATABASE_URL=postgres://qubeadmin:qubepass@localhost:5432/qubedata?sslmode=disable
JWT_SECRET=<strong random secret — minimum 32 chars>

# Container registry — pick one:
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home          # GitHub
QUBE_IMAGE_REGISTRY=registry.gitlab.com/iot-team4/product           # GitLab
```

### 4. Firewall

```bash
ufw allow 8080/tcp    # Cloud API + WebSocket (frontend + Qubes)
ufw allow 8081/tcp    # TP-API (Qubes only — restrict source IPs if possible)
# Do NOT expose 5432 (Postgres)
```

### 5. Superadmin account

Pre-seeded in `cloud/migrations/003_test_seeds.sql`:
- Email: `iotteam@internal.local`
- Password: `iotteam2024` (change in production)

To change: `POST /api/v1/auth/login` then update via DB:
```sql
UPDATE users SET password_hash = crypt('newpassword', gen_salt('bf',12))
WHERE email = 'iotteam@internal.local';
```

---

## Flash-time Qube provisioning

`write-to-database.sh` inserts the device into `qubedb` at flash time:

```bash
# On flash machine — set once:
export ENTERPRISE_DB_HOST=cloud-vm-ip:5432
export ENTERPRISE_DB_USER=qubeadmin
export ENTERPRISE_DB_PASS=qubepass
export ENTERPRISE_DB_NAME=qubedb

# Called per device:
./scripts/write-to-database.sh <device_id> <register_key> <maintain_key>
# Example:
./scripts/write-to-database.sh Q-1001 R4KY-ZTQ5-XXXX KC3L-T7XT-YYYY
```

This inserts a row in `qubes` so the device is ready to be claimed. `/boot/mit.txt` is written to the device separately during the flash process by `local.sh`.

---

## Qube device setup

### 1. Install Docker + Swarm

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER && newgrp docker
docker swarm init
docker network create --attachable --driver overlay qube-net
sudo mkdir -p /opt/qube/data
```

### 2. Deploy conf-agent (runs as systemd binary, not a Docker service)

Extract the binary from the image and run it as a systemd service:

```bash
# Extract binary
docker pull ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest
CONTAINER=$(docker create ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest)
sudo docker cp $CONTAINER:/app/conf-agent /usr/local/bin/enterprise-conf-agent
docker rm $CONTAINER
sudo chmod +x /usr/local/bin/enterprise-conf-agent

# Env file
sudo mkdir -p /opt/qube
cat > /opt/qube/.env << EOF
CLOUD_WS_URL=ws://<cloud-ip>:8080/ws
TPAPI_URL=http://<cloud-ip>:8081
SQLITE_PATH=/opt/qube/data/qube.db
WORK_DIR=/opt/qube
POLL_INTERVAL=30
EOF

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
systemctl enable enterprise-conf-agent
systemctl start enterprise-conf-agent
```

conf-agent will:
1. Read `/boot/mit.txt` → `POST /v1/device/register` → polls until device is claimed
2. Once claimed: save `QUBE_TOKEN` to `/opt/qube/.env`, connect WebSocket
3. On first config sync: write SQLite, run `docker stack deploy` to bring up full edge stack

### 3. Full edge stack (auto-deployed by conf-agent)

conf-agent downloads a generated `docker-compose.yml` from the cloud and deploys it.
**You do not deploy these manually.** The stack includes:

| Service | Role |
|---------|------|
| `influxdb` | Edge data store |
| `influxdb-relay` | Write buffer (core-switch → relay → influxdb) |
| `core-switch` | Routes reader data to influxdb-relay |
| `enterprise-influx-to-sql` | Reads influxdb → TP-API → Azure TimescaleDB |
| Reader containers | Deployed automatically when you add readers in cloud portal |

All services bind-mount `/opt/qube/data` (host path) for shared SQLite access.

Data flow: `reader → core-switch:8585 → influxdb-relay:9096 → influxdb:8086`

Verify after claim + sync:
```bash
docker service ls   # all services should be 1/1
journalctl -u enterprise-conf-agent -f   # watch for "stack deploy OK"
```

---

## Reader containers

Reader containers are **not deployed manually**. They are automatically deployed by conf-agent
when the user adds a reader in the cloud portal.

Each reader container:
- Bind-mounts `/opt/qube/data` (read-only) for SQLite access
- Reads its config from SQLite on startup
- Sends data to `core-switch:8585` via HTTP

The container image is resolved from the reader template's `image_suffix` and the
`registry_settings` table. Update registry via:
```bash
# GitHub + arm64 (real Raspberry Pi / Kadas Qubes — default)
curl -X PUT http://<cloud-ip>:8080/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"arm64"}'

# GitHub + amd64 (Multipass x86_64 dev VMs)
curl -X PUT http://<cloud-ip>:8080/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home","arch":"amd64"}'
```

> `arch` defaults to `arm64` if not set. Switch it any time — takes effect on next conf-agent sync.

---

## Health checks

```bash
# Cloud services
curl http://<cloud-ip>:8080/health    # Cloud API + WS
curl http://<cloud-ip>:8081/health    # TP-API

# On Qube
docker service ls                      # all services running?
docker service logs qube_conf-agent   # conf-agent sync activity
sqlite3 /var/lib/docker/volumes/qube-data/_data/qube.db ".tables"  # SQLite populated?
```

---

## GitLab vs GitHub registry

Only `QUBE_IMAGE_REGISTRY` needs to change. No code changes required.

```bash
# GitHub (default in dev)
QUBE_IMAGE_REGISTRY=ghcr.io/sandun-s/qube-enterprise-home

# GitLab (production)
QUBE_IMAGE_REGISTRY=registry.gitlab.com/iot-team4/product
```

The cloud-api uses this to generate `docker-compose.yml` that conf-agent deploys on Qubes.
Update via the registry API endpoint to switch without redeployment:
```bash
curl -X PUT http://localhost:8080/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"gitlab","gitlab_base":"registry.gitlab.com/iot-team4/product"}'
```
