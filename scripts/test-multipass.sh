#!/usr/bin/env bash
# =============================================================================
# test-multipass.sh — Full end-to-end v2 test using two Multipass VMs
#
# Creates and provisions:
#   qube-cloud-vm  (2CPU, 3GB) — Postgres/TimescaleDB + Cloud API + InfluxDB
#   qube-device-vm (1CPU, 1GB) — conf-agent, Docker Swarm, SQLite
#
# Run from the repo root:
#   chmod +x scripts/test-multipass.sh
#   ./scripts/test-multipass.sh
#
# Prerequisites: multipass, curl, jq
# =============================================================================
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CLOUD_VM="qube-cloud-vm"
QUBE_VM="qube-device-vm"
QUBE_ID="Q-1001"
REG_KEY="TEST-Q1001-REG"

GREEN='\033[0;32m'; RED='\033[0;31m'; CYAN='\033[0;36m'; NC='\033[0m'
log()  { echo -e "${CYAN}[$(date +%H:%M:%S)]${NC} $1"; }
ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; }

echo "=============================================="
echo " Qube Enterprise v2 — Multipass E2E Test"
echo "=============================================="

command -v multipass &>/dev/null || { echo "ERROR: multipass not installed"; exit 1; }
command -v curl      &>/dev/null || { echo "ERROR: curl not installed"; exit 1; }
command -v jq        &>/dev/null || { echo "ERROR: jq not installed"; exit 1; }

# ── 1. Launch VMs ─────────────────────────────────────────────────────────────
log "[1/7] Launching VMs..."

multipass launch --name "$CLOUD_VM" --cpus 2 --memory 3G --disk 15G 22.04 2>/dev/null \
  || log "  $CLOUD_VM already exists"
multipass launch --name "$QUBE_VM"  --cpus 1 --memory 1G --disk 8G  22.04 2>/dev/null \
  || log "  $QUBE_VM already exists"

CLOUD_IP=$(multipass info "$CLOUD_VM" | grep "IPv4" | awk '{print $2}')
QUBE_IP=$(multipass  info "$QUBE_VM"  | grep "IPv4" | awk '{print $2}')
log "  cloud-vm IP: $CLOUD_IP"
log "  qube-vm  IP: $QUBE_IP"

# ── 2. Copy files to cloud VM ─────────────────────────────────────────────────
log "[2/7] Copying source to $CLOUD_VM..."
multipass transfer -r "$REPO_DIR/cloud" "$CLOUD_VM":/tmp/cloud-src
multipass transfer -r "$REPO_DIR/cloud/migrations" "$CLOUD_VM":/tmp/migrations
multipass transfer -r "$REPO_DIR/cloud/migrations-telemetry" "$CLOUD_VM":/tmp/migrations-telemetry

# ── 3. Setup cloud VM ─────────────────────────────────────────────────────────
log "[3/7] Provisioning $CLOUD_VM (Docker, TimescaleDB, Cloud API)..."

multipass exec "$CLOUD_VM" -- sudo bash << 'CLOUDSETUP'
set -e

# Install Docker
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh

# Build cloud-api from source
cd /tmp/cloud-src
sudo docker build -t qube-cloud-api:local .

