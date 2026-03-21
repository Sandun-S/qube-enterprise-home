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

---

## 7. GitHub CI/CD setup (testing with single repo)

Your single GitHub repo `github.com/Sandun-S/qube-enterprise-home` holds all services. GitHub Actions builds each service as a separate image under the same repo path:

```
ghcr.io/sandun-s/qube-enterprise-home/cloud-api:amd64.latest
ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest
ghcr.io/sandun-s/qube-enterprise-home/influx-to-sql:arm64.latest
ghcr.io/sandun-s/qube-enterprise-home/mqtt-gateway:arm64.latest
```

**Important:** GHCR image names must be **all lowercase**. `github.repository` returns `Sandun-S/qube-enterprise-home` so the workflow lowercases it automatically with `tr`.

### Step 1 — Push code to your repo

```bash
git clone https://github.com/Sandun-S/qube-enterprise-home
cd qube-enterprise-home
# copy all project files in
git add . && git commit -m "initial" && git push origin main
```

GitHub Actions starts automatically on push. Check the **Actions** tab.

### Step 2 — Make packages public

After the first successful build, go to:
`github.com/Sandun-S?tab=packages`

For each package → **Package settings** → **Change visibility** → **Public**

This lets Qubes pull images without authentication. Or keep private and set up pull credentials in `/opt/qube/.env`.

### Step 3 — Set QUBE_IMAGE_REGISTRY in cloud-api

In `docker-compose.dev.yml` (already set) and on your Azure VM's docker-compose:
```yaml
QUBE_IMAGE_REGISTRY: "ghcr.io/sandun-s/qube-enterprise-home"
```

The cloud-api uses this when generating `docker-compose.yml` for Qubes. Qubes then pull from this registry.

---

## 8. GitLab production (separate repos per service)

When moving to the IoT team's GitLab, each service gets its own repo:

```
registry.gitlab.com/iot-team4/product/enterprise-cloud-api:amd64.latest
registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest
registry.gitlab.com/iot-team4/product/enterprise-influx-to-sql:arm64.latest
registry.gitlab.com/iot-team4/product/mqtt-gateway:arm64.latest
```

Set on cloud VM:
```bash
export QUBE_IMAGE_REGISTRY=registry.gitlab.com/iot-team4/product
```

The `modbus-gateway`, `opc-ua-gateway`, `snmp-gateway` already exist in GitLab — they are reused as-is. Only the 4 Enterprise images are new.

---

## 9. Azure VM testing

### Create Azure VM (free student credits)

```bash
# Ubuntu 22.04, Standard_B2s (2 vCPU, 4GB RAM), open ports 8080 and 8081
az vm create \
  --resource-group qube-test-rg \
  --name cloud-vm \
  --image Ubuntu2204 \
  --size Standard_B2s \
  --admin-username azureuser \
  --generate-ssh-keys

# Open ports
az vm open-port --resource-group qube-test-rg --name cloud-vm --port 8080
az vm open-port --resource-group qube-test-rg --name cloud-vm --port 8081
```

### Deploy cloud-api to Azure VM

```bash
# Get VM IP
AZURE_IP=$(az vm show -d -g qube-test-rg -n cloud-vm --query publicIps -o tsv)

# SSH in and set up
ssh azureuser@$AZURE_IP

# On the VM:
git clone https://github.com/Sandun-S/qube-enterprise-home
cd qube-enterprise-home
cp scripts/cloud-vm-compose.yml docker-compose.yml
mkdir -p migrations
cp cloud/migrations/*.sql migrations/
export DB_PASS=yourpassword
export JWT_SECRET=$(openssl rand -hex 32)

# Login to ghcr.io (needed if packages are private)
echo "<github-token>" | docker login ghcr.io -u Sandun-S --password-stdin

docker compose up -d

# Test
curl http://localhost:8080/health
```

### GitHub Secrets for auto-deploy

Add in **Settings → Secrets → Actions**:

| Secret | Value |
|---|---|
| `CLOUD_VM_HOST` | Azure VM public IP |
| `CLOUD_VM_USER` | `azureuser` |
| `CLOUD_VM_SSH_KEY` | Contents of `~/.ssh/id_rsa` (private key) |

After this, every push to main:
1. Builds images → pushes to ghcr.io
2. Runs API test suite
3. SSHs into Azure VM → pulls new image → restarts cloud-api

### Run API tests against Azure VM

```bash
./test/test_api.sh http://$AZURE_IP:8080
```

---

## 10. Summary — GitHub testing vs GitLab production

| | GitHub (testing) | GitLab (production) |
|---|---|---|
| Repo structure | Single repo, all services | Separate repo per service |
| Registry | `ghcr.io/sandun-s/qube-enterprise-home/` | `registry.gitlab.com/iot-team4/product/` |
| Image name | `...home/cloud-api:amd64.latest` | `.../enterprise-cloud-api:amd64.latest` |
| `QUBE_IMAGE_REGISTRY` | `ghcr.io/sandun-s/qube-enterprise-home` | `registry.gitlab.com/iot-team4/product` |
| Cloud deploy | SSH to Azure VM | SSH to production cloud VM |
| Qube images | Pulled from ghcr.io | Pulled from GitLab registry |

The **only thing that changes** between GitHub test and GitLab production is the `QUBE_IMAGE_REGISTRY` environment variable on the cloud-api. Everything else is identical.
