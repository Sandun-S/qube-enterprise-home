#!/usr/bin/env bash
# =============================================================================
# test-multipass-api.sh — Run full API test suite against Multipass VMs
#
# Runs FROM YOUR DESKTOP against two Multipass VMs:
#   cloud-vm — runs Postgres + Enterprise Cloud API
#   qube-vm  — simulates a Qube device
#
# Prerequisites:
#   multipass installed on your desktop
#   VMs already created and running (or use --create to create them)
#   jq installed on your desktop
#
# Usage:
#   ./scripts/test-multipass-api.sh                    # test existing VMs
#   ./scripts/test-multipass-api.sh --create           # create VMs first
#   ./scripts/test-multipass-api.sh --destroy          # destroy VMs after test
#   ./scripts/test-multipass-api.sh --create --destroy # full cycle
# =============================================================================
set -e

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
GREEN='\033[0;32m'; RED='\033[0;31m'; CYAN='\033[0;36m'; NC='\033[0m'
log()  { echo -e "${CYAN}[test]${NC} $1"; }
ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; }

command -v multipass &>/dev/null || { echo "ERROR: multipass not installed"; exit 1; }
command -v curl      &>/dev/null || { echo "ERROR: curl not installed"; exit 1; }
command -v jq        &>/dev/null || { echo "ERROR: jq not installed"; exit 1; }

# ── 1. Create VMs if requested ────────────────────────────────────────────────
if $CREATE_VMS; then
  log "Creating Multipass VMs..."
  multipass launch --name "$CLOUD_VM" --cpus 2 --memory 2G --disk 10G 22.04 2>/dev/null \
    || log "$CLOUD_VM already exists"
  multipass launch --name "$QUBE_VM"  --cpus 1 --memory 1G --disk 8G  22.04 2>/dev/null \
    || log "$QUBE_VM already exists"

  log "Getting VM IPs..."
fi

CLOUD_IP=$(multipass info "$CLOUD_VM" 2>/dev/null | grep "IPv4" | awk '{print $2}')
QUBE_IP=$(multipass  info "$QUBE_VM"  2>/dev/null | grep "IPv4" | awk '{print $2}')

if [ -z "$CLOUD_IP" ]; then
  echo "ERROR: $CLOUD_VM not found. Run with --create first or create manually."
  exit 1
fi

log "cloud-vm: $CLOUD_IP"
log "qube-vm:  $QUBE_IP"
log "Running tests against: http://$CLOUD_IP:8080"
echo ""

# ── 2. Setup cloud VM if not already running ──────────────────────────────────
HEALTH=$(curl -sf "http://$CLOUD_IP:8080/health" 2>/dev/null | jq -r .status 2>/dev/null)
if [ "$HEALTH" != "ok" ]; then
  log "Cloud API not running — setting up cloud-vm..."

  # Transfer source
  multipass transfer -r "$REPO_DIR/cloud" "$CLOUD_VM":/tmp/cloud-src 2>/dev/null
  multipass transfer -r "$REPO_DIR/cloud/migrations" "$CLOUD_VM":/tmp/migrations 2>/dev/null

  multipass exec "$CLOUD_VM" -- sudo bash << 'CLOUDSETUP'
set -e
# Install Docker if needed
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh

