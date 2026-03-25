# Qube Enterprise: Exhaustive Deep Dive & Code Implementation Guide

This document provides a line-by-line, struct-by-struct analysis of the core workflows in the Qube Enterprise system. It details exact files, functions, SQL queries, and logic used for device provisioning, authentication, configuration synchronization, CSV/Docker Compose generation, and telemetry ingestion.

---

## 1. Authentication, Tokens, and the "Claim" Flow

The system manages two distinct authentication realms.

### A. Cloud API Authentication (Port 8080)
Used strictly by the web frontend for human operators.

*   **Location:** `cloud/internal/api/auth.go`
*   **Database:** `users` table.
*   **Password Hashing:** Relies purely on PostgreSQL's `pgcrypto` extension for maximum security and DB-level compatibility.
    *   **Registration Query (`registerHandler`):**
        ```sql
        INSERT INTO users (org_id, email, password_hash, role)
        VALUES ($1, $2, crypt($3, gen_salt('bf',12)), 'admin') RETURNING id
        ```
    *   **Login Query (`loginHandler`):**
        ```sql
        SELECT id, org_id, role, (password_hash = crypt($2, password_hash)) AS pw_match
        FROM users WHERE email=$1
        ```
*   **Token (JWT):** If `pw_match` is true, a JWT is created via `makeJWT()`. It contains `user_id`, `org_id`, `role`, and an `exp` (expiration) set to exactly 24 hours (`time.Now().Add(24 * time.Hour).Unix()`). The token must be passed as `Authorization: Bearer <token>`.

### B. Device Authentication (Port 8081)
Qube devices communicate with the TP-API using permanent, deterministic HMAC tokens.

*   **Location:** `cloud/internal/api/qubes.go` (Cloud side) & `cloud/internal/tpapi/router.go` (TP-API side).
*   **The Initial State:** A Qube arrives from the factory with `/boot/mit.txt` containing:
    ```yaml
    deviceid: Qube-1302
    register: 4D4L-R4KY-ZTQ5
    ```

**The "Claim" Action (Cloud Side):**
When a user submits the `register_key` in the UI, `claimQubeHandler` executes:
1.  **Lookup:**
    ```sql
    SELECT id, org_id FROM qubes WHERE register_key=$1
    ```
2.  **Secret Retrieval:** Fetches the `org_secret` from the `organisations` table.
3.  **HMAC Generation:**
    ```go
    func computeHMAC(qubeID, orgSecret string) string {
        mac := hmac.New(sha256.New, []byte(orgSecret))
        mac.Write([]byte(qubeID + ":" + orgSecret))
        return hex.EncodeToString(mac.Sum(nil))
    }
    ```
4.  **Storage:** The resulting hash is saved to `qubes.auth_token_hash`.

**Device Self-Registration (Qube Side):**
When `conf-agent` starts, it reads `/boot/mit.txt`. If it lacks a token, it calls the *public* `/v1/device/register` TP-API endpoint.
1.  **The API Check (`deviceRegisterHandler`):**
    ```sql
    SELECT q.org_id, COALESCE(o.org_secret, ''), (q.org_id IS NOT NULL) AS claimed
    FROM qubes q LEFT JOIN organisations o ON o.id = q.org_id
    WHERE q.id=$1 AND q.register_key=$2
    ```
2.  **The Return:** If `claimed` is true, the TP-API computes the *exact same* HMAC using the same `computeHMAC()` function and returns it.
3.  **Persistence:** The `conf-agent` writes it to `/opt/qube/.env`.

**Request Authentication:**
Every subsequent TP-API call passes through `qubeAuthMiddleware`. It extracts `X-Qube-ID` and `Authorization: Bearer <token>`, looks up the `org_secret`, re-runs `computeHMAC()`, and asserts equality via `hmac.Equal()`.

---

## 2. Configuration Hashing: The "State Engine"

The cloud manages the desired state of a Qube via a unified SHA-256 hash. Any change to a gateway or sensor triggers a recomputation.

