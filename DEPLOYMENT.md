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
```

### 2. Create SQLite volume

```bash
docker volume create qube-data
```

### 3. Deploy conf-agent

```bash
docker service create \
  --name qube_conf-agent \
  --restart-condition any \
  --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock \
  --mount type=volume,source=qube-data,target=/opt/qube/data \
  --mount type=bind,source=/boot/mit.txt,target=/boot/mit.txt,readonly \
  -e CLOUD_WS_URL=ws://<cloud-ip>:8080/ws \
  -e TPAPI_URL=http://<cloud-ip>:8081 \
  -e SQLITE_PATH=/opt/qube/data/qube.db \
  -e WORK_DIR=/opt/qube \
  -e POLL_INTERVAL=30 \
  ghcr.io/sandun-s/qube-enterprise-home/enterprise-conf-agent:arm64.latest
```

conf-agent will:
1. Read `/boot/mit.txt` → `POST /v1/device/register` → get QUBE_TOKEN
2. Connect WebSocket to cloud → begin receiving config updates
3. On first config: write SQLite, run `docker stack deploy`

### 4. Deploy enterprise-influx-to-sql

```bash
docker service create \
  --name qube_influx-to-sql \
  --restart-condition any \
  --mount type=volume,source=qube-data,target=/opt/qube/data,readonly \
  -e SQLITE_PATH=/opt/qube/data/qube.db \
  -e TPAPI_URL=http://<cloud-ip>:8081 \
  -e QUBE_ID=Q-1001 \
  -e QUBE_TOKEN=<token from conf-agent registration> \
  -e INFLUX_URL=http://127.0.0.1:8086 \
  -e INFLUX_DB=edgex \
  ghcr.io/sandun-s/qube-enterprise-home/enterprise-influx-to-sql:arm64.latest
```

QUBE_TOKEN is obtained by conf-agent during self-registration. Read it from conf-agent logs
or the database: `SELECT auth_token_hash FROM qubes WHERE id='Q-1001';`

---

## Reader containers

Reader containers are **not deployed manually**. They are automatically deployed by conf-agent
when the user adds a reader in the cloud portal.

Each reader container:
- Mounts the `qube-data` volume (read-only)
- Reads its config from SQLite on startup
- Sends data to `core-switch:8585` via HTTP

The container image is resolved from the reader template's `image_suffix` and the
`registry_settings` table. Update registry via:
```
PUT /api/v1/admin/registry  (superadmin)
{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}
```

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