# Build cloud-api locally
sudo mkdir -p /opt/qube-enterprise/migrations
sudo cp /tmp/migrations/*.sql /opt/qube-enterprise/migrations/
cd /tmp/cloud-src
sudo docker build -t cloud-api:local .

cat > /tmp/compose.yml << 'COMPOSE'
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
    ports: ["5432:5432"]
    healthcheck:
      test: ["CMD","pg_isready","-U","qubeadmin"]
      interval: 3s
      retries: 20
  cloud-api:
    image: cloud-api:local
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      DATABASE_URL: postgres://qubeadmin:qubepass@postgres:5432/qubedb?sslmode=disable
      JWT_SECRET: multipass-test-secret
      API_PORT: "8080"
      TP_PORT: "8081"
      QUBE_IMAGE_REGISTRY: "ghcr.io/sandun-s/qube-enterprise-home"
volumes:
  pgdata:
COMPOSE

sudo cp /tmp/compose.yml /opt/qube-enterprise/docker-compose.yml
cd /opt/qube-enterprise
sudo docker compose up -d
echo "Waiting for API..."
for i in $(seq 1 30); do
  curl -sf http://localhost:8080/health | grep -q "ok" && echo "API ready" && exit 0
  sleep 3
done
echo "ERROR: API failed to start"
exit 1
CLOUDSETUP
  log "cloud-vm setup complete"
else
  log "Cloud API already running ✓"
fi

# ── 3. Setup qube VM if not already configured ────────────────────────────────
QUBE_CONF_STATUS=$(multipass exec "$QUBE_VM" -- \
  bash -c "systemctl is-active enterprise-conf-agent 2>/dev/null || echo inactive")

if [ "$QUBE_CONF_STATUS" = "inactive" ] || [ "$QUBE_CONF_STATUS" = "unknown" ]; then
  log "Setting up qube-vm..."

  multipass exec "$QUBE_VM" -- sudo bash << QUBESETUP
set -e
command -v docker &>/dev/null || curl -fsSL https://get.docker.com | sh
sudo docker swarm init 2>/dev/null || true
sudo docker network ls | grep -q qube-net || \
  sudo docker network create --attachable --driver overlay qube-net

# Write fake /boot/mit.txt (simulates real Qube device)
sudo bash -c 'cat > /boot/mit.txt << MIT
deviceid: Q-1001
devicename: Q-1001
devicetype: rasp4_v2
register: TEST-Q1001-REG
maintain: TEST-Q1001-MNT
MIT'

sudo mkdir -p /opt/qube/configs

# Write .env for conf-agent
sudo bash -c 'cat > /opt/qube/.env << ENV
TPAPI_URL=http://$CLOUD_IP:8081
WORK_DIR=/opt/qube
POLL_INTERVAL=15
MIT_TXT_PATH=/boot/mit.txt
ENV'
echo "qube-vm configured"
QUBESETUP
  log "qube-vm setup complete"
fi

# ── 4. Run the API test suite from desktop ────────────────────────────────────
log "Running API test suite against http://$CLOUD_IP:8080 ..."
echo ""

chmod +x "$REPO_DIR/test/test_api.sh"
"$REPO_DIR/test/test_api.sh" "http://$CLOUD_IP:8080"
TEST_EXIT=$?

echo ""

# ── 5. Run device integration checks ─────────────────────────────────────────
log "Running device integration checks..."
echo ""

BASE="http://$CLOUD_IP:8080"
TPBASE="http://$CLOUD_IP:8081"
PASS=0; FAIL=0

check() {
  local label=$1 expected=$2 actual=$3
  if [ "$actual" = "$expected" ]; then ok "$label"; ((PASS++))
  else fail "$label — expected '$expected', got '$actual'"; ((FAIL++)); fi
}

# Register org for integration test
TOKEN=$(curl -sf -X POST "$BASE/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d "{\"org_name\":\"Multipass Test\",\"email\":\"mp_$(date +%s)@test.com\",\"password\":\"testpass\"}" \
  2>/dev/null | jq -r .token)

check "Register org" "true" "$([ -n "$TOKEN" ] && echo true || echo false)"

# Check device self-register (should be pending before claim)
REGISTER_STATUS=$(curl -sf -X POST "$TPBASE/v1/device/register" \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' \
  2>/dev/null | jq -r .status 2>/dev/null)

check "Device self-register before claim returns pending" "pending" "$REGISTER_STATUS"

# Claim device
CLAIM=$(curl -sf -X POST "$BASE/api/v1/qubes/claim" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"register_key":"TEST-Q1001-REG"}' 2>/dev/null)
QUBE_TOKEN=$(echo "$CLAIM" | jq -r .auth_token 2>/dev/null)

check "Claim device" "true" "$([ -n "$QUBE_TOKEN" ] && echo true || echo false)"

# Check device self-register after claim (should return claimed + token)
REGISTER_AFTER=$(curl -sf -X POST "$TPBASE/v1/device/register" \
  -H "Content-Type: application/json" \
  -d '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' \
  2>/dev/null | jq -r .status 2>/dev/null)

check "Device self-register after claim returns claimed" "claimed" "$REGISTER_AFTER"

# Heartbeat
HB=$(curl -sf -X POST "$TPBASE/v1/heartbeat" \
  -H "Content-Type: application/json" \
  -H "X-Qube-ID: Q-1001" \
  -H "Authorization: Bearer $QUBE_TOKEN" \
  -d '{}' 2>/dev/null | jq -r .acknowledged 2>/dev/null)

check "Heartbeat acknowledged" "true" "$HB"

# Wait for qube-vm conf-agent to pick up the claim and send heartbeat
if [ -n "$QUBE_IP" ]; then
  log "Waiting 30s for qube-vm conf-agent to auto-register..."
  sleep 30

  QUBE_STATUS=$(curl -sf -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/qubes/Q-1001" 2>/dev/null | jq -r .status 2>/dev/null)
  check "Qube status updated to online by conf-agent" "online" "$QUBE_STATUS"

  # Add a gateway and wait for conf-agent to sync
  MODBUS_TMPL=$(curl -sf -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/templates" \
    2>/dev/null | jq -r '[.[] | select(.is_global==true and .protocol=="modbus_tcp")][0].id')

  if [ -n "$MODBUS_TMPL" ] && [ "$MODBUS_TMPL" != "null" ]; then
    GW_ID=$(curl -sf -X POST "$BASE/api/v1/qubes/Q-1001/gateways" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d '{"name":"Test_Panel","protocol":"modbus_tcp","host":"192.168.1.100","port":502}' \
      2>/dev/null | jq -r .gateway_id)

    curl -sf -X POST "$BASE/api/v1/gateways/$GW_ID/sensors" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d "{\"name\":\"Test_Meter\",\"template_id\":\"$MODBUS_TMPL\",\"address_params\":{\"unit_id\":1},\"tags_json\":{}}" \
      >/dev/null 2>/dev/null

    log "Gateway+sensor added. Waiting 40s for conf-agent to sync..."
    sleep 40

    # Check files appeared on qube-vm
    COMPOSE_OK=$(multipass exec "$QUBE_VM" -- \
      bash -c "[ -f /opt/qube/docker-compose.yml ] && echo yes || echo no" 2>/dev/null)
    check "docker-compose.yml written to qube-vm" "yes" "$COMPOSE_OK"

    CSV_OK=$(multipass exec "$QUBE_VM" -- \
      bash -c "[ -f /opt/qube/configs/test-panel/config.csv ] && echo yes || echo no" 2>/dev/null)
    check "config.csv written for test-panel gateway" "yes" "$CSV_OK"

    SENSOR_MAP_OK=$(multipass exec "$QUBE_VM" -- \
      bash -c "[ -f /opt/qube/sensor_map.json ] && echo yes || echo no" 2>/dev/null)
    check "sensor_map.json written to qube-vm" "yes" "$SENSOR_MAP_OK"
  else
    fail "Could not find modbus template for gateway test"
    ((FAIL++))
  fi
fi

# ── 6. Summary ────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════"
echo " Integration Test Summary (device checks)"
echo "════════════════════════════════════════════════"
echo -e "  ${GREEN}PASS${NC}: $PASS"
echo -e "  ${RED}FAIL${NC}: $FAIL"
echo "════════════════════════════════════════════════"
echo ""
echo " Cloud API:  http://$CLOUD_IP:8080"
echo " TP-API:     http://$CLOUD_IP:8081"
[ -n "$QUBE_IP" ] && echo " Qube VM:    $QUBE_IP"
echo ""
echo " Useful commands:"
echo "   multipass exec $QUBE_VM -- journalctl -u enterprise-conf-agent -f"
echo "   multipass exec $QUBE_VM -- cat /opt/qube/docker-compose.yml"
echo "   multipass exec $QUBE_VM -- cat /opt/qube/configs/test-panel/config.csv"
echo "   multipass shell $CLOUD_VM"

# ── 7. Cleanup ────────────────────────────────────────────────────────────────
if $DESTROY_VMS; then
  log "Destroying VMs..."
  multipass delete "$CLOUD_VM" "$QUBE_VM" 2>/dev/null || true
  multipass purge 2>/dev/null || true
  log "VMs destroyed"
fi

# Exit with the API test result
exit $TEST_EXIT
