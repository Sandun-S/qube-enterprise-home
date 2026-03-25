# Qube Enterprise: Deep Dive Architecture & Workflows

This document provides a fully technical, deep-level explanation of the core workflows in the Qube Enterprise system. It covers how the different components (Cloud API, TP API, Conf Agent) interact, focusing on device provisioning, authentication, configuration synchronization, and data ingestion.

---

## 1. Authentication & Token Lifecycle

The system uses two completely separate authentication mechanisms: JWT for user/frontend interaction with the Cloud API, and HMAC for Qube device interaction with the TP-API.

### User Login & Cloud API Auth (JWT)

When a user logs in via the Cloud API (`POST /api/v1/auth/login`), the system verifies their credentials using PostgreSQL's `crypt()` function (compatible with bcrypt).
Upon success, it generates a JSON Web Token (JWT) signed with `HS256` using the `JWT_SECRET` environment variable.

*   **Token Content:** The JWT payload includes `user_id`, `org_id`, `role`, and an expiration time (`exp`) of 24 hours.
*   **Lifecycle:** The token expires after 24 hours. Users must log in again to receive a new token. It is passed in the `Authorization: Bearer <token>` header for all protected Cloud API routes (`:8080`).

### Qube Device Auth & TP-API (HMAC)

Qube devices do *not* use JWTs. They use a permanent, deterministic HMAC token to authenticate with the TP-API (`:8081`). This token is automatically generated when a customer claims the device.

*   **Generation (The "Claim" Flow):**
    1.  At the factory, a Qube is flashed and assigned a `device_id` (e.g., "Qube-1302") and a unique `register_key` (e.g., "4D4L-R4KY-ZTQ5"). These are written to `/boot/mit.txt` on the device and inserted into the enterprise database.
    2.  A user logs into the Cloud API and calls `POST /api/v1/qubes/claim` with the `register_key` found on their physical device.
    3.  The Cloud API verifies the key, associates the device with the user's `org_id`, and retrieves the organisation's `org_secret` from the database.
    4.  It generates the `QUBE_TOKEN` using HMAC-SHA256: `HMAC-SHA256(org_secret, "device_id:org_secret")`.
    5.  This deterministic token is stored in the database (`qubes.auth_token_hash`).

*   **Retrieval by the Device (Self-Registration):**
    1.  When the Qube boots, the `conf-agent` reads `/boot/mit.txt` to get its `device_id` and `register_key`.
    2.  If the agent doesn't have a token (e.g., first boot), it calls the *public* TP-API endpoint `POST /v1/device/register` with these credentials.
    3.  If the device *has* been claimed (step 2 above), the TP-API computes the HMAC token using the exact same formula and returns it to the device.
    4.  The `conf-agent` saves this token to `/opt/qube/.env` (`QUBE_TOKEN=<token>`).

*   **Lifecycle:** The `QUBE_TOKEN` **does not change or expire** unless the device is unclaimed/re-claimed by a different organization, which would change the `org_secret` used to generate it. The `conf-agent` includes this token in the `Authorization: Bearer <token>` header (along with `X-Qube-ID`) for all subsequent TP-API calls. The TP-API's `qubeAuthMiddleware` re-computes the HMAC on every request to verify it.

---

## 2. Configuration Hashing & Synchronization

The enterprise system uses a declarative, state-reconciliation model to manage edge devices. Instead of sending imperative "install this sensor" commands, the cloud maintains the "desired state" of the device and computes a hash of this state.

### Hash Generation (Cloud Side)

Whenever a user modifies a gateway, sensor, or template via the Cloud API (e.g., `POST /api/v1/gateways/{id}/sensors`), the `recomputeConfigHash` function is triggered.

1.  **Canonical JSON:** It queries the database for all active gateways and their active sensors associated with the `qube_id`.
2.  It serializes this entire configuration into a specific, canonical JSON structure:
    ```json
    {
      "qube_id": "Qube-1302",
      "gateways": [
        {
          "id": "gw-1",
          "protocol": "modbus_tcp",
          "config_json": {...},
          "sensors": [
            {"id": "s-1", "address_params": {...}, "tags_json": {...}}
          ]
        }
      ]
    }
    ```
3.  **SHA-256:** It computes a SHA-256 hash of this JSON string.
4.  **Storage:** The new hash (and the JSON snapshot) is saved to the `config_state` table for that Qube.

### Polling & Synchronization (Qube Side)

