# 01 - Introduction and Architecture

Welcome to the Qube Enterprise guided learning path! Since you are new to the codebase, we'll break it down systematically.

## What is Qube Enterprise?
Qube started as **Qube Lite**, edge devices (Raspberry Pis) that read from industrial sensors using protocols like Modbus, OPC-UA, SNMP. 
These devices traditionally had to be configured directly via SSH or basic commands.

**Qube Enterprise** adds a centralized cloud management layer. Instead of configuring each device (Qube) manually:
1. You configure sensors in a Cloud Web UI.
2. The Cloud saves configurations to a database and updates a "configuration hash."
3. The Qube edge devices poll the Cloud, see the hash changed, and automatically download new configurations and `docker-compose.yml` setups.
4. The Qube automatically restarts its internal containers to apply changes.
5. Telemetry data from sensors flows automatically from the Qube up to the Cloud database.

## Architecture Overview

There are two distinct sides to this system:

### 1. Cloud Side (The Brain)
Runs on a cloud server (or VM).
- **Enterprise Postgres Database:** Stores organizations, users, Qubes, gateways, sensors, and the actual live telemetry data.
- **Cloud API (`:8080`):** Used by the Web UI / Frontend. Authenticated via **JWT** (JSON Web Tokens). Users manage devices here.
- **TP-API (`:8081`):** Short for "Telemetry and Provisioning API." This is **only** for Qube devices to talk to. Authenticated via **HMAC** (using a secret token).

### 2. Edge Side (The Qube Device)
Runs on the physical hardware (e.g., Raspberry Pi) in the factory/field.
- **Docker Compose / Docker Swarm:** The engine that runs smaller gateway programs.
- **Enterprise `conf-agent`:** A lightweight Go program running in the background. It constantly polls the Cloud TP-API to check if configurations have changed. If yes, it downloads new files and triggers `docker stack deploy` or `docker compose up`.
- **Protocol Gateways (e.g., `mqtt-gateway`, `modbus-gateway`):** Programs that read physical sensors.
- **`enterprise-influx-to-sql`:** Another Go program. It listens to the data coming from sensors (temporarily stored in local InfluxDB) and pushes it up to the Cloud TP-API.

### Summary of the Flow
User sets up sensor in UI -> Postgres DB Updates -> Hash changes -> Qube `conf-agent` spots hash change -> Qube downloads new config -> Qube restarts gateway containers -> Gateway reads sensor -> Gateway sends data to local InfluxDB -> `enterprise-influx-to-sql` reads it -> Pushes to Cloud TP-API -> Saved in Postgres!

This setup is known as **"Zero-Touch Provisioning."** After the initial setup, you never need to manually SSH into the box again.
