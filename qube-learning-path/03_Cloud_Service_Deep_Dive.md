# 03 - Cloud Service Deep Dive

The Cloud service is the central brain of Qube Enterprise. If you look at the `cloud/` folder, it is written entirely in Go.

## Directory Structure
- `cmd/server/main.go`: The starting point of the Cloud service. It connects to the database (using `pgxpool`), and spins up two web servers concurrently (Port 8080 and 8081).
- `internal/api/`: Contains the code for the Cloud API (Port 8080). This is what the frontend UI talks to. It expects users to log in and present a JWT Token. 
  - `gateways.go` & `sensors.go`: Handle adding/editing devices. When a sensor is added here, it recalculates the Qube configuration hash.
- `internal/tpapi/`: Contains the code for the TP-API (Port 8081). This is what the Qube edge devices talk to. It expects Qubes to authenticate with a special HMAC hash, ensuring no one can spoof a Qube.
  - `sync.go`: Handles Qubes asking "has my configuration changed?" and sending them the new configurations (`docker-compose.yml` and CSVs).
  - `telemetry.go`: Endpoint (`/v1/telemetry/ingest`) that receives live sensor data from Qubes.
- `migrations/`: Contains `.sql` files to set up the Postgres tables (`qubes`, `gateways`, `sensors`, `sensor_readings`).

## How the Database Connection works
The cloud uses `pgxpool`, an excellent Postgres driver for Go. Instead of opening and closing database connections for every request, it maintains a pool of open connections, granting extremely high performance.

## The JWT vs HMAC Auth
If you want to view telemetry data via a curl request to Port `8080`, you use a JWT Bearer token:
`Authorization: Bearer <token>`
This is human/UI authentication.

If a Qube device needs to sync configurations via Port `8081`, it uses an HMAC signature generated from its device secret. This is machine-to-machine authentication.

## How Hashing works
In `hash.go`, whenever you add a sensor to a Gateway in Postgres, the API generates a JSON representation of the configuration. It then calculates an SHA256 Hash string of that JSON (e.g., `a1b2c3d4...`). It stores that hash in the DB.
When the Qube polls the server, it says "My current hash is `old_hash`". The server responds: "Your hash is outdated, the new one is `a1b2c3d4...`, please download new files."
