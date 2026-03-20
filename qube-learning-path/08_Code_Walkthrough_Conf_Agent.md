# 08 - Code Walkthrough: Conf Agent

The `conf-agent` (`conf-agent/main.go`) is the beating heart of the edge device. It runs continuously as a system daemon (or Docker container).

## Initialization
In `func main()`:
1. **Load Config**: Reads `POLL_INTERVAL` and `TPAPI_URL`.
2. **Read `mit.txt`**: The agent attempts to read `/boot/mit.txt`. This is a file flashed into the SD card of the Raspberry Pi at the factory. It contains the hardcoded `deviceid` and `register`.
3. **Wait for Network**: It loops `GET /health` until the Cloud API is reachable.
4. **Self Registration (`selfRegister`)**:
   - If it doesn't have a `QUBE_TOKEN` stored locally, it calls `POST /v1/device/register` on the Cloud TP-API using the `device_id` and `register_key`.
   - If the Cloud says "202 Pending" (customer hasn't claimed it yet), it sleeps for 60 seconds and tries again.
   - If the Cloud says "200 Claimed", it downloads the `qube_token`, saves it locally to `.env`, and proceeds.
5. **Local State**: It reads a hidden file `.config_hash` from its local hard drive. This tells it what version of the configuration it is currently running.

## The Main Loop
It enters an infinite loop using a `time.Ticker` (every 30 seconds):
`runCycle(client, cfg, &localHash, hashFile)`

In `runCycle`:
1. **Heartbeat**: Calls `POST /v1/heartbeat` so the Cloud knows the device is online.
2. **Remote Commands**: Calls `POST /v1/commands/poll` (allows cloud users to reboot the Pi or restart specific containers).
3. **Check Hash**: Calls `GET /v1/sync/state` to get the latest Hash from the Cloud.
   - If `remoteHash == localHash`, it does nothing and goes back to sleep.
   - If they mismatch, it triggers `getConfig(client)`.

## Applying Configuration
The `applyConfig` function is called when a new hash is detected:
1. It downloads the giant JSON response.
2. **Write Compose**: Saves `docker-compose.yml` to the local disk.
3. **Write CSVs**: Loops through the `CSVFiles` map in the JSON and writes them (e.g., `/opt/qube/configs/modbus-a/config.csv`).
4. **Deploy Docker**: Calls `deployDocker(cfg.WorkDir)`.

In `deployDocker()`:
- It checks if Docker is running in **Swarm Mode**: `docker info --format {{.Swarm.LocalNodeState}}`
- If Swarm mode (`active`), it runs: 
  `docker stack deploy -c docker-compose.yml qube`
- If normal Compose (e.g., local testing), it runs:
  `docker compose up -d`

Docker natively diffs the new `docker-compose.yml` against the running containers. If configs changed, it gracefully shuts down the affected gateway containers and starts new ones with the fresh CSV files mounted inside them!
