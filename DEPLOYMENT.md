# Qube Enterprise — Deployment Guide

## Architecture recap

```
Factory image flash
  → /boot/mit.txt written (deviceid, register_key)
  → write-to-database.sh inserts into both:
      Qube Lite MySQL  (conf-api DB)        ← existing unchanged
      Enterprise Postgres (qubedb)          ← new 12-line addition

Device boots:
  local-inst.sh  → installs Docker, reboots
  initramfs.sh   → sets up encryption, reboots
  local.sh       → docker swarm init
                 → docker network create qube-net
                 → systemctl enable conf-agent (Qube Lite)
                 → systemctl enable enterprise-conf-agent (Enterprise NEW)

Enterprise conf-agent starts:
  → reads /boot/mit.txt
  → polls TP-API /v1/device/register every 60s
  → once claimed: gets QUBE_TOKEN, saves to /opt/qube/.env
  → begins 30s hash-sync: downloads compose + CSV files
  → docker stack deploy → gateway containers start
  → enterprise-influx-to-sql reads InfluxDB → Postgres via TP-API


  local.sh — add 5 lines after systemctl enable conf-agent.service:
  mkdir -p /opt/qube
  echo "TPAPI_URL=http://CLOUD_IP:8081" > /opt/qube/.env
  echo "WORK_DIR=/opt/qube" >> /opt/qube/.env
  echo "POLL_INTERVAL=30" >> /opt/qube/.env
  echo "MIT_TXT_PATH=/boot/mit.txt" >> /opt/qube/.env
  cp /mit-ro/network/enterprise-conf-agent.service /etc/systemd/system/
  systemctl enable enterprise-conf-agent.service
```

---

## No conflicts with Qube Lite

| Thing | Qube Lite | Enterprise |
|---|---|---|
| Work directory | `/mit/qube/` | `/opt/qube/` |
| conf-agent service | `conf-agent.service` | `enterprise-conf-agent.service` |
| Stack name | `qube` (existing services) | adds new services to same `qube` stack |
| core-switch | unchanged, same container | Enterprise gateways POST to the same core-switch |
| InfluxDB | same influxdb container | enterprise-influx-to-sql reads from it |
| Networks | `qube-net` overlay | same `qube-net` overlay |

The only shared resource is the Docker Swarm stack named `qube`. Enterprise conf-agent runs
`docker stack deploy -c /opt/qube/docker-compose.yml qube` which **adds** new gateway
services to the existing stack without touching the Qube Lite services.

---

## 1. Build and push images (GitHub Actions / GitLab CI)

Using GitHub Container Registry (ghcr.io):

```bash
# In your GitHub repo settings:
# Settings → Secrets → Actions → add:
#   CLOUD_VM_HOST     = your cloud VM IP
#   CLOUD_VM_USER     = ubuntu (or your SSH user)
#   CLOUD_VM_SSH_KEY  = your private SSH key

# Push to main branch triggers:
#   cloud-api:amd64.latest          → ghcr.io/<org>/enterprise-cloud-api:amd64.latest
#   enterprise-conf-agent:arm64.latest
#   enterprise-influx-to-sql:arm64.latest
#   mqtt-gateway:arm64.latest
```

The GitHub Actions workflow at `.github/workflows/build-push.yml` handles this.
ARM64 images are cross-compiled on the GitHub amd64 runner using QEMU — no ARM hardware needed for building.

If using GitLab registry, change `REGISTRY` in the workflow to:
```
registry.gitlab.com/iot-team4/product
```

---

## 2. Cloud VM setup (production)

On a fresh Ubuntu 22.04 VM:

```bash
# Copy files to VM
scp -r cloud/migrations ubuntu@<cloud-ip>:/opt/qube-enterprise/
scp scripts/setup-cloud.sh ubuntu@<cloud-ip>:/tmp/

# Run setup
ssh ubuntu@<cloud-ip>
sudo bash /tmp/setup-cloud.sh
```

Or deploy via CI/CD automatically on push to main.

**Firewall rules needed:**
- Port 8080 — Cloud API (frontend + IoT team access)
- Port 8081 — TP-API (Qube devices only — consider IP whitelisting)
- Port 5432 — Postgres (internal only)

---

## 3. Real Qube integration

### Change 1: write-to-database.sh (minimal — 12 lines at bottom)

The updated `scripts/write-to-database.sh` already includes the Enterprise Postgres insert.
Set these environment variables before running:

```bash
export ENTERPRISE_DB_HOST="cloud-vm-ip:5432"
export ENTERPRISE_DB_USER="qubeadmin"
export ENTERPRISE_DB_PASS="qubepass"
export ENTERPRISE_DB_NAME="qubedb"
```

The existing MySQL insert lines are completely unchanged.

