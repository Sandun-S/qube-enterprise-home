# Azure VM Setup — Qube Enterprise v2

VM: `qubevm` (20.197.15.165, port 2002, user azureuser)
Registry: GitHub (`ghcr.io/sandun-s/qube-enterprise-home`)

**Cloud VM runs:** PostgreSQL (management + telemetry), cloud-api, test-ui
**Does NOT run:** InfluxDB (that lives on the Qube edge device)

---

## Step 1 — Create directories on the VM

```bash
ssh qubevm "sudo mkdir -p /opt/qube-enterprise/migrations /opt/qube-enterprise/migrations-telemetry && sudo chown -R azureuser:azureuser /opt/qube-enterprise"
```

---

## Step 2 — Copy migration files from local machine (run from repo root)

```bash
rsync -av cloud/migrations/ qubevm:/opt/qube-enterprise/migrations/
rsync -av cloud/migrations-telemetry/ qubevm:/opt/qube-enterprise/migrations-telemetry/
rsync -av scripts/setup-cloud.sh qubevm:~/
```

> If rsync is unavailable, use scp with explicit port:
> ```bash
> scp -P 2002 cloud/migrations/*.sql azureuser@20.197.15.165:/opt/qube-enterprise/migrations/
> scp -P 2002 cloud/migrations-telemetry/*.sql azureuser@20.197.15.165:/opt/qube-enterprise/migrations-telemetry/
> scp -P 2002 scripts/setup-cloud.sh azureuser@20.197.15.165:~/
> ```

---

## Step 3 — Run setup script on the VM

```bash
ssh qubevm
chmod +x setup-cloud.sh
sudo ./setup-cloud.sh
```

This generates `/opt/qube-enterprise/docker-compose.yml` and starts all services.

When done, verify:
```bash
curl http://localhost:8080/health | jq .
cat /opt/qube-enterprise/cloud-credentials.txt
```

---

## Step 4 — Open Azure firewall ports

In Azure Portal → VM → **Networking** → Add inbound port rules:

| Port | Purpose |
|------|---------|
| `8080` | Cloud API + WebSocket (frontend + Qubes) |
| `8081` | TP-API (Qubes only) |
| `8888` | Test UI |

Do **not** expose port `5432` (Postgres).

Verify from local machine:
```bash
curl http://20.197.15.165:8080/health | jq .
```

Open test UI in browser: `http://20.197.15.165:8888`

---

## Step 5 — Log in to test UI

Open `http://20.197.15.165:8888` → **Superadmin** tab:
- Email: `iotteam@internal.local`
- Password: `iotteam2024`

---

## CI/CD — Automatic deploys

After setup, every push to `main` automatically:
1. Builds all images → pushes to GHCR
2. Runs API test suite in CI with a fresh postgres
3. **Copies all migration files to the VM via SCP**
4. **Runs all migrations against the live postgres container** (idempotent — safe to re-run every deploy)
5. Pulls new `cloud-api` and `test-ui` images → restarts containers

**This means: adding a new migration file (`004_xxx.sql`) and pushing to `main` is all you need to do.** The CI will apply it automatically on the next deploy.

No manual `rsync` or `psql` steps needed after initial setup.

Requires GitHub Actions secrets:
| Secret | Value |
|--------|-------|
| `CLOUD_VM_HOST` | `20.197.15.165` |
| `CLOUD_VM_SSH_PORT` | `2002` |
| `CLOUD_VM_USER` | `azureuser` |
| `CLOUD_VM_SSH_KEY` | Private SSH key |

And Actions variable:
| Variable | Value |
|----------|-------|
| `ENABLE_DEPLOY` | `true` |

---

## Summary

| Env var | What it controls |
|---------|-----------------|
| `CLOUD_API_IMAGE` | Docker image for cloud-api (default: GHCR amd64.latest) |
| `TEST_UI_IMAGE` | Docker image for test-ui (default: GHCR amd64.latest) |
| `JWT_SECRET` | Auto-generated if not set, saved to cloud-credentials.txt |

Default superadmin: `iotteam@internal.local` / `iotteam2024`

---

## Existing VM — Remove stale InfluxDB

If InfluxDB is running on the cloud VM from an older setup, remove it:

```bash
cd /opt/qube-enterprise
docker compose stop influxdb
docker compose rm -f influxdb
docker volume rm qube-enterprise_influxdb_data
```

Then remove the `influxdb` service block from `/opt/qube-enterprise/docker-compose.yml`
and the `influxdb_data` volume entry.
