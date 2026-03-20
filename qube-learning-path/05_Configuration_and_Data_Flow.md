# 05 - Configuration and Data Flow

To bring it all together, here are the two core workflows of the Qube Enterprise project.

## Workflow 1: Configuration Flow (Cloud to Edge)
*How does setting up a sensor on a Web UI magically make the device read that sensor?*

1. **User Action:** A user clicks "Add Sensor" on the Cloud Web UI.
2. **Cloud API:** The UI hits `POST /api/v1/gateways/{id}/sensors`. The Cloud API saves the sensor data into Postgres.
3. **Cloud Hash:** The Cloud recalculates the JSON configuration for the device and generates a new SHA256 hash.
4. **Edge Poll:** The `conf-agent` running on the Qube device wakes up (it checks every 30s) and hits `GET /v1/sync/state`. It sends its old hash. The server tells it to update.
5. **Edge Download:** `conf-agent` calls `GET /v1/sync/config` and downloads a `.tar.gz` payload containing a `docker-compose.yml`, protocol CSV files, and `sensor_map.json`.
6. **Edge Restart:** `conf-agent` unzips these files to `/opt/qube/configs/` and runs `docker compose up -d`. 
7. **Gateway Active:** The specific container (e.g., Modbus Gateway) wakes up, reads the newly downloaded `.csv` file, and begins polling the physical Modbus registers.

## Workflow 2: Data Flow (Edge to Cloud)
*How does live data reach the Cloud Dashboard?*

1. **Hardware Poll:** The running Gateway container reads `231.5 Volts` from the Modbus wires.
2. **Local Storage:** The Gateway sends JSON to formatting services, which insert that data point into the local `InfluxDB` instance running inside the Qube container system.
3. **Telemetry Extract:** The `enterprise-influx-to-sql` Go binary queries `InfluxDB` every 60 seconds: "Give me all new data points from the last minute".
4. **Data Mapping:** It checks the `sensor_map.json`. It sees that the physical register `Main_Meter.voltage` maps to Cloud UUID `sensor-1234-abcd`.
5. **Cloud Upload:** It formats the payload and POSTs them to the Cloud API at `:8081`. 
6. **DB Insert:** The Cloud API validates the Qube's signature and inserts the reading directly into the heavy-duty Enterprise Postgres Database table `sensor_readings`.
7. **User View:** The Frontend UI requests `GET /api/v1/data/sensors/{uuid}/latest` and plots the graph.