### Change 2: Add enterprise-conf-agent to local.sh

Add these two lines near the end of `local.sh`, right after the existing:
```bash
systemctl enable conf-agent.service
```

Add:
```bash
# ── Qube Enterprise conf-agent ──────────────────────────────────
cp /mit-ro/network/enterprise-conf-agent.service /etc/systemd/system
systemctl enable enterprise-conf-agent.service
```

### Change 3: Add to image (env-setup files)

Add to the image:
- `/mit-ro/network/enterprise-conf-agent.service` — the systemd unit file
- `/usr/local/bin/enterprise-conf-agent` — the arm64 binary (built by CI/CD)

The binary is compiled for `linux/arm64`. Pull it from the registry or build with:
```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o enterprise-conf-agent ./conf-agent/
```

### Change 4: /opt/qube/.env (written by setup-qube.sh or manually)

The enterprise-conf-agent.service needs `TPAPI_URL` set before it starts.
This is written to `/opt/qube/.env` by `scripts/setup-qube.sh`.

On a real Qube, add this to `local.sh`:
```bash
# Set Enterprise TP-API URL
mkdir -p /opt/qube
echo "TPAPI_URL=http://<cloud-ip>:8081" > /opt/qube/.env
echo "WORK_DIR=/opt/qube" >> /opt/qube/.env
echo "POLL_INTERVAL=30" >> /opt/qube/.env
echo "MIT_TXT_PATH=/boot/mit.txt" >> /opt/qube/.env
```

Or better — bake the TPAPI_URL into the image during the flash process so it's always set.

---

## 4. Multipass VM test

Test the full flow without real hardware:

```bash
chmod +x scripts/test-multipass.sh
./scripts/test-multipass.sh
```

This creates two VMs, builds everything locally, runs the full flow, and verifies:
- Device self-registration (pending → claimed)
- Config sync (docker-compose.yml + CSV files appear on qube-vm)
- Heartbeat keeps qube online

For manual Multipass test:

```bash
# Launch VMs
multipass launch --name cloud-vm --cpus 2 --memory 2G 22.04
multipass launch --name qube-vm  --cpus 1 --memory 1G 22.04

CLOUD_IP=$(multipass info cloud-vm | grep IPv4 | awk '{print $2}')
QUBE_IP=$(multipass  info qube-vm  | grep IPv4 | awk '{print $2}')

# Setup cloud
multipass exec cloud-vm -- bash -c "..."   # run setup-cloud.sh

# Setup qube
multipass exec qube-vm -- sudo bash /tmp/setup-qube.sh --cloud-ip $CLOUD_IP

# Watch agent
multipass exec qube-vm -- journalctl -u enterprise-conf-agent -f

# Run full API test against the cloud VM
./test/test_api.sh http://$CLOUD_IP:8080

# Cleanup
multipass delete cloud-vm qube-vm && multipass purge
```

---

## 5. Boot sequence comparison

### Qube Lite (unchanged)
```
Flash → local-inst.sh (Docker install) → reboot
      → initramfs.sh (encryption setup) → reboot  
      → local.sh:
          docker swarm init
          docker network create qube-net
          systemctl enable conf-agent   ← Qube Lite agent
          → conf-agent starts Qube Lite stack (influxdb, core-switch, etc.)
```

### Enterprise additions to local.sh
```
local.sh (same as above, plus):
    mkdir -p /opt/qube
    echo "TPAPI_URL=http://<cloud>:8081" > /opt/qube/.env
    cp enterprise-conf-agent.service /etc/systemd/system/
    systemctl enable enterprise-conf-agent   ← Enterprise agent starts
    → polls /v1/device/register until customer claims
    → downloads compose + CSVs
    → docker stack deploy → gateway containers join existing qube stack
```

---

## 6. Summary of files to add to Qube image

| File | Destination | Source |
|---|---|---|
| `enterprise-conf-agent` binary | `/usr/local/bin/enterprise-conf-agent` | Built by CI/CD (arm64) |
| `enterprise-conf-agent.service` | `/mit-ro/network/` | `scripts/enterprise-conf-agent.service` |

### Additions to existing scripts

**`write-to-database.sh`** — 12 lines at bottom (already in `scripts/write-to-database.sh`)

**`local.sh`** — add after `systemctl enable conf-agent.service`:
```bash
mkdir -p /opt/qube
cat > /opt/qube/.env << ENV
TPAPI_URL=http://CLOUD_IP:8081
WORK_DIR=/opt/qube
POLL_INTERVAL=30
MIT_TXT_PATH=/boot/mit.txt
ENV
cp /mit-ro/network/enterprise-conf-agent.service /etc/systemd/system/
systemctl enable enterprise-conf-agent.service
```

Replace `CLOUD_IP` with your actual cloud server IP before baking the image.
