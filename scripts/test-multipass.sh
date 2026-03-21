#!/bin/bash
# =============================================================================
# test-multipass.sh — End-to-end test using two Multipass VMs
#
# Creates:
#   cloud-vm  (amd64, 2CPU, 2GB) — runs Postgres + Enterprise Cloud API
#   qube-vm   (amd64, 1CPU, 1GB) — simulates a Qube device
#
# Run from the repo root:
#   chmod +x scripts/test-multipass.sh
#   ./scripts/test-multipass.sh
#
# Prerequisites:
#   - Multipass installed (https://multipass.run)
#   - Docker images built or available in registry
#   - ./cloud/migrations/ exists
# =============================================================================
set -e

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CLOUD_VM="cloud-vm"
QUBE_VM="qube-vm"
TEST_QUBE_ID="Q-1001"
TEST_REG_KEY="TEST-Q1001-REG"

echo "=============================================="
echo " Qube Enterprise — Multipass Integration Test"
echo "=============================================="

# ── 0. Check prerequisites ─────────────────────────────────────────────────────
command -v multipass &>/dev/null || { echo "ERROR: multipass not installed"; exit 1; }
command -v curl      &>/dev/null || { echo "ERROR: curl not installed"; exit 1; }
command -v jq        &>/dev/null || { echo "ERROR: jq not installed"; exit 1; }

# ── 1. Launch VMs ─────────────────────────────────────────────────────────────
echo "[1/7] Launching VMs..."

multipass launch --name "$CLOUD_VM" --cpus 2 --memory 2G --disk 10G 22.04 2>/dev/null \
  || echo "  $CLOUD_VM already exists, continuing"

multipass launch --name "$QUBE_VM"  --cpus 1 --memory 1G --disk 8G  22.04 2>/dev/null \
  || echo "  $QUBE_VM already exists, continuing"

CLOUD_IP=$(multipass info "$CLOUD_VM" | grep "IPv4" | awk '{print $2}')
QUBE_IP=$(multipass  info "$QUBE_VM"  | grep "IPv4" | awk '{print $2}')
echo "   cloud-vm IP: $CLOUD_IP"
echo "   qube-vm IP:  $QUBE_IP"

# ── 2. Copy files to cloud VM ─────────────────────────────────────────────────
echo "[2/7] Copying files to cloud-vm..."
multipass transfer -r "$REPO_DIR/cloud/migrations" "$CLOUD_VM":/tmp/migrations
multipass transfer    "$REPO_DIR/scripts/setup-cloud.sh" "$CLOUD_VM":/tmp/setup-cloud.sh