*   **Location:** `cloud/internal/api/hash.go` (`recomputeConfigHash`)
*   **The Data Collection Phase:**
    1.  Fetch all active gateways for the Qube:
        ```sql
        SELECT g.id, g.name, g.protocol, g.host, g.port, g.config_json, g.service_image
        FROM gateways g WHERE g.qube_id=$1 AND g.status='active'
        ```
    2.  For *each* gateway, fetch active sensors:
        ```sql
        SELECT id, name, template_id, address_params, tags_json
        FROM sensors WHERE gateway_id=$1 AND status='active'
        ```
*   **The Serialization Phase:**
    The results are packed into a canonical map:
    ```go
    canonical, err := json.Marshal(map[string]any{
        "qube_id":  qubeID,
        "gateways": gateways, // The slice of gwRow structs populated above
    })
    ```
*   **The Hashing Phase:**
    ```go
    sum := sha256.Sum256(canonical)
    hash := hex.EncodeToString(sum[:])
    ```
*   **Storage:** The `hash` and `canonical` JSON are updated in the `config_state` table.

---

## 3. The `conf-agent` Polling & Execution Loop

The edge agent runs a relentless reconciliation loop.

*   **Location:** `conf-agent/main.go`
*   **The `runCycle()` Loop (every 30s):**
    1.  `sendHeartbeat()`: POST to `/v1/heartbeat`. Updates `last_seen` in the DB.
    2.  `executeCommands()`: POST to `/v1/commands/poll`. Fetches pending shell commands (e.g., `restart_service`), executes via `os/exec`, and POSTs the stdout/stderr back to `/v1/commands/{id}/ack`.
    3.  `getState()`: GET `/v1/sync/state`. Returns the cloud's SHA-256 hash.
    4.  **The Check:** If `state.Hash == localHash`, it aborts the cycle.
    5.  `getConfig()`: GET `/v1/sync/config`. Downloads the entire configuration payload if there's a mismatch.
    6.  `applyConfig()`: Writes the downloaded `docker_compose_yml`, `csv_files`, and `sensor_map` to `/opt/qube/configs/...`.
    7.  `deployDocker()`: The critical deployment step.
        ```go
        swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
        isSwarm := strings.TrimSpace(string(swarmOut)) == "active"
        if isSwarm {
            // Production Qube
            exec.Command("docker", "stack", "deploy", "-c", composePath, "--with-registry-auth", "qube")
        } else {
            // Dev VM
            exec.Command("docker", "compose", "-f", composePath, "up", "-d", "--remove-orphans")
        }
        ```

---

## 4. Server-Side Rendering: CSVs and Docker Compose

When the `conf-agent` calls `/v1/sync/config`, the TP-API dynamically generates the files.

*   **Location:** `cloud/internal/tpapi/sync.go`
*   **The Data Gathering:** Similar to hashing, it queries active `services` and their associated `service_csv_rows`.

### A. CSV File Generation (`renderGatewayFiles`)
Each protocol demands a different configuration file format.

1.  **Modbus TCP (`config.csv`):**
    Outputs a flat CSV matching the legacy Modbus gateway format:
    ```csv
    #Equipment,Reading,RegType,Address,type,Output,Table,Tags
    Main_Meter,voltage_a,HoldingRegister,40001,float32,influxdb,Measurements,site=HQ
    ```

2.  **SNMP (`devices.csv` & `maps/*.csv`):**
    Generates a master `devices.csv` file mapping IPs to template files.
    ```csv
    #Table, Device, SNMP csv, Community, Version, Output, Tags
    Measurements,10.0.0.50,gxt-rt-ups.csv,public,2c,influxdb,name=UPS_A
    ```
    *Crucially*, it also iterates through the raw `_oids` JSON payload from the database and writes a separate OID mapping file for each template (e.g., `maps/gxt-rt-ups.csv`):
    ```csv
    battery_voltage,.1.3.6.1.4.1.476.1.42.3.5.1.23.1.2.1.1.2
    ```

