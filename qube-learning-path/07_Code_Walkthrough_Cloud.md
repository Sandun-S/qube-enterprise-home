# 07 - Code Walkthrough: Cloud API & TP-API

The `cloud/` directory contains the main Go server backend. It compiles into a single binary but runs two separate web servers concurrently.

## Entry Point: `cmd/server/main.go`
This is where execution begins.
1. **Database:** It connects to PostgreSQL using the `pgxpool` library (connection pooling for high performance).
2. **Servers:** It starts two Goroutines (concurrent threads):
   - `http.ListenAndServe(":8081", tpapi.NewRouter(pool))`: The TP-API for the Qube devices.
   - `http.ListenAndServe(":8080", api.NewRouter(pool, jwtSecret))`: The Cloud API for the Web UI.

## Part 1: The Cloud API (`internal/api`)
This handles human interaction (the frontend dashboard).

- **`router.go` & `middleware.go`**: All routes under `/api/v1/` require a JWT Token. The middleware intercepts requests, validates the signature (`jwtSecret`), and attaches the User ID and Organization ID to the request context.
- **`gateways.go` & `sensors.go`**: These handle standard CRUD operations (Create, Read, Update, Delete) into the Postgres `gateways` and `sensors` tables. 
  - **CRITICAL**: Whenever you add, update, or delete a sensor, the code explicitly calls `recomputeConfigHash()` (found in `hash.go`).
- **`hash.go` (`recomputeConfigHash`)**: This function queries the database for *all* active gateways and sensors for a specific Qube. It dumps them into a giant JSON object. Then, it runs a `SHA-256` hash over that JSON. If the JSON changed, the hash changes (`a1b2c...`). It saves this new hash in the `config_state` table.

## Part 2: The TP-API (`internal/tpapi`)
This handles machine interaction (the Qubes).

- **`router.go`**: Routes under `/v1/` do NOT use JWTs. Instead, they use HMAC. The `tpapiAuthMiddleware` looks at the `X-Qube-ID` header, looks up the device's secret key in the database, and cryptographically verifies the signature.
- **`sync.go`**: This is the most complex and important file in the project.
  - **`syncStateHandler` (`GET /v1/sync/state`)**: Extremely fast endpoint. The `conf-agent` on the device hits this every 30 seconds. It simply does `SELECT hash FROM config_state` and returns it.
  - **`syncConfigHandler` (`GET /v1/sync/config`)**: When the hash mismatches, the Qube calls this. This function does heavy lifting:
    1. Selects all services and gateways from DB.
    2. Builds the specific CSV format required for each protocol (`modbus_tcp`, `opcua`, `snmp`, `mqtt`) using the `renderGatewayFiles` function. For instance, `modbus_tcp` gets `#Equipment,Reading,RegType...`.
    3. Builds the `docker-compose.yml` payload dynamically using `buildFullComposeYML()`. It creates a block for `enterprise-conf-agent`, `enterprise-influx-to-sql`, and blocks for every active gateway.
    4. Builds the `sensor_map.json`, telling the device how to map `Equipment.Reading` strings into UUIDs.
    5. Returns all these files in a single large JSON response.
- **`sync.go` (`deviceRegisterHandler`)**: Devices call this on their very first boot (reading `/boot/mit.txt`). They pass `device_id` and `register_key`. If the customer has claimed the device in the portal, this endpoint returns the `qube_token` (HMAC secret).

## Part 3: Migrations (`migrations/`)
The database schema uses plain `.sql` files. They run automatically when the `postgres` container starts in dev.
- `qubes` stores devices.
- `gateways` stores communication protocols attached to qubes.
- `sensors` stores specific registers being read by gateways.
- `config_state` holds the magical configuration hash that drives the entire sync system.
