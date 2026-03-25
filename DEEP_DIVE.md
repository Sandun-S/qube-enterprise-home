# Qube Enterprise: Deep Dive Architecture & Code Workflows

This document provides a highly technical, deep-level explanation of the core workflows in the Qube Enterprise system. It details the exact files, functions, and logic used for device provisioning, authentication, configuration synchronization, CSV/Docker Compose generation, and telemetry ingestion.

---

## 1. Authentication & Token Lifecycle

The system utilizes two distinct authentication mechanisms running on different ports:

### User Login & Cloud API Auth (JWT, Port 8080)
The Cloud Management API is designed for human users (frontend).
*   **Logic Location:** `cloud/internal/api/auth.go` (`loginHandler`, `registerHandler`)
*   **Authentication:** Uses PostgreSQL's built-in `crypt()` function with the Blowfish cipher to verify passwords, ensuring compatibility with Go's `bcrypt`.
*   **Token Generation:** `makeJWT(secret, userID, orgID, role)` generates a JSON Web Token signed via HS256 (`JWT_SECRET`).
*   **Payload:** Includes `user_id`, `org_id`, `role` (superadmin, admin, editor, viewer), and an `exp` claim set to **24 hours**.
*   **Lifecycle:** The token expires daily. Clients must send it in the `Authorization: Bearer <token>` header. It's verified in `cloud/internal/api/middleware.go` by `jwtMiddleware` and checked against required roles by `requireRole`.

### Qube Device Auth & TP-API (HMAC, Port 8081)
Qubes communicate exclusively with the Telemetry/Provisioning API (TP-API). They do *not* use JWTs. They use a deterministic HMAC token that **does not expire**.
*   **Logic Location:** `cloud/internal/api/qubes.go` (`claimQubeHandler`) and `cloud/internal/tpapi/sync.go` (`deviceRegisterHandler`).

#### The Token Generation & Retrieval Flow
1.  **Factory Flash:** During manufacturing, `scripts/write-to-database.sh` generates a unique `device_id` (e.g., "Qube-1302") and a `register_key` (e.g., "4D4L-R4KY-ZTQ5"). These are written to the physical device at `/boot/mit.txt` and inserted into the enterprise Postgres database.
2.  **User Claims Device:** A user enters the `register_key` in the portal. The Cloud API (`claimQubeHandler`) links the device to their `org_id` and retrieves their `org_secret`.
    *   **Generation:** It computes the `QUBE_TOKEN` using: `hmac.New(sha256.New, []byte(orgSecret))`. The signed string is `qubeID + ":" + orgSecret`. This hash is saved to `qubes.auth_token_hash`.
3.  **Qube Self-Registration:** When the Qube boots, the `conf-agent` (`conf-agent/main.go`) reads `/boot/mit.txt`.
    *   If `/opt/qube/.env` doesn't have a `QUBE_TOKEN`, it calls `POST /v1/device/register` on the TP-API (unauthenticated).
    *   It sends its `device_id` and `register_key`.
    *   The TP-API (`deviceRegisterHandler`) checks if the device is claimed. If so, it computes the *exact same* HMAC token using the `org_secret` and returns it to the Qube.
4.  **Persistence:** The `conf-agent` saves this token to `/opt/qube/.env` (`QUBE_TOKEN=<token>`). It never changes unless the device is re-claimed.
5.  **Verification:** Every subsequent TP-API call passes through `qubeAuthMiddleware` (`cloud/internal/tpapi/router.go`), which dynamically recomputes the HMAC using the `X-Qube-ID` header and verifies it against the `Authorization` header.

---

## 2. Configuration Hashing & State Sync

The system uses a declarative "state-reconciliation" model, driven by configuration hashes.

### Hash Generation (Cloud Side)
Whenever an entity affecting a Qube's configuration is modified (gateway added, sensor updated, template patched), the hash is recomputed.
*   **Logic Location:** `cloud/internal/api/hash.go` (`recomputeConfigHash`)
*   **The Process:**
    1.  It queries the database for all active `gateways` belonging to the `qube_id`.
    2.  For each gateway, it fetches its active `sensors`.
    3.  It builds a canonical Go struct containing this nested data:
        ```go
        map[string]any{
            "qube_id":  qubeID,
            "gateways": []gwRow{...}, // Includes protocol, host, config_json, service_image, and sensors
        }
        ```
    4.  It serializes this struct to JSON, computes a SHA-256 hash (`sha256.Sum256`), and encodes it as a hex string.
    5.  It updates the `config_state` table with this new `hash` and the raw `config_snapshot` JSON.

### Polling & Syncing (Qube Side)
The `conf-agent` (`conf-agent/main.go`) runs a continuous loop (`runCycle`) every `POLL_INTERVAL` (default 30s).
1.  **State Check:** It calls `GET /v1/sync/state` and compares the cloud `hash` with its local hash (`/opt/qube/.config_hash`).
2.  **Config Download:** If there's a mismatch, it calls `GET /v1/sync/config`.
3.  **Application:** The `applyConfig` function writes the payload to disk (Docker Compose YAML, CSVs, Env files) and triggers a deployment.

---

## 3. Dynamic CSV and Docker Compose Generation