3.  **MQTT (`mapping.yml`):**
    Groups rows by topic and generates a YAML file utilizing JSONPath extraction.
    ```yaml
    - Topic: "sensors/temp/+"
      Table: "Measurements"
      Mapping:
        - Device: ["FIXED", "TempSensor"]
          Reading: ["FIXED", "temperature"]
          Value: ["FIELD", "data.temp_c"] # Translates to gjson path "data.temp_c"
          Output: ["FIXED", "influxdb"]
    ```

### B. Gateway Config Generation (`renderGatewayConfig`)
Generates the `configs.yml` for the gateway binary itself (connection settings).
Example for Modbus:
```yaml
loglevel: "info"
modbus:
  server: "tcp://192.168.1.100:502"
  readingsfile: "config.csv"
  freqsec: 20
```

### C. Docker Compose Generation (`buildFullComposeYML`)
Dynamically constructs the Swarm-compatible YAML string.
1.  **Base:** Adds `enterprise-conf-agent` and `enterprise-influx-to-sql` attached to the `qube-net` overlay.
2.  **Gateways:** Iterates through `services`. For each:
    *   Looks up the image via `imageForService(pool, protocol)`.
    *   Creates a `service` block with volume mounts mapping the generated CSVs into the container:
        ```yaml
        volumes:
          - /opt/qube/configs/panel-a/configs.yml:/app/configs.yml:ro
          - /opt/qube/configs/panel-a/config.csv:/app/config.csv:ro
        ```

### D. The Bridge Map (`sensor_map.json`)
The TP-API builds a map linking the gateway's string output (`Equipment.Reading`) back to the cloud's UUID `sensor_id`. This is critical for the telemetry pipeline.
```json
{
  "Main_Meter.voltage_a": "550e8400-e29b-41d4-a716-446655440000"
}
```

---

## 5. The Edge Data Pipeline: Ingestion to Cloud

Data collection relies on legacy containers passing data to new Enterprise bridges.

### Step 1: Gateway -> Core-Switch
A protocol gateway (e.g., `mqtt-gateway`) reads data.
*   **Location:** `mqtt-gateway/main.go`
*   **Process:** The MQTT gateway uses `gjson.Get(jsonStr, rule.JSONPath)` to extract values based on the generated `mapping.yml`.
*   It POSTs a JSON payload to the local `core-switch` container (`http://coreswitch:8080/ingest`).

### Step 2: Core-Switch -> Local InfluxDB
The legacy `core-switch` container (untouched by this enterprise code) receives the JSON. It transforms it into InfluxDB Line Protocol and writes it to a local InfluxDB v1 container.
*   **Crucial Format:** It translates the payload into tags: `device=Main_Meter` and `reading=voltage_a`.

### Step 3: Local InfluxDB -> TP-API (`enterprise-influx-to-sql`)
This is the Enterprise data bridge running on the Qube.
*   **Location:** `enterprise-influx-to-sql/main.go`
*   **The Loop (`runTransfer`):** Runs every 60s.
    1.  Loads `/config/sensor_map.json` (written by the `conf-agent`).
    2.  Executes an InfluxQL query against the local InfluxDB:
        ```sql
        SELECT mean(value) FROM "Measurements"
        WHERE time >= '...' AND time < '...'
        GROUP BY time(1m), device, reading ORDER BY time ASC
        ```
    3.  **The Resolution:** It loops through the results. For every record, it reconstructs the key:
        ```go
        key := rec.Equipment + "." + rec.Reading // e.g., "Main_Meter.voltage_a"
        sensorID, ok := sensorMap[key]
        ```
    4.  If matched, it appends the `Reading` struct to a batch.
    5.  It POSTs the batch to `POST /v1/telemetry/ingest` on the TP-API.

### Step 4: TP-API -> Cloud PostgreSQL
*   **Location:** `cloud/internal/tpapi/telemetry.go`
*   The TP-API receives the `[]Reading` payload.
*   It performs a bulk, batched `INSERT` into the `sensor_readings` table.
*   The data is now available globally via the Cloud API `GET /api/v1/data/readings` endpoints.