#!/bin/bash
# setup-cloud.sh — Qube Enterprise v2 Cloud VM Setup
# Usage: chmod +x setup-cloud.sh && sudo ./setup-cloud.sh
#
# Expects the following already copied to the VM:
#   $WORK_DIR/migrations/            — cloud/migrations/*.sql
#   $WORK_DIR/migrations-telemetry/  — cloud/migrations-telemetry/*.sql
set -e

CLOUD_API_IMAGE="${CLOUD_API_IMAGE:-ghcr.io/sandun-s/qube-enterprise-home/cloud-api:amd64.latest}"
TEST_UI_IMAGE="${TEST_UI_IMAGE:-ghcr.io/sandun-s/qube-enterprise-home/test-ui:amd64.latest}"
DB_PASS="${DB_PASS:-qubepass}"
DB_NAME="${DB_NAME:-qubedb}"
DB_USER="${DB_USER:-qubeadmin}"
WORK_DIR="/opt/qube-enterprise"
JWT_SECRET="${JWT_SECRET:-$(openssl rand -hex 32)}"
CLOUD_IP=$(hostname -I | awk '{print $1}')

echo "== Qube Enterprise v2 — Cloud VM Setup =="
echo "   IP: $CLOUD_IP   Work: $WORK_DIR"

# Install Docker
if ! command -v docker &>/dev/null; then
  apt-get update -qq
  apt-get install -y ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
    | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
    https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
    | tee /etc/apt/sources.list.d/docker.list >/dev/null
  apt-get update -qq
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
fi

mkdir -p "$WORK_DIR"
cd "$WORK_DIR"

# Validate migrations are present
if [ ! -d "migrations" ] || [ ! -d "migrations-telemetry" ]; then
  echo "ERROR: migrations directories missing."
  echo "  Copy from repo: cloud/migrations/ and cloud/migrations-telemetry/"
  exit 1
fi

cat > docker-compose.yml << COMPOSE
networks:
  qube_net:
    driver: bridge
volumes:
  postgres_data:
services:
  postgres:
    image: timescale/timescaledb:latest-pg16
    restart: unless-stopped
    networks: [qube_net]
    environment:
      POSTGRES_DB:       $DB_NAME
      POSTGRES_USER:     $DB_USER
      POSTGRES_PASSWORD: $DB_PASS
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - $WORK_DIR/cloud/migrations/001_init.sql:/docker-entrypoint-initdb.d/001_init.sql:ro
      - $WORK_DIR/cloud/migrations/002_global_data.sql:/docker-entrypoint-initdb.d/002_global_data.sql:ro
      - $WORK_DIR/cloud/migrations/003_test_seeds.sql:/docker-entrypoint-initdb.d/003_test_seeds.sql:ro
      - $WORK_DIR/cloud/migrations-telemetry/001_timescale_init.sh:/docker-entrypoint-initdb.d/010_timescale_init.sh:ro
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $DB_USER -d $DB_NAME"]
      interval: 5s
      retries: 20

  cloud-api:
    image: $CLOUD_API_IMAGE
    restart: unless-stopped
    networks: [qube_net]
    depends_on:
      postgres: {condition: service_healthy}
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      DATABASE_URL:           postgres://$DB_USER:$DB_PASS@postgres:5432/$DB_NAME?sslmode=disable
      TELEMETRY_DATABASE_URL: postgres://$DB_USER:$DB_PASS@postgres:5432/qubedata?sslmode=disable
      JWT_SECRET:             $JWT_SECRET
      QUBE_IMAGE_REGISTRY:    ghcr.io/sandun-s/qube-enterprise-home

  test-ui:
    image: $TEST_UI_IMAGE
    restart: unless-stopped
    networks: [qube_net]
    ports:
      - "8888:80"
COMPOSE

echo "JWT_SECRET=$JWT_SECRET"    > cloud-credentials.txt
echo "CLOUD_IP=$CLOUD_IP"       >> cloud-credentials.txt
echo "Superadmin: iotteam@internal.local / iotteam2024" >> cloud-credentials.txt

docker compose pull
docker compose up -d

echo "Waiting for Cloud API..."
for i in $(seq 1 40); do
  curl -sf http://localhost:8080/health | grep -q '"ok"' && break
  sleep 3
done
curl -sf http://localhost:8080/health | jq . || true

echo ""
echo "== Stack running =="
echo "   Cloud API:  http://$CLOUD_IP:8080"
echo "   TP-API:     http://$CLOUD_IP:8081"
echo "   Test UI:    http://$CLOUD_IP:8888"
echo "   Superadmin: iotteam@internal.local / iotteam2024"
echo "   Credentials saved to: $WORK_DIR/cloud-credentials.txt"
