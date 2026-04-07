# Azure VM Setup — Qube Enterprise v2

VM: `qubevm` (20.197.15.165, port 2002, user azureuser)
Registry: GitHub (`ghcr.io/sandun-s/qube-enterprise-home`)

---

## Step 1 — Create directories on the VM

```bash
ssh qubevm "sudo mkdir -p /opt/qube-enterprise/migrations /opt/qube-enterprise/migrations-telemetry && sudo chown -R azureuser:azureuser /opt/qube-enterprise"
```

---

## Step 2 — Copy files from local machine (run from repo root)

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

`CLOUD_API_IMAGE` = the image used to run cloud-api **on this VM**.

```bash
ssh qubevm
chmod +x setup-cloud.sh
sudo CLOUD_API_IMAGE=ghcr.io/sandun-s/qube-enterprise-home/cloud-api:amd64.latest \
     ./setup-cloud.sh
```

When done, check it's up:
```bash
curl http://localhost:8080/health | jq .
cat /opt/qube-enterprise/cloud-credentials.txt   # JWT_SECRET saved here
```

---

## Step 4 — Update reader container registry to GitHub (via API)

`QUBE_IMAGE_REGISTRY` = where cloud-api tells **Qubes** to pull reader containers from.
The setup script defaults this to GitLab — update it to GitHub here.

```bash
# Get superadmin token
SA_TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"iotteam@internal.local","password":"iotteam2024"}' \
  | jq -r '.token')

echo $SA_TOKEN   # should not be empty

# Switch registry to GitHub
curl -X PUT http://localhost:8080/api/v1/admin/registry \
  -H "Authorization: Bearer $SA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}'
```

---

## Step 5 — Open Azure firewall ports

In Azure Portal → VM → **Networking** → Add inbound port rules:
- `8080` — Cloud API + WebSocket (frontend + Qubes)
- `8081` — TP-API (Qubes only)

Do **not** expose port `5432` (Postgres).

Verify from local machine:
```bash
curl http://20.197.15.165:8080/health | jq .
```

---

## Summary

| Env var | What it controls |
|---|---|
| `CLOUD_API_IMAGE` | Docker image for cloud-api on this VM |
| `QUBE_IMAGE_REGISTRY` | Registry Qubes use to pull reader containers (set via API) |

Default superadmin: `iotteam@internal.local` / `iotteam2024`
