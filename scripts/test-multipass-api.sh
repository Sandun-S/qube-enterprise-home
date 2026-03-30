#!/usr/bin/env bash
# =============================================================================
# test-multipass-api.sh — Run full API test suite against Multipass VMs (v2)
#
# Runs FROM YOUR DESKTOP against two Multipass VMs:
#   qube-cloud-vm — Postgres (TimescaleDB) + Enterprise Cloud API + InfluxDB
#   qube-device-vm — simulates a Qube (conf-agent, SQLite, Docker Swarm)
#
# Prerequisites:
#   multipass, curl, jq installed on your desktop
#   VMs already created and running (or use --create to provision them)
#
# Usage:
#   ./scripts/test-multipass-api.sh                    # test existing VMs
#   ./scripts/test-multipass-api.sh --create           # provision VMs first
#   ./scripts/test-multipass-api.sh --destroy          # destroy VMs after test
#   ./scripts/test-multipass-api.sh --create --destroy # full cycle
# =============================================================================
set -euo pipefail

CLOUD_VM="qube-cloud-vm"
QUBE_VM="qube-device-vm"
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CREATE_VMS=false
DESTROY_VMS=false

for arg in "$@"; do
  case $arg in
    --create)  CREATE_VMS=true  ;;
    --destroy) DESTROY_VMS=true ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; RED='\033[0;31m'; CYAN='\033[0;36m'; YELLOW='\033[0;33m'; NC='\033[0m'
log()   { echo -e "${CYAN}[test]${NC} $1"; }
ok()    { echo -e "  ${GREEN}✓${NC} $1"; }
fail()  { echo -e "  ${RED}✗${NC} $1"; ((FAIL++)) || true; }
warn()  { echo -e "  ${YELLOW}!${NC} $1"; }
PASS=0; FAIL=0
check() {
  local label=$1 expected=$2 actual=$3
  if [ "$actual" = "$expected" ]; then ok "$label"; ((PASS++)) || true
  else fail "$label — expected '$expected', got '$actual'"; fi
}

command -v multipass &>/dev/null || { echo "ERROR: multipass not installed"; exit 1; }
command -v curl      &>/dev/null || { echo "ERROR: curl not installed"; exit 1; }
command -v jq        &>/dev/null || { echo "ERROR: jq not installed"; exit 1; }

# ── 1. Create VMs if requested ────────────────────────────────────────────────
if $CREATE_VMS; then
  log "Creating Multipass VMs..."
  multipass launch --name "$CLOUD_VM" --cpus 2 --memory 3G --disk 15G 22.04 2>/dev/null \
    || log "$CLOUD_VM already exists"
  multipass launch --name "$QUBE_VM"  --cpus 1 --memory 1G --disk 8G  22.04 2>/dev/null \
    || log "$QUBE_VM already exists"
fi

CLOUD_IP=$(multipass info "$CLOUD_VM" 2>/dev/null | grep "IPv4" | awk '{print $2}')
QUBE_IP=$(multipass  info "$QUBE_VM"  2>/dev/null | grep "IPv4" | awk '{print $2}' || echo "")

if [ -z "$CLOUD_IP" ]; then
  echo "ERROR: $CLOUD_VM not found. Run with --create first."
  exit 1
fi

log "cloud-vm: $CLOUD_IP"
[ -n "$QUBE_IP" ] && log "qube-vm:  $QUBE_IP" || warn "qube-vm not found — skipping device integration checks"
log "Running tests against: http://$CLOUD_IP:8080"
echo ""

# ── 2. Setup cloud VM ─────────────────────────────────────────────────────────
HEALTH=$(curl -sf "http://$CLOUD_IP:8080/health" 2>/dev/null | jq -r .status 2>/dev/null || echo "")
if [ "$HEALTH" != "ok" ]; then
  log "Cloud API not running — setting up $CLOUD_VM..."

  # Transfer source + all migrations
  multipass transfer -r "$REPO_DIR/cloud" "$CLOUD_VM":/tmp/cloud-src
  multipass transfer -r "$REPO_DIR/cloud/migrations" "$CLOUD_VM":/tmp/migrations
  multipass transfer -r "$REPO_DIR/cloud/migrations-telemetry" "$CLOUD_VM":/tmp/migrations-telemetry

  multipass exec "$CLOUD_VM" -- sudo bash << 'CLOUDSETUP'
set -e

# Install Docker
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh

# Build cloud-api image from source
cd /tmp/cloud-src
sudo docker build -t qube-cloud-api:local .

