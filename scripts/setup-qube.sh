#!/usr/bin/env bash
# setup-qube.sh — runs inside qube-vm
# Usage: bash setup-qube.sh <CLOUD_IP>
set -euo pipefail

CLOUD_IP="${1:-192.168.64.10}"
TPAPI_URL="http://${CLOUD_IP}:8081"

echo "==> Cloud IP: $CLOUD_IP"
echo "==> TP-API URL: $TPAPI_URL"

echo "==> Updating apt..."
sudo apt-get update -qq

echo "==> Installing Go 1.22..."
curl -fsSL https://go.dev/dl/go1.22.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tar.gz
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> /home/ubuntu/.bashrc

echo "==> Installing Docker..."
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker ubuntu
sudo systemctl enable docker
sudo systemctl start docker

echo "==> Installing InfluxDB 2 via Docker..."
sudo docker run -d \
  --name influxdb \
  --restart unless-stopped \
  -p 8086:8086 \
  -e DOCKER_INFLUXDB_INIT_MODE=setup \
  -e DOCKER_INFLUXDB_INIT_USERNAME=qubeadmin \
  -e DOCKER_INFLUXDB_INIT_PASSWORD=qubepass123 \
  -e DOCKER_INFLUXDB_INIT_ORG=qube \
  -e DOCKER_INFLUXDB_INIT_BUCKET=sensors \
  -e DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=qube-influx-token-dev \
  influxdb:2

echo "==> Building Conf-Agent..."
cd /home/ubuntu/qube-enterprise/conf-agent
go mod tidy
go build -o /usr/local/bin/qube-conf-agent .

echo "==> Creating working directory for Conf-Agent..."
sudo mkdir -p /opt/qube/configs
sudo chown -R ubuntu:ubuntu /opt/qube

echo "==> Creating Conf-Agent env file (fill in QUBE_ID and QUBE_TOKEN after claiming)..."
cat > /opt/qube/.env <<ENV
TPAPI_URL=${TPAPI_URL}
QUBE_ID=Q-1001
QUBE_TOKEN=REPLACE_WITH_TOKEN_FROM_CLAIM
WORK_DIR=/opt/qube
POLL_INTERVAL=30
ENV

echo "==> Creating systemd service for Conf-Agent..."
sudo tee /etc/systemd/system/qube-agent.service > /dev/null <<'SERVICE'
[Unit]
Description=Qube Enterprise Conf-Agent
After=network.target docker.service

[Service]
User=ubuntu
EnvironmentFile=/opt/qube/.env
ExecStart=/usr/local/bin/qube-conf-agent
Restart=on-failure
RestartSec=10s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SERVICE

sudo systemctl daemon-reload
# NOTE: Don't start yet — need to set QUBE_TOKEN first after claiming

echo ""
echo "✓ qube-vm provisioned."
echo ""
echo "  Next steps:"
echo "  1. Claim a Qube via the Test UI or API (you'll get a token)"
echo "  2. Edit /opt/qube/.env — set QUBE_ID and QUBE_TOKEN"
echo "  3. sudo systemctl start qube-agent"
echo "  4. Watch logs: sudo journalctl -fu qube-agent"
