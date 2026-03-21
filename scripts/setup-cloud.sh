#!/bin/bash
# setup-cloud.sh — Qube Enterprise Cloud VM Setup
# Works for Multipass VM test and production cloud VM.
# Usage: chmod +x setup-cloud.sh && sudo ./setup-cloud.sh
set -e

REGISTRY="registry.gitlab.com/iot-team4/product/enterprise-cloud-api"
IMAGE_TAG="${IMAGE_TAG:-amd64.latest}"
DB_PASS="${DB_PASS:-qubepass}"
DB_NAME="${DB_NAME:-qubedb}"
DB_USER="${DB_USER:-qubeadmin}"
WORK_DIR="/opt/qube-enterprise"
JWT_SECRET="${JWT_SECRET:-$(openssl rand -hex 32)}"
CLOUD_IP=$(hostname -I | awk '{print $1}')

echo "== Qube Enterprise — Cloud VM Setup =="
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

cat > docker-compose.yml << COMPOSE
version: "3.8"
services:
  postgres:
    image: postgres:15
    restart: unless-stopped
    environment:
      POSTGRES_DB:       $DB_NAME
      POSTGRES_USER:     $DB_USER
      POSTGRES_PASSWORD: $DB_PASS
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "$DB_USER", "-d", "$DB_NAME"]
      interval: 5s
      retries: 20

  cloud-api:
    image: $REGISTRY:$IMAGE_TAG
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      DATABASE_URL: postgres://$DB_USER:$DB_PASS@postgres:5432/$DB_NAME?sslmode=disable
      JWT_SECRET:   $JWT_SECRET
      API_PORT:     8080
      TP_PORT:      8081

volumes:
  postgres_data:
COMPOSE

if [ ! -d "migrations" ]; then
  echo "ERROR: copy migrations first: cp -r /repo/cloud/migrations $WORK_DIR/"
  exit 1
fi

cat > .env << ENV
JWT_SECRET=$JWT_SECRET
DB_PASS=$DB_PASS
CLOUD_IP=$CLOUD_IP
ENV
echo "JWT_SECRET: $JWT_SECRET" > cloud-credentials.txt
echo "Superadmin: iotteam@internal.local / iotteam2024" >> cloud-credentials.txt

docker compose pull cloud-api 2>/dev/null || true
docker compose up -d

echo "Waiting for API..."
until curl -s http://localhost:8080/health | grep -q "ok"; do sleep 2; done

echo ""
echo "== Cloud API running =="
echo "   Cloud API: http://$CLOUD_IP:8080"
echo "   TP-API:    http://$CLOUD_IP:8081"
echo "   Superadmin: iotteam@internal.local / iotteam2024"
echo "   Credentials saved to: $WORK_DIR/cloud-credentials.txt"