# ── 3. Setup cloud VM ─────────────────────────────────────────────────────────
echo "[3/7] Setting up cloud-vm (Postgres + Cloud API)..."
multipass exec "$CLOUD_VM" -- bash -c "
  sudo mkdir -p /opt/qube-enterprise/migrations
  sudo cp /tmp/migrations/*.sql /opt/qube-enterprise/migrations/
  chmod +x /tmp/setup-cloud.sh
  # For testing, build from local source instead of pulling from registry
  # This uses the dev compose approach
  sudo apt-get update -qq
  sudo apt-get install -y docker.io docker-compose-v2 2>/dev/null || true
  sudo apt-get install -y ca-certificates curl gnupg 2>/dev/null || true
"

# Transfer the docker-compose.dev approach to cloud VM
# Simpler: just run Postgres + a locally built cloud-api using docker compose
multipass transfer -r "$REPO_DIR/cloud" "$CLOUD_VM":/tmp/cloud-src

multipass exec "$CLOUD_VM" -- bash << CLOUDSETUP
set -e
# Install Docker
if ! command -v docker &>/dev/null; then
  curl -fsSL https://get.docker.com | sudo sh
fi

# Build cloud-api locally on the VM
cd /tmp/cloud-src
sudo docker build -t qube-cloud-api:local .

# Write compose file
sudo mkdir -p /opt/qube-enterprise
cat > /tmp/cloud-compose.yml << COMPOSE
version: "3.8"
services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: qubedb
      POSTGRES_USER: qubeadmin
      POSTGRES_PASSWORD: qubepass
    volumes:
      - /opt/qube-enterprise/migrations:/docker-entrypoint-initdb.d:ro
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "qubeadmin"]
      interval: 3s
      retries: 20
  cloud-api:
    image: qube-cloud-api:local
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      DATABASE_URL: postgres://qubeadmin:qubepass@postgres:5432/qubedb?sslmode=disable
      JWT_SECRET: multipass-test-secret-123
      API_PORT: "8080"
      TP_PORT: "8081"
volumes:
  pgdata:
COMPOSE

sudo cp /tmp/cloud-compose.yml /opt/qube-enterprise/docker-compose.yml
cd /opt/qube-enterprise
sudo docker compose up -d

echo "Waiting for API to be healthy..."
for i in \$(seq 1 30); do
  curl -s http://localhost:8080/health | grep -q "ok" && break
  sleep 3
done
curl -s http://localhost:8080/health
CLOUDSETUP

echo "   cloud-vm ready ✓"

# ── 4. Setup qube VM ──────────────────────────────────────────────────────────
echo "[4/7] Setting up qube-vm (simulated Qube device)..."

# Create a fake mit.txt on the qube VM
multipass exec "$QUBE_VM" -- bash << QUBESETUP
set -e
# Install Docker
if ! command -v docker &>/dev/null; then
  curl -fsSL https://get.docker.com | sudo sh
fi

# Create fake /boot/mit.txt (simulates real Qube device identity)
sudo bash -c "cat > /boot/mit.txt << MIT
deviceid: $TEST_QUBE_ID
devicename: $TEST_QUBE_ID
devicetype: rasp4_v2
register: $TEST_REG_KEY
maintain: TEST-Q1001-MNT
MIT"
echo "   /boot/mit.txt written: $TEST_QUBE_ID / $TEST_REG_KEY"

# Init swarm + qube-net (same as Qube Lite local.sh)
sudo docker swarm init || true
sudo docker network ls | grep -q qube-net || \
  sudo docker network create --attachable --driver overlay qube-net

sudo mkdir -p /opt/qube/configs
QUBESETUP

# Build and transfer conf-agent binary to qube VM
echo "[4b] Building conf-agent for the qube VM..."
multipass transfer -r "$REPO_DIR/conf-agent" "$QUBE_VM":/tmp/conf-agent-src

multipass exec "$QUBE_VM" -- bash << BUILDAGENT
set -e
sudo apt-get update -qq
sudo apt-get install -y golang 2>/dev/null || true
if command -v go &>/dev/null; then
  cd /tmp/conf-agent-src
  sudo go mod tidy
  sudo CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/enterprise-conf-agent .
  echo "   conf-agent built from source"
else
  echo "   WARNING: Go not available, cannot build conf-agent"
  echo "   Install Go on qube-vm or use docker image"
fi
BUILDAGENT

# Write systemd service on qube VM
multipass exec "$QUBE_VM" -- sudo bash << SERVICE
cat > /etc/systemd/system/enterprise-conf-agent.service << SVC
[Unit]
Description=Qube Enterprise Conf-Agent
After=network-online.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/qube
EnvironmentFile=-/opt/qube/.env
ExecStart=/usr/local/bin/enterprise-conf-agent
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=enterprise-conf-agent
SVC

cat > /opt/qube/.env << ENV
TPAPI_URL=http://$CLOUD_IP:8081
WORK_DIR=/opt/qube
POLL_INTERVAL=15
MIT_TXT_PATH=/boot/mit.txt
ENV

systemctl daemon-reload
systemctl enable enterprise-conf-agent.service
[ -f /usr/local/bin/enterprise-conf-agent ] && systemctl start enterprise-conf-agent.service || true
SERVICE

echo "   qube-vm ready ✓"

# ── 5. API test flow ──────────────────────────────────────────────────────────
echo "[5/7] Running API test flow..."
sleep 5  # let cloud API fully start

CLOUD_API="http://$CLOUD_IP:8080"
TPAPI="http://$CLOUD_IP:8081"

# Register org
TOKEN=$(curl -s -X POST "$CLOUD_API/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Multipass Test Org","email":"admin@mptest.com","password":"testpass123"}' \
  | jq -r .token)
echo "   JWT token obtained ✓"

# Check device register status (should be pending — not claimed yet)
REGISTER_STATUS=$(curl -s -X POST "$TPAPI/v1/device/register" \
  -H "Content-Type: application/json" \
  -d "{\"device_id\":\"$TEST_QUBE_ID\",\"register_key\":\"$TEST_REG_KEY\"}" \
  | jq -r .status)
echo "   Self-register before claim: $REGISTER_STATUS (expected: pending)"

# Claim the device
QUBE_AUTH_TOKEN=$(curl -s -X POST "$CLOUD_API/api/v1/qubes/claim" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"register_key\":\"$TEST_REG_KEY\"}" | jq -r .auth_token)
echo "   Device claimed ✓  token: ${QUBE_AUTH_TOKEN:0:16}..."

# Check device register status (should be claimed now)
REGISTER_STATUS=$(curl -s -X POST "$TPAPI/v1/device/register" \
  -H "Content-Type: application/json" \
  -d "{\"device_id\":\"$TEST_QUBE_ID\",\"register_key\":\"$TEST_REG_KEY\"}" \
  | jq -r .status)
echo "   Self-register after claim: $REGISTER_STATUS (expected: claimed)"

# Wait for conf-agent to auto-register and start heartbeats
echo "   Waiting 30s for conf-agent to register and sync..."
sleep 30

# Check qube is online
QUBE_STATUS=$(curl -s -H "Authorization: Bearer $TOKEN" \
  "$CLOUD_API/api/v1/qubes/$TEST_QUBE_ID" | jq -r .status)
echo "   Qube status: $QUBE_STATUS (expected: online)"

# ── 6. Add gateway + sensor ───────────────────────────────────────────────────
echo "[6/7] Adding gateway and sensor..."

MODBUS_TMPL_ID=$(curl -s -H "Authorization: Bearer $TOKEN" "$CLOUD_API/api/v1/templates" \
  | jq -r '[.[] | select(.is_global==true and .protocol=="modbus_tcp")][0].id')

GW_ID=$(curl -s -X POST "$CLOUD_API/api/v1/qubes/$TEST_QUBE_ID/gateways" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Panel_A","protocol":"modbus_tcp","host":"192.168.1.100","port":502}' \
  | jq -r .gateway_id)
echo "   Gateway created: $GW_ID"

SENSOR_ID=$(curl -s -X POST "$CLOUD_API/api/v1/gateways/$GW_ID/sensors" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"Main_Meter\",\"template_id\":\"$MODBUS_TMPL_ID\",\"address_params\":{\"unit_id\":1},\"tags_json\":{\"location\":\"panel_a\"}}" \
  | jq -r .sensor_id)
echo "   Sensor created: $SENSOR_ID"

echo "   Waiting 40s for conf-agent to sync config..."
sleep 40

# Check docker-compose.yml appeared on qube-vm
COMPOSE_EXISTS=$(multipass exec "$QUBE_VM" -- \
  bash -c "[ -f /opt/qube/docker-compose.yml ] && echo 'yes' || echo 'no'")
echo "   docker-compose.yml on qube-vm: $COMPOSE_EXISTS (expected: yes)"

CSV_EXISTS=$(multipass exec "$QUBE_VM" -- \
  bash -c "[ -f /opt/qube/configs/panel-a/config.csv ] && echo 'yes' || echo 'no'")
echo "   config.csv for panel-a: $CSV_EXISTS (expected: yes)"

SENSOR_MAP_EXISTS=$(multipass exec "$QUBE_VM" -- \
  bash -c "[ -f /opt/qube/sensor_map.json ] && echo 'yes' || echo 'no'")
echo "   sensor_map.json: $SENSOR_MAP_EXISTS (expected: yes)"

# ── 7. Summary ────────────────────────────────────────────────────────────────
echo ""
echo "=============================================="
echo " Multipass Integration Test Complete"
echo "=============================================="
echo "  Cloud API:   http://$CLOUD_IP:8080"
echo "  TP-API:      http://$CLOUD_IP:8081"
echo "  Qube VM:     $QUBE_IP"
echo ""
echo "  Useful commands:"
echo "  multipass exec $QUBE_VM  -- journalctl -u enterprise-conf-agent -f"
echo "  multipass exec $CLOUD_VM -- docker compose -f /opt/qube-enterprise/docker-compose.yml logs -f"
echo "  multipass exec $QUBE_VM  -- cat /opt/qube/docker-compose.yml"
echo "  multipass exec $QUBE_VM  -- cat /opt/qube/configs/panel-a/config.csv"
echo ""
echo "  To clean up:"
echo "  multipass delete $CLOUD_VM $QUBE_VM && multipass purge"
echo "=============================================="