# Copy migrations
sudo mkdir -p /opt/qube-enterprise/migrations
sudo mkdir -p /opt/qube-enterprise/migrations-telemetry
sudo cp /tmp/migrations/*.sql /opt/qube-enterprise/migrations/
sudo cp /tmp/migrations-telemetry/*.sql /opt/qube-enterprise/migrations-telemetry/

# Write docker-compose (TimescaleDB + InfluxDB + Cloud API)
cat > /opt/qube-enterprise/docker-compose.yml << 'COMPOSE'
version: "3.8"
networks:
  qube_net:
    driver: bridge
volumes:
  postgres_data:
  influxdb_data:
services:
  postgres:
    image: timescale/timescaledb:latest-pg16
    restart: unless-stopped
    networks: [qube_net]
    ports: ["5432:5432"]
    environment:
      POSTGRES_USER: qubeadmin
      POSTGRES_PASSWORD: qubepass
      POSTGRES_DB: qubedb
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - /opt/qube-enterprise/migrations/001_init.sql:/docker-entrypoint-initdb.d/001_init.sql:ro
      - /opt/qube-enterprise/migrations/002_global_data.sql:/docker-entrypoint-initdb.d/002_global_data.sql:ro
      - /opt/qube-enterprise/migrations/003_test_seeds.sql:/docker-entrypoint-initdb.d/003_test_seeds.sql:ro
      - /opt/qube-enterprise/migrations-telemetry/001_timescale_init.sql:/docker-entrypoint-initdb.d/010_timescale_init.sql:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U qubeadmin -d qubedb"]
      interval: 5s
      retries: 15
  influxdb:
    image: influxdb:1.8
    restart: unless-stopped
    networks: [qube_net]
    ports: ["8086:8086"]
    volumes: [influxdb_data:/var/lib/influxdb]
    environment:
      INFLUXDB_DB: edgex
      INFLUXDB_HTTP_AUTH_ENABLED: "false"
  cloud-api:
    image: qube-cloud-api:local
    restart: unless-stopped
    networks: [qube_net]
    ports: ["8080:8080", "8081:8081"]
    environment:
      DATABASE_URL: postgres://qubeadmin:qubepass@postgres:5432/qubedb?sslmode=disable
      TELEMETRY_DATABASE_URL: postgres://qubeadmin:qubepass@postgres:5432/qubedata?sslmode=disable
      JWT_SECRET: multipass-e2e-secret-change-me
      QUBE_IMAGE_REGISTRY: ghcr.io/sandun-s/qube-enterprise-home
    depends_on:
      postgres: {condition: service_healthy}
COMPOSE

cd /opt/qube-enterprise
sudo docker compose up -d

echo "Waiting for Cloud API..."
for i in $(seq 1 40); do
  curl -sf http://localhost:8080/health | grep -q '"ok"' && echo "API ready!" && exit 0
  sleep 3
done
echo "ERROR: Cloud API did not start"
exit 1
CLOUDSETUP

ok "cloud-vm ready — http://$CLOUD_IP:8080"

# ── 4. Setup qube VM ──────────────────────────────────────────────────────────
log "[4/7] Provisioning $QUBE_VM (Docker Swarm, conf-agent, SQLite)..."

multipass transfer -r "$REPO_DIR/conf-agent" "$QUBE_VM":/tmp/conf-agent-src
multipass transfer    "$REPO_DIR/test/mit.txt" "$QUBE_VM":/tmp/mit.txt

multipass exec "$QUBE_VM" -- sudo bash << QUBESETUP
set -e

# Install Docker + Go + SQLite
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh
apt-get update -qq
apt-get install -y sqlite3 golang-go 2>/dev/null || true

# Device identity from test mit.txt
cp /tmp/mit.txt /boot/mit.txt
echo "  Device identity:"
cat /boot/mit.txt

# Docker Swarm + overlay network
docker swarm init 2>/dev/null || true
docker network ls | grep -q qube-net || \
  docker network create --attachable --driver overlay qube-net

# SQLite data dir
mkdir -p /opt/qube/data

# Build conf-agent
cd /tmp/conf-agent-src
go mod tidy 2>/dev/null || true
CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/enterprise-conf-agent .
echo "  conf-agent binary built"

# Env file for v2
cat > /opt/qube/.env << ENV
CLOUD_WS_URL=ws://$CLOUD_IP:8080/ws
TPAPI_URL=http://$CLOUD_IP:8081
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
sleep 3
systemctl is-active enterprise-conf-agent.service && echo "  conf-agent active" || echo "  WARNING: conf-agent not running"
QUBESETUP

ok "qube-vm ready — conf-agent polling http://$CLOUD_IP:8081"

# ── 5. API flow ───────────────────────────────────────────────────────────────
log "[5/7] Running API flow..."
sleep 5

CLOUD_API="http://$CLOUD_IP:8080"
TPAPI="http://$CLOUD_IP:8081"

# Register org
TS=$(date +%s)
TOKEN=$(curl -s -X POST "$CLOUD_API/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d "{\"org_name\":\"E2E Org $TS\",\"email\":\"e2e$TS@test.local\",\"password\":\"e2epass123\"}" \
  | jq -r .token)
[ -n "$TOKEN" ] && ok "Org registered, JWT obtained" || { fail "Failed to register org"; exit 1; }

# Device register — pending before claim
STATUS=$(curl -s -X POST "$TPAPI/v1/device/register" \
  -H "Content-Type: application/json" \
  -d "{\"device_id\":\"$QUBE_ID\",\"register_key\":\"$REG_KEY\"}" | jq -r .status)
[ "$STATUS" = "pending" ] && ok "Self-register before claim: pending" \
  || fail "Expected pending, got: $STATUS"

# Claim the device
CLAIM=$(curl -s -X POST "$CLOUD_API/api/v1/qubes/claim" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"register_key\":\"$REG_KEY\"}")
QUBE_TOKEN=$(echo "$CLAIM" | jq -r .auth_token)
[ -n "$QUBE_TOKEN" ] && [ "$QUBE_TOKEN" != "null" ] \
  && ok "Device claimed, token: ${QUBE_TOKEN:0:16}..." \
  || { fail "Claim failed: $CLAIM"; exit 1; }

# Device register — claimed after claim
STATUS=$(curl -s -X POST "$TPAPI/v1/device/register" \
  -H "Content-Type: application/json" \
  -d "{\"device_id\":\"$QUBE_ID\",\"register_key\":\"$REG_KEY\"}" | jq -r .status)
[ "$STATUS" = "claimed" ] && ok "Self-register after claim: claimed" \
  || fail "Expected claimed, got: $STATUS"

# Heartbeat
HB=$(curl -s -X POST "$TPAPI/v1/heartbeat" \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"online","mem_free_mb":512,"disk_free_gb":20}' | jq -r .acknowledged)
[ "$HB" = "true" ] && ok "Heartbeat acknowledged" || fail "Heartbeat failed"

# Sync state
HASH=$(curl -s "$TPAPI/v1/sync/state" \
  -H "X-Qube-ID: $QUBE_ID" \
  -H "Authorization: Bearer $QUBE_TOKEN" | jq -r .hash)
ok "Sync state: hash=$HASH"

# Wait for conf-agent on qube-vm to auto-register
log "  Waiting 35s for conf-agent on $QUBE_VM to connect..."
sleep 35

QSTATUS=$(curl -s -H "Authorization: Bearer $TOKEN" "$CLOUD_API/api/v1/qubes/$QUBE_ID" | jq -r .status)
[ "$QSTATUS" = "online" ] && ok "Qube status: online (conf-agent connected)" \
  || fail "Expected online, got: $QSTATUS"

# ── 6. Reader + sensor → SQLite sync ─────────────────────────────────────────
log "[6/7] Creating reader + sensor, waiting for SQLite sync..."

MODBUS_RT=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$CLOUD_API/api/v1/reader-templates" | \
  jq -r '.[] | select(.protocol=="modbus_tcp") | .id' | head -1)

if [ -z "$MODBUS_RT" ] || [ "$MODBUS_RT" = "null" ]; then
  fail "No modbus reader template — check migrations/002_global_data.sql"
else
  READER=$(curl -s -X POST "$CLOUD_API/api/v1/qubes/$QUBE_ID/readers" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"E2E Test Reader\",\"protocol\":\"modbus_tcp\",\"template_id\":\"$MODBUS_RT\",\"config_json\":{\"host\":\"192.168.1.100\",\"port\":502,\"poll_interval_sec\":30}}")
  READER_ID=$(echo "$READER" | jq -r .reader_id)
  [ -n "$READER_ID" ] && [ "$READER_ID" != "null" ] \
    && ok "Reader created: $READER_ID" || fail "Reader creation failed: $READER"

  if [ -n "$READER_ID" ] && [ "$READER_ID" != "null" ]; then
    curl -s -X POST "$CLOUD_API/api/v1/readers/$READER_ID/sensors" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d '{"name":"E2E Meter","params":{"unit_id":1},"output":"influxdb"}' >/dev/null
    ok "Sensor added to reader"

    log "  Waiting 40s for conf-agent to sync config to SQLite..."
    sleep 40

    # Verify SQLite written on qube-vm
    SQLITE_EXISTS=$(multipass exec "$QUBE_VM" -- \
      bash -c "[ -f /opt/qube/data/qube.db ] && echo yes || echo no" 2>/dev/null)
    [ "$SQLITE_EXISTS" = "yes" ] && ok "SQLite qube.db exists on qube-vm" \
      || fail "SQLite not found at /opt/qube/data/qube.db on qube-vm"

    READER_COUNT=$(multipass exec "$QUBE_VM" -- \
      bash -c "sqlite3 /opt/qube/data/qube.db 'SELECT COUNT(*) FROM readers;' 2>/dev/null || echo 0")
    [ "${READER_COUNT:-0}" -gt 0 ] && ok "SQLite readers table: $READER_COUNT row(s)" \
      || fail "SQLite readers table empty"

    SENSOR_COUNT=$(multipass exec "$QUBE_VM" -- \
      bash -c "sqlite3 /opt/qube/data/qube.db 'SELECT COUNT(*) FROM sensors;' 2>/dev/null || echo 0")
    [ "${SENSOR_COUNT:-0}" -gt 0 ] && ok "SQLite sensors table: $SENSOR_COUNT row(s)" \
      || fail "SQLite sensors table empty"

    # Show SQLite content
    echo ""
    log "  SQLite readers on qube-vm:"
    multipass exec "$QUBE_VM" -- \
      bash -c "sqlite3 /opt/qube/data/qube.db 'SELECT id, name, protocol FROM readers;' 2>/dev/null" \
      | sed 's/^/    /'
  fi
fi

# ── 7. Summary ────────────────────────────────────────────────────────────────
echo ""
echo "=============================================="
echo " Multipass E2E Test Complete"
echo "=============================================="
echo "  Cloud API:   http://$CLOUD_IP:8080"
echo "  TP-API:      http://$CLOUD_IP:8081"
echo "  InfluxDB:    http://$CLOUD_IP:8086"
echo "  Qube VM:     $QUBE_IP"
echo ""
echo "  Inspect SQLite on qube-vm:"
echo "    multipass exec $QUBE_VM -- sqlite3 /opt/qube/data/qube.db '.tables'"
echo "    multipass exec $QUBE_VM -- sqlite3 /opt/qube/data/qube.db 'SELECT * FROM readers;'"
echo ""
echo "  Watch conf-agent logs:"
echo "    multipass exec $QUBE_VM -- journalctl -u enterprise-conf-agent -f"
echo ""
echo "  Cloud API logs:"
echo "    multipass exec $CLOUD_VM -- docker compose -f /opt/qube-enterprise/docker-compose.yml logs -f cloud-api"
echo ""
echo "  Clean up:"
echo "    multipass delete $CLOUD_VM $QUBE_VM && multipass purge"
echo "=============================================="
