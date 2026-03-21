# 10 — CI/CD: Building Images and Deploying

---

## Why Docker images instead of copying binaries

Old way: build a binary → copy to device → restart service
New way: build a Docker image → push to registry → device pulls automatically

The device never needs you to copy anything. It pulls from the registry when `docker stack deploy` runs. If you push a new image with the same tag (`arm64.latest`), the next time conf-agent redeploys, Docker pulls the new image automatically.

---

## The four images we build

| Image | Platform | Who uses it |
|---|---|---|
| `cloud-api` | amd64 | Cloud VM (Azure, any Linux server) |
| `conf-agent` | arm64 | Every Qube — always running |
| `influx-to-sql` | arm64 | Every Qube — always running |
| `mqtt-gateway` | arm64 | Qubes with MQTT gateways |

`modbus-gateway`, `opc-ua-gateway`, `snmp-gateway` already exist in the IoT team's GitLab registry — we reuse them.

---

## The Dockerfile pattern (all four use the same pattern)

```dockerfile
# Stage 1: Build — uses full Go toolchain (~300MB)
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o cloud-api ./cmd/server

# Stage 2: Run — uses tiny Alpine (~5MB)
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/cloud-api .
CMD ["./cloud-api"]
```

Multi-stage build: stage 1 compiles, stage 2 only contains the binary. Final image is ~15MB instead of ~350MB. The `CGO_ENABLED=0` flag produces a statically linked binary — no C library dependencies, works in any Linux container.

For arm64 (conf-agent, etc.): CI/CD passes `--platform linux/arm64` to `docker build`. QEMU emulation runs on the amd64 GitHub runner to cross-compile.

---

## GitHub Actions workflow explained

```yaml
# .github/workflows/build-push.yml

env:
  REGISTRY: ghcr.io

jobs:
  cloud-api:
    runs-on: ubuntu-latest     # GitHub's free amd64 Linux runner
    steps:
      # Checkout code
      - uses: actions/checkout@v4

      # IMPORTANT: GHCR requires lowercase repository names
      # github.repository = "Sandun-S/qube-enterprise-home" (has capital S)
      # Docker tags must be lowercase
      - name: Lowercase repo name
        run: echo "REPO=$(echo '${{ github.repository }}' | tr '[:upper:]' '[:lower:]')" >> $GITHUB_ENV
        # Result: REPO=sandun-s/qube-enterprise-home

      # Login to GitHub Container Registry
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}         # your GitHub username
          password: ${{ secrets.GITHUB_TOKEN }} # auto-provided by GitHub Actions

      # Build and push (only pushes on main branch, not on PRs)
      - uses: docker/build-push-action@v5
        with:
          context:   ./cloud          # build context = ./cloud directory
          platforms: linux/amd64
          push:      ${{ github.ref == 'refs/heads/main' }}
          tags: |
            ghcr.io/${{ env.REPO }}/cloud-api:amd64.latest
```

`secrets.GITHUB_TOKEN` is automatically available in every Actions workflow — you don't create it. It has write access to your own repo's packages (ghcr.io).

---

## ARM64 cross-compilation

Building arm64 on an amd64 machine requires QEMU (an emulator):

```yaml
arm64-images:
  runs-on: ubuntu-latest    # amd64 machine
  strategy:
    matrix:
      include:
        - name: conf-agent
          context: ./conf-agent
        - name: influx-to-sql
          context: ./enterprise-influx-to-sql
        - name: mqtt-gateway
          context: ./mqtt-gateway
  steps:
    # Install QEMU — lets amd64 runner execute arm64 instructions
    - uses: docker/setup-qemu-action@v3
      with:
        platforms: arm64

    # Build with arm64 platform
    - uses: docker/build-push-action@v5
      with:
        platforms: linux/arm64   # ← cross-compile for arm64
        tags: |
          ghcr.io/${{ env.REPO }}/${{ matrix.name }}:arm64.latest
```

Cross-compilation is slow (5-15 minutes) because QEMU emulates the CPU. For faster builds, you'd use a native arm64 runner or Go's native cross-compilation (`GOARCH=arm64 go build`). The Dockerfile currently uses QEMU, but could be changed to use Go's native cross-compilation for much faster builds.

---

## Image registry differences: GitHub vs GitLab

The same codebase deploys to two different registries:

```
GitHub (testing):
  One repo, all services under it:
  ghcr.io/sandun-s/qube-enterprise-home/cloud-api:amd64.latest
  ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest
  ghcr.io/sandun-s/qube-enterprise-home/influx-to-sql:arm64.latest
  ghcr.io/sandun-s/qube-enterprise-home/mqtt-gateway:arm64.latest

GitLab (production):
  Separate repo per service (existing IoT team structure):
  registry.gitlab.com/iot-team4/product/enterprise-cloud-api:amd64.latest
  registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest
  registry.gitlab.com/iot-team4/product/enterprise-influx-to-sql:arm64.latest
  registry.gitlab.com/iot-team4/product/mqtt-gateway:arm64.latest
```

The cloud-api uses `QUBE_IMAGE_REGISTRY` and per-image env vars to know which registry to put in the generated `docker-compose.yml` that gets sent to Qubes.

For GitHub testing:
```yaml
QUBE_IMAGE_REGISTRY: "ghcr.io/sandun-s/qube-enterprise-home"
# → generated compose uses: ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest
```

For GitLab production:
```yaml
QUBE_IMAGE_REGISTRY: "registry.gitlab.com/iot-team4/product"
QUBE_IMG_CONF_AGENT: "registry.gitlab.com/iot-team4/product/enterprise-conf-agent:arm64.latest"
QUBE_IMG_INFLUX_SQL: "registry.gitlab.com/iot-team4/product/enterprise-influx-to-sql:arm64.latest"
# Per-image overrides needed because GitLab uses "enterprise-" prefix
```

---

## How to make packages public on GitHub

After the first successful build, images appear in your GitHub packages:
`github.com/Sandun-S?tab=packages`

Each package → **Package settings** → **Change visibility** → **Public**

Public means Qubes can `docker pull ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest` without authentication. Keep the cloud-api private if you want.

---

## Deploying to Azure VM

The deploy job SSHs into the VM and runs:
```bash
docker login ghcr.io -u ${{ github.actor }} --password-stdin
docker compose pull cloud-api   # pull new image
docker compose up -d cloud-api  # restart with new image
```

For this to work, add three secrets in **GitHub Settings → Secrets → Actions**:
- `CLOUD_VM_HOST` — IP address of your Azure VM
- `CLOUD_VM_USER` — SSH username (usually `azureuser`)
- `CLOUD_VM_SSH_KEY` — content of your private SSH key (`~/.ssh/id_rsa`)

---

## Local build without CI/CD

To build and test locally:

```bash
# Build cloud-api (your machine's platform)
cd cloud
docker build -t cloud-api:local .

# Build conf-agent for arm64 (cross-compile)
cd ../conf-agent
docker build --platform linux/arm64 -t conf-agent:arm64.local .

# Or build Go binary directly (faster, no Docker needed)
cd conf-agent
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o enterprise-conf-agent .
# Produces a binary you can scp to a Raspberry Pi and run directly
```
