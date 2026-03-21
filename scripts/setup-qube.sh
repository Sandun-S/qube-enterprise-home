#!/bin/bash
# setup-qube.sh — Qube Enterprise Agent Setup
# Works for Multipass VM test and real Qube device.
# On real Qube called from local.sh. On Multipass run manually.
# Usage: sudo ./setup-qube.sh --cloud-ip 192.168.1.50
set -e

CLOUD_IP=""
WORK_DIR="/opt/qube"
REGISTRY="registry.gitlab.com/iot-team4/product"
AGENT_IMAGE="${REGISTRY}/enterprise-conf-agent:arm64.latest"
POLL_INTERVAL="${POLL_INTERVAL:-30}"

while [[ $# -gt 0 ]]; do
  case $1 in
    --cloud-ip) CLOUD_IP="$2"; shift 2 ;;
    *) echo "Unknown: $1"; exit 1 ;;
  esac
done

[ -z "$CLOUD_IP" ] && { echo "Usage: ./setup-qube.sh --cloud-ip <ip>"; exit 1; }

TPAPI_URL="http://$CLOUD_IP:8081"
echo "== Qube Enterprise Agent Setup =="
echo "   TP-API: $TPAPI_URL"

# Read /boot/mit.txt if present (real Qube)
QUBE_ID=""; REGISTER_KEY=""
if [ -f /boot/mit.txt ]; then
  QUBE_ID=$(grep "^deviceid:" /boot/mit.txt | awk '{print $2}')
  REGISTER_KEY=$(grep "^register:" /boot/mit.txt | awk '{print $2}')
  echo "   Device: $QUBE_ID  Key: $REGISTER_KEY"
fi

# Create directories
mkdir -p "$WORK_DIR/configs"

# Write .env
cat > "$WORK_DIR/.env" << ENV
TPAPI_URL=$TPAPI_URL
WORK_DIR=$WORK_DIR
POLL_INTERVAL=$POLL_INTERVAL
MIT_TXT_PATH=/boot/mit.txt
ENV

# Docker (skip if already installed)
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

# Swarm + qube-net (skip if already done by Qube Lite local.sh)
docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null | grep -q "active" || docker swarm init
docker network ls | grep -q "qube-net" || \
  docker network create --attachable --driver overlay qube-net

# Write systemd service
cat > /etc/systemd/system/enterprise-conf-agent.service << SERVICE
[Unit]
Description=Qube Enterprise Conf-Agent
After=network-online.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
WorkingDirectory=$WORK_DIR
EnvironmentFile=-$WORK_DIR/.env
ExecStart=/usr/local/bin/enterprise-conf-agent
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=enterprise-conf-agent

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable enterprise-conf-agent.service

# Try to start (will wait for claim if binary exists)
if [ -f /usr/local/bin/enterprise-conf-agent ]; then
  systemctl restart enterprise-conf-agent.service
  echo "== Agent started — logs: journalctl -u enterprise-conf-agent -f =="
else
  echo "== WARNING: /usr/local/bin/enterprise-conf-agent not found =="
  echo "   Copy the binary from build artifacts or pull from registry"
  echo "   Then: systemctl start enterprise-conf-agent"
fi

[ -n "$REGISTER_KEY" ] && echo "   Claim in portal with key: $REGISTER_KEY"
