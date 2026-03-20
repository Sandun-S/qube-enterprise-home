#!/usr/bin/env bash
# setup-cloud.sh — runs inside cloud-vm
set -euo pipefail

echo "==> Updating apt..."
sudo apt-get update -qq

echo "==> Installing dependencies..."
sudo apt-get install -y -qq curl git postgresql postgresql-contrib python3

echo "==> Installing Go 1.22..."
curl -fsSL https://go.dev/dl/go1.22.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /home/ubuntu/.bashrc
echo 'export PATH=$PATH:/usr/local/go/bin' >> /home/ubuntu/.profile
export PATH=$PATH:/usr/local/go/bin

echo "==> Configuring PostgreSQL..."
sudo systemctl start postgresql
sudo systemctl enable postgresql

sudo -u postgres psql <<'PSQL'
CREATE USER qubeadmin WITH PASSWORD 'qubepass';
CREATE DATABASE qubedb OWNER qubeadmin;
GRANT ALL PRIVILEGES ON DATABASE qubedb TO qubeadmin;
PSQL

# Allow local connections with password
PG_HBA=$(sudo -u postgres psql -t -P format=unaligned -c "SHOW hba_file;")
echo "host qubedb qubeadmin 0.0.0.0/0 md5" | sudo tee -a "$PG_HBA"
sudo sed -i "s/#listen_addresses = 'localhost'/listen_addresses = '*'/" \
    /etc/postgresql/*/main/postgresql.conf
sudo systemctl restart postgresql

echo "==> Running database migrations..."
PGPASSWORD=qubepass psql -h 127.0.0.1 -U qubeadmin -d qubedb \
    -f /home/ubuntu/qube-enterprise/cloud/migrations/001_init.sql
PGPASSWORD=qubepass psql -h 127.0.0.1 -U qubeadmin -d qubedb \
    -f /home/ubuntu/qube-enterprise/cloud/migrations/002_gateways_sensors.sql
PGPASSWORD=qubepass psql -h 127.0.0.1 -U qubeadmin -d qubedb \
    -f /home/ubuntu/qube-enterprise/cloud/migrations/003_device_catalog.sql

echo "==> Building Cloud API server..."
cd /home/ubuntu/qube-enterprise/cloud
go mod download
go build -o /usr/local/bin/qube-cloud ./cmd/server

echo "==> Creating systemd service for Cloud API..."
sudo tee /etc/systemd/system/qube-cloud.service > /dev/null <<'SERVICE'
[Unit]
Description=Qube Enterprise Cloud API
After=network.target postgresql.service

[Service]
User=ubuntu
Environment=DATABASE_URL=postgres://qubeadmin:qubepass@127.0.0.1:5432/qubedb?sslmode=disable
Environment=JWT_SECRET=dev-jwt-secret-change-in-production
ExecStart=/usr/local/bin/qube-cloud
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SERVICE

sudo systemctl daemon-reload
sudo systemctl enable qube-cloud
sudo systemctl start qube-cloud

echo "==> Serving Test UI on port 9000..."
sudo tee /etc/systemd/system/qube-ui.service > /dev/null <<'SERVICE'
[Unit]
Description=Qube Test UI
After=network.target

[Service]
User=ubuntu
WorkingDirectory=/home/ubuntu/qube-enterprise/test-ui
ExecStart=python3 -m http.server 9000
Restart=on-failure

[Install]
WantedBy=multi-user.target
SERVICE

sudo systemctl daemon-reload
sudo systemctl enable qube-ui
sudo systemctl start qube-ui

echo ""
echo "✓ cloud-vm provisioned."
echo "  Cloud API: http://$(hostname -I | awk '{print $1}'):8080"
echo "  TP-API:    http://$(hostname -I | awk '{print $1}'):8081"
echo "  Test UI:   http://$(hostname -I | awk '{print $1}'):9000"
