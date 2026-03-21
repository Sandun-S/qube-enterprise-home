# 09 — Docker Swarm: Why Qube Uses It

---

## Docker Compose vs Docker Swarm

You might wonder: why not just use `docker compose up`? Here's the difference:

| | Docker Compose | Docker Swarm |
|---|---|---|
| Command | `docker compose up -d` | `docker stack deploy -c file.yml qube` |
| Service names | `container-name` | `stackname_service-name` (e.g. `qube_panel-a`) |
| Restart on failure | depends on `restart:` policy | always, built into `deploy:` |
| Logs | `docker compose logs panel-a` | `docker service logs qube_panel-a` |
| Restart a service | `docker compose restart panel-a` | `docker service update --force qube_panel-a` |
| Scale | limited | `docker service scale qube_panel-a=3` |
| Requires swarm init | no | yes |

Qube uses Swarm because it's already set up (by `local.sh` during flashing) and `docker service update --force` is the correct way to restart a service — it re-pulls the image if needed, handles rollback if the new version fails, and doesn't cause downtime on multi-node setups.

---

## Swarm setup (already done by local.sh)

```bash
# Done once during Qube flashing (local.sh)
docker swarm init
docker network create --attachable --driver overlay qube-net

# Never needs to be done again, even after reboots
# Docker persists swarm state in /var/lib/docker/swarm/
```

The `qube-net` network is an overlay network — it spans across multiple Swarm nodes. Even though Qube is always single-node, using overlay means containers on different services can find each other by hostname (`http://core-switch:8080`, `http://influxdb:8086`).

---

## Stack deploy

```bash
docker stack deploy -c docker-compose.yml qube
```

- `qube` is the stack name — all services get prefixed: `qube_panel-a`, `qube_influxdb`, etc.
- Compose file must use `deploy:` blocks (not `restart:` — that's Compose syntax)
- First deploy: creates services. Subsequent deploys: updates changed services only.
- `--with-registry-auth`: passes GitLab/GitHub credentials to Docker so it can pull private images

---

## The generated docker-compose.yml

When conf-agent syncs, it writes this file to `/opt/qube/docker-compose.yml`:

```yaml
version: "3.8"

networks:
  qube-net:
    external: true    # use the pre-existing overlay network

services:
  enterprise-conf-agent:
    image: ghcr.io/sandun-s/qube-enterprise-home/conf-agent:arm64.latest
    networks:
      - qube-net
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock  # must mount to run docker commands
      - /opt/qube:/opt/qube
    deploy:
      replicas: 1
      restart_policy:
        condition: any   # restart on any failure

  panel-a:                 # ← gateway name from API (sanitized)
    image: ghcr.io/.../modbus-gateway:arm64.latest
    networks:
      - qube-net
    volumes:
      - /opt/qube/configs/panel-a/configs.yml:/app/configs.yml:ro
      - /opt/qube/configs/panel-a/config.csv:/app/config.csv:ro
    deploy:
      replicas: 1
      restart_policy:
        condition: any
```

After `docker stack deploy`, this becomes:
```
$ docker service ls
qube_enterprise-conf-agent    1/1    ghcr.io/.../conf-agent
qube_panel-a                  1/1    .../modbus-gateway
qube_influxdb                 1/1    influxdb:1.8
qube_core-switch              1/1    .../core-switch
```

---

## Adding a second Modbus gateway

When you add a second gateway named `Panel_B` via the API, the next sync produces:

```yaml
  panel-a:
    image: .../modbus-gateway:arm64.latest
    volumes:
      - /opt/qube/configs/panel-a/configs.yml:/app/configs.yml:ro

  panel-b:                 # ← new service
    image: .../modbus-gateway:arm64.latest
    volumes:
      - /opt/qube/configs/panel-b/configs.yml:/app/configs.yml:ro  # different config
      - /opt/qube/configs/panel-b/config.csv:/app/config.csv:ro
```

Same Docker image, different config files. Both containers run independently, each polling a different Modbus device. Docker Swarm starts `qube_panel-b` without touching `qube_panel-a`.

---

## How conf-agent restarts a gateway

```go
// When receiving "restart_service" command with service="panel-a":
swarmOut, _ := exec.Command("docker", "info",
    "--format", "{{.Swarm.LocalNodeState}}").Output()
isSwarm := strings.TrimSpace(string(swarmOut)) == "active"

if isSwarm {
    // Forces the service to restart even if nothing changed
    exec.Command("docker", "service", "update", "--force", "qube_panel-a").Run()
} else {
    // Dev mode
    exec.Command("docker", "compose", "restart", "panel-a").Run()
}
```

`docker service update --force` causes Swarm to stop the current container and start a new one. It respects the `restart_policy` — if the new container fails immediately, Swarm retries. If it keeps failing, Swarm backs off (exponential backoff).

---

## Persisting data across restarts

Qube Lite uses bind mounts to local directories:
```yaml
volumes:
  - ./influxdb/Qube-1302:/var/lib/influxdb  # host path → container path
```

This is why InfluxDB data survives container restarts — it's on the Qube's filesystem, not inside the container. Enterprise uses the same pattern:
```yaml
volumes:
  - /opt/qube/configs/panel-a/config.csv:/app/config.csv:ro
```

The `:ro` means read-only — the container can't modify the file, only read it. This prevents a buggy gateway from corrupting its own config.

---

## Why `version: "3.8"` in the compose file

Swarm requires the v3 compose format. v3 supports `deploy:` blocks. v2 doesn't. The `version:` key tells Docker which format to parse.

Note: Docker Compose v2 (the modern standalone tool) shows a warning "version attribute is obsolete" — this is just a warning, not an error. The file still works. The warning appears because modern Compose doesn't need the version key, but Swarm still does.

---

## Checking service health on a Qube

```bash
# List all running services
docker service ls

# See logs for enterprise conf-agent
journalctl -u enterprise-conf-agent -f

# See logs for a gateway container
docker service logs qube_panel-a -f

# See what files conf-agent wrote
ls -la /opt/qube/configs/panel-a/
cat /opt/qube/configs/panel-a/config.csv
cat /opt/qube/configs/panel-a/configs.yml

# Check sensor_map
cat /opt/qube/sensor_map.json

# See docker-compose.yml that was deployed
cat /opt/qube/docker-compose.yml

# Manually force a config resync (clears local hash)
echo "" > /opt/qube/.config_hash
# → conf-agent will detect hash mismatch on next poll and re-sync
```