The `conf-agent` running on the Qube is responsible for keeping the physical device in sync with the cloud's desired state.

1.  **Polling:** Every `POLL_INTERVAL` (default 30s), the `conf-agent` calls `GET /v1/sync/state` on the TP-API.
2.  **Comparison:** It receives the current `hash` from the cloud and compares it against its locally stored hash (`/opt/qube/.config_hash`).
3.  **Mismatch:** If the hashes differ, it means the cloud configuration has changed. The agent calls `GET /v1/sync/config` to download the entire new configuration payload.

---

## 3. CSV and Docker Compose Generation

When the `conf-agent` requests `GET /v1/sync/config`, the TP-API generates everything the device needs to run its gateways.

### CSV Generation

Different protocol gateways (Modbus, OPC-UA, SNMP, MQTT) require configuration in different CSV or YAML formats. The TP-API handles this translation.

1.  **Database Query:** The TP-API fetches all active `services` (gateways) and their associated `service_csv_rows` for the Qube. These rows represent the individual registers or nodes to poll.
2.  **Protocol-Specific Rendering:** The `renderGatewayFiles` function formats the data based on the protocol:
    *   **Modbus:** Generates `#Equipment,Reading,RegType,Address...`
    *   **OPC-UA:** Generates `#Table,Device,Reading,OpcNode...`
    *   **SNMP:** Generates a main `devices.csv` and also creates individual OID mapping CSV files in a `maps/` subdirectory based on the device template.
    *   **MQTT:** Generates a `mapping.yml` (despite the function name) grouping rules by topic and defining JSONPath extraction rules.
3.  **Sensor Map:** Crucially, it generates `sensor_map.json`. This file maps the combination of `Equipment.Reading` (which the gateway outputs) back to the internal `sensor_id` UUID used by the Cloud database.

### Docker Compose Generation

The TP-API dynamically constructs a `docker-compose.yml` file (`buildFullComposeYML`).

1.  **Base Services:** It always includes the `enterprise-conf-agent` and `enterprise-influx-to-sql` services.
2.  **Dynamic Gateways:** For every active gateway service, it adds a new block to the compose file.
    *   It determines the correct Docker image (e.g., `ghcr.io/.../modbus:arm64.latest`) based on the protocol and the `registry_config` table.
    *   It configures host-path volume mounts to map the generated CSV files into the container (e.g., `/opt/qube/configs/panel-a/config.csv:/app/config.csv:ro`).
3.  **Swarm Support:** The generated file is formatted to be compatible with `docker stack deploy` (using the `qube-net` overlay network and `deploy` blocks), matching the production environment of real Qubes.

### Execution on the Qube

Once the `conf-agent` receives this payload:
1.  It writes the `docker-compose.yml` to disk.
2.  It creates the directories and writes all the protocol-specific `.csv` and `.yml` files into `/opt/qube/configs/<service-name>/`.
3.  It writes the `sensor_map.json`.
4.  It detects if the device is running Docker Swarm.
    *   If Swarm is active (Production): It runs `docker stack deploy -c docker-compose.yml qube`.
    *   If not (Dev): It runs `docker compose up -d`.
5.  Docker automatically restarts any containers whose configuration files or environment variables have changed.

---

## 4. Telemetry Pipeline (Data Ingestion)

The telemetry pipeline involves the existing Qube "Core-Switch", the new `enterprise-influx-to-sql` service, and the TP-API.

1.  **Gateways to Core-Switch:** The gateway containers poll the physical devices based on their generated CSV configs. They HTTP POST this data to the local `core-switch` container running on the Qube.
2.  **Core-Switch to InfluxDB:** The `core-switch` routes this data and writes it to a local InfluxDB v1 container using the Line Protocol, tagging the data with `device=<Equipment>` and `reading=<Reading>`.
3.  **Influx-to-SQL Bridge:** The `enterprise-influx-to-sql` service runs a polling loop (every 60s):
    *   It reads the `sensor_map.json` file generated during the config sync.
    *   It queries the local InfluxDB v1 for recent readings.
    *   It matches the InfluxDB tags (`device` + `reading`) against the `sensor_map.json` to resolve the original cloud `sensor_id`.
    *   It packages these readings into a JSON payload and HTTP POSTs them to the TP-API endpoint `/v1/telemetry/ingest`.
4.  **TP-API to Postgres:** The TP-API receives the batch, verifies the HMAC token, and inserts the data into the Cloud PostgreSQL `sensor_readings` table, where it can be queried by the frontend.