The `GET /v1/sync/config` endpoint (`cloud/internal/tpapi/sync.go` -> `syncConfigHandler`) is the engine that translates abstract cloud state into concrete edge files.

### CSV Generation
The handler fetches all active `services` and their `service_csv_rows` (the individual sensor registers/nodes). It passes these to `renderGatewayFiles` and `renderGatewayConfig`.
*   **Modbus TCP:** Generates `configs/<service>/config.csv` with the header `#Equipment,Reading,RegType,Address,type,Output,Table,Tags`.
*   **OPC-UA:** Generates `#Table,Device,Reading,OpcNode,Type,Freq,Output,Tags`.
*   **SNMP:** Generates a main `devices.csv` outlining IP addresses and template filenames. Crucially, it also dynamically generates individual OID map files (e.g., `configs/<service>/maps/gxt-rt-ups.csv`) containing `field_name,OID` pairs, based on the template's JSON arrays.
*   **MQTT:** Generates `configs/<service>/config.csv` which is actually a YAML file (`mapping.yml` format). It groups rows by `Topic` and defines complex JSONPath extraction rules (e.g., extracting `$.data.voltage` from an incoming MQTT payload).
*   **Gateway Config (`configs.yml`):** Generates the specific connection settings (broker URL, poll frequency, credentials) for the gateway binary to read.

### Sensor Map Generation
Crucially, during CSV generation, the TP-API builds `sensor_map.json`:
```json
{
  "UPS_Main.Battery_Voltage": "uuid-sensor-001"
}
```
This maps the gateway's output format (`Equipment.Reading` or `Device.Reading`) back to the cloud's internal UUID. The `conf-agent` writes this to `/opt/qube/sensor_map.json`.

### Docker Compose Generation
The `buildFullComposeYML` function constructs the deployment YAML.
1.  **Base Layer:** It injects the `enterprise-conf-agent` and `enterprise-influx-to-sql` services, ensuring they share the `qube-net` overlay network.
2.  **Dynamic Gateways:** It loops over the active `services`.
    *   It determines the Docker image via `imageForService`, checking the `registry_config` table (supporting GitHub/GitLab registries).
    *   It defines volume mounts linking the generated `/opt/qube/configs/<service>/...` files into the container's `/app/...` directory as read-only (`:ro`).
3.  **Deployment Execution:**
    *   The `conf-agent` (`deployDocker`) detects the environment using `docker info`.
    *   If `Swarm` is active (Production Qube): It runs `docker stack deploy -c docker-compose.yml qube`.
    *   If not (Dev mode): It runs `docker compose up -d`.

---

## 4. Telemetry Pipeline (Data Ingestion)

Data flows from the edge gateways back to the cloud via a specific pipeline designed to bridge the legacy Qube Lite architecture with the new Enterprise backend.

1.  **Gateways to Core-Switch:** The individual protocol gateways (Modbus, MQTT, etc.) poll their devices. They format the data as JSON and POST it to the local `core-switch` container (`http://core-switch:8585/batch`).
2.  **Core-Switch to InfluxDB:** The `core-switch` (a legacy component left untouched) converts this JSON into InfluxDB Line Protocol. It writes to a local InfluxDB v1 container, tagging the data with `device=<Equipment>` and `reading=<Reading>`.
3.  **The Enterprise Bridge (`enterprise-influx-to-sql`):**
    *   **Logic Location:** `enterprise-influx-to-sql/main.go`
    *   This service runs a polling loop (every 60s).
    *   It queries the local InfluxDB v1 using InfluxQL (`SELECT mean(value) FROM "Measurements" ... GROUP BY time(1m), device, reading`).
    *   **Resolution:** It loads `/config/sensor_map.json` (placed there by the `conf-agent`). It concatenates the InfluxDB tags (`rec.Equipment + "." + rec.Reading`) and looks up the corresponding `sensor_id` UUID.
    *   It batches these resolved readings into a JSON payload.
4.  **TP-API to Postgres:**
    *   The bridge POSTs the batch to the TP-API: `/v1/telemetry/ingest`.
    *   **Logic Location:** `cloud/internal/tpapi/telemetry.go` (`telemetryIngestHandler`)
    *   The TP-API verifies the HMAC token, parses the batch, and performs a bulk `INSERT INTO sensor_readings` in the Cloud PostgreSQL database, making the live data available to the frontend via the Cloud API (`:8080`).

---

## 5. Remote Command Execution

The `conf-agent` also supports executing ad-hoc shell commands sent from the cloud.
1.  **Queueing:** An Editor/Admin calls `POST /api/v1/qubes/{id}/commands` with a command like `restart_service`. This is inserted into `qube_commands`.
2.  **Polling:** The `conf-agent` calls `POST /v1/commands/poll` on the TP-API every cycle.
3.  **Execution:** The TP-API returns pending commands. The `conf-agent` (`execCommand` in `main.go`) executes them. For example, `restart_service` translates to `docker service update --force qube_<service>` (Swarm) or `docker compose restart <service>` (Compose).
4.  **Acknowledgment:** The agent POSTs the execution output back to `/v1/commands/{id}/ack`, updating the status in the database.