# Copy migrations
sudo mkdir -p /opt/qube-enterprise/migrations
sudo mkdir -p /opt/qube-enterprise/migrations-telemetry
sudo cp /tmp/migrations/*.sql /opt/qube-enterprise/migrations/
sudo cp /tmp/migrations-telemetry/*.sql /opt/qube-enterprise/migrations-telemetry/

# Write compose — mirrors docker-compose.dev.yml (without conf-agent/readers)
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
      JWT_SECRET: multipass-test-secret-change-me
      QUBE_IMAGE_REGISTRY: ghcr.io/sandun-s/qube-enterprise-home
    depends_on:
      postgres: {condition: service_healthy}
COMPOSE

cd /opt/qube-enterprise
sudo docker compose up -d

echo "Waiting for Cloud API..."
for i in $(seq 1 40); do
  curl -sf http://localhost:8080/health | grep -q '"ok"' && echo "API ready" && exit 0
  sleep 3
done
echo "ERROR: Cloud API did not start in time"
exit 1
CLOUDSETUP

  log "cloud-vm setup complete"
else
  log "Cloud API already running ✓"
fi

# ── 3. Setup qube VM ──────────────────────────────────────────────────────────
if [ -n "$QUBE_IP" ]; then
  AGENT_STATUS=$(multipass exec "$QUBE_VM" -- \
    bash -c "systemctl is-active enterprise-conf-agent 2>/dev/null || echo inactive")

  if [ "$AGENT_STATUS" != "active" ]; then
    log "Setting up $QUBE_VM..."

    # Transfer conf-agent source
    multipass transfer -r "$REPO_DIR/conf-agent" "$QUBE_VM":/tmp/conf-agent-src
    multipass transfer "$REPO_DIR/test/mit.txt" "$QUBE_VM":/tmp/mit.txt

    multipass exec "$QUBE_VM" -- sudo bash << QUBESETUP
set -e

# Install Docker + sqlite3 + Go
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh
apt-get install -y sqlite3 golang-go 2>/dev/null || true

# Fake /boot/mit.txt (Qube identity)
cp /tmp/mit.txt /boot/mit.txt
echo "Qube identity: $(cat /boot/mit.txt | head -1)"

# Docker Swarm + qube-net overlay
docker swarm init 2>/dev/null || true
docker network ls | grep -q qube-net || \
  docker network create --attachable --driver overlay qube-net

# SQLite data directory
mkdir -p /opt/qube/data

# Build conf-agent from source
cd /tmp/conf-agent-src
go mod tidy 2>/dev/null || true
CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/enterprise-conf-agent .
echo "conf-agent built"

# Write env file for v2
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
echo "conf-agent service started"
QUBESETUP

    log "qube-vm setup complete"
  else
    log "conf-agent already running on qube-vm ✓"
  fi
fi

# ── 4. Run the full API test suite ────────────────────────────────────────────
log "Running full API test suite against http://$CLOUD_IP:8080 ..."
echo ""

chmod +x "$REPO_DIR/test/test_api.sh"
"$REPO_DIR/test/test_api.sh" "http://$CLOUD_IP:8080"
TEST_EXIT=$?

echo ""

# ── 5. Device integration checks (qube-vm only) ───────────────────────────────
if [ -n "$QUBE_IP" ]; then
  log "Running device integration checks against qube-vm..."
  echo ""

  BASE="http://$CLOUD_IP:8080"
  TPBASE="http://$CLOUD_IP:8081"

  # Register a fresh org for integration test
  TS=$(date +%s)
  TOKEN=$(curl -sf -X POST "$BASE/api/v1/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"org_name\":\"MP Integration $TS\",\"email\":\"mp$TS@test.local\",\"password\":\"testpass123\"}" \
    2>/dev/null | jq -r .token)
  check "Register org for integration test" "true" "$([ -n "$TOKEN" ] && echo true || echo false)"

  # Self-register before claim → pending
  STATUS_BEFORE=$(curl -sf -X POST "$TPBASE/v1/device/register" \
    -H "Content-Type: application/json" \
    -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' \
    2>/dev/null | jq -r .status 2>/dev/null || echo "")
  check "Self-register before claim → pending" "pending" "$STATUS_BEFORE"

  # Claim device
  CLAIM=$(curl -sf -X POST "$BASE/api/v1/qubes/claim" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"register_key":"TEST-Q1001-REG"}' 2>/dev/null || echo "{}")
  QUBE_TOKEN=$(echo "$CLAIM" | jq -r .auth_token 2>/dev/null || echo "")
  check "Claim device" "true" "$([ -n "$QUBE_TOKEN" ] && [ "$QUBE_TOKEN" != "null" ] && echo true || echo false)"

  # Self-register after claim → claimed + token
  STATUS_AFTER=$(curl -sf -X POST "$TPBASE/v1/device/register" \
    -H "Content-Type: application/json" \
    -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' \
    2>/dev/null | jq -r .status 2>/dev/null || echo "")
  check "Self-register after claim → claimed" "claimed" "$STATUS_AFTER"

  # Heartbeat
  HB=$(curl -sf -X POST "$TPBASE/v1/heartbeat" \
    -H "X-Qube-ID: Q-1001" \
    -H "Authorization: Bearer $QUBE_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"status":"online","mem_free_mb":512,"disk_free_gb":20}' \
    2>/dev/null | jq -r .acknowledged 2>/dev/null || echo "false")
  check "Heartbeat acknowledged" "true" "$HB"

  # Wait for conf-agent on qube-vm to pick up the claim
  log "Waiting 35s for conf-agent to auto-register and connect..."
  sleep 35

  QUBE_STATUS=$(curl -sf -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/qubes/Q-1001" 2>/dev/null | jq -r .status 2>/dev/null || echo "")
  check "Qube status → online (conf-agent connected)" "online" "$QUBE_STATUS"

  # Add a reader + sensor, then wait for conf-agent to sync SQLite
  MODBUS_RT=$(curl -sf -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/reader-templates" 2>/dev/null | \
    jq -r '.[] | select(.protocol=="modbus_tcp") | .id' | head -1)

  if [ -n "$MODBUS_RT" ] && [ "$MODBUS_RT" != "null" ]; then
    READER_RESP=$(curl -sf -X POST "$BASE/api/v1/qubes/Q-1001/readers" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d "{\"name\":\"Integration Test Reader\",\"protocol\":\"modbus_tcp\",\"template_id\":\"$MODBUS_RT\",\"config_json\":{\"host\":\"192.168.1.100\",\"port\":502,\"poll_interval_sec\":30}}" \
      2>/dev/null || echo "{}")
    READER_ID=$(echo "$READER_RESP" | jq -r .reader_id 2>/dev/null || echo "")
    check "Create modbus reader" "true" "$([ -n "$READER_ID" ] && [ "$READER_ID" != "null" ] && echo true || echo false)"

    if [ -n "$READER_ID" ] && [ "$READER_ID" != "null" ]; then
      curl -sf -X POST "$BASE/api/v1/readers/$READER_ID/sensors" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        -d '{"name":"Integration Test Meter","params":{"unit_id":1},"output":"influxdb"}' \
        >/dev/null 2>&1 || true

      log "Reader + sensor added. Waiting 40s for conf-agent to sync to SQLite..."
      sleep 40

      # Check SQLite was written on qube-vm
      SQLITE_EXISTS=$(multipass exec "$QUBE_VM" -- \
        bash -c "[ -f /opt/qube/data/qube.db ] && echo yes || echo no" 2>/dev/null || echo "no")
      check "SQLite qube.db written to qube-vm" "yes" "$SQLITE_EXISTS"

      READERS_OK=$(multipass exec "$QUBE_VM" -- \
        bash -c "sqlite3 /opt/qube/data/qube.db 'SELECT COUNT(*) FROM readers;' 2>/dev/null || echo 0")
      check "SQLite readers table has rows" "true" "$([ "${READERS_OK:-0}" -gt 0 ] 2>/dev/null && echo true || echo false)"

      SENSORS_OK=$(multipass exec "$QUBE_VM" -- \
        bash -c "sqlite3 /opt/qube/data/qube.db 'SELECT COUNT(*) FROM sensors;' 2>/dev/null || echo 0")
      check "SQLite sensors table has rows" "true" "$([ "${SENSORS_OK:-0}" -gt 0 ] 2>/dev/null && echo true || echo false)"
    fi
  else
    warn "No modbus reader template found — skipping reader/sensor sync checks"
  fi

  # ── Integration summary ──────────────────────────────────────────────────────
  echo ""
  echo "════════════════════════════════════════════════"
  echo " Device Integration Checks"
  echo "════════════════════════════════════════════════"
  echo -e "  ${GREEN}PASS${NC}: $PASS   ${RED}FAIL${NC}: $FAIL"
  echo "════════════════════════════════════════════════"
  echo ""
  echo "  Cloud API:  http://$CLOUD_IP:8080"
  echo "  TP-API:     http://$CLOUD_IP:8081"
  echo "  Qube VM:    $QUBE_IP"
  echo ""
  echo "  Useful commands:"
  echo "    multipass exec $QUBE_VM -- journalctl -u enterprise-conf-agent -f"
  echo "    multipass exec $QUBE_VM -- sqlite3 /opt/qube/data/qube.db '.tables'"
  echo "    multipass exec $QUBE_VM -- sqlite3 /opt/qube/data/qube.db 'SELECT id,name,protocol FROM readers;'"
  echo "    multipass shell $CLOUD_VM"
fi

# ── 6. Cleanup ────────────────────────────────────────────────────────────────
if $DESTROY_VMS; then
  log "Destroying VMs..."
  multipass delete "$CLOUD_VM" "$QUBE_VM" 2>/dev/null || true
  multipass purge 2>/dev/null || true
  log "VMs destroyed"
fi

exit $TEST_EXIT
