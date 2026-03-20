# 09 - Code Walkthrough: Influx-to-SQL

The `enterprise-influx-to-sql` (`enterprise-influx-to-sql/main.go`) bridges the gap between the old Qube Lite systems and the new Qube Enterprise Cloud.

## The Problem
Qube Lite's architecture consists of heavily optimized binaries written in C++ or Rust (like `core-switch`) that read Modbus data and shove it extremely fast into a local `InfluxDB v1` database. 
However, the Enterprise Cloud uses `PostgreSQL` and expects data to be sent via HTTP JSON payloads containing UUIDs (`sensor_id`). It knows nothing about `InfluxDB`.

## The Solution
Instead of rewriting the entire core-switch, we just add this Go binary as a "sidecar" on the device to translate.

## Walkthrough (`main.go`)

### 1. Initialization
`main()` loads `Configs.yml`, which contains the URLs to the local InfluxDB instance and the remote Cloud TP-API. It loops until InfluxDB boots up and is reachable.
It then starts a ticker (every 60 seconds) calling `runTransfer(cfg)`.

### 2. Loading the Map
Inside `runTransfer()`, it calls `loadSensorMap()`. 
Remember the `sensor_map.json` file that `conf-agent` downloaded during the sync phase? It looks like this:
```json
{
  "Main_Meter.voltage_v": "uuid-sensor-abcd-1234",
  "Panel_B.active_power": "uuid-sensor-efgh-5678"
}
```
It loads this into a Go map in memory.

### 3. Querying InfluxDB
It queries the local InfluxDB using InfluxQL (a SQL-like language for time-series data):
```sql
SELECT mean(value) FROM "Measurements" 
WHERE time >= {1 minute ago} 
GROUP BY time(1m), device, reading
```
- It groups data into 1-minute averages to save bandwidth.
- Native Qube Lite inserts data with tags: `device=Main_Meter` and `reading=voltage_v`.

### 4. Translation
For every row returned from InfluxDB:
- It combines device and reading to create the key: `Main_Meter.voltage_v`.
- It looks up that key in the `sensorMap`.
- If found, it extracts the `sensorID` (`uuid-sensor-abcd-1234`).
- It packages it into a `Reading` struct containing: `Time`, `SensorID`, `Value`.

### 5. Uploading to Cloud
Once it has translated all the readings into an array of `Reading` structs, it batches them into groups (e.g., 1000 records per post) and calls `postReadings()`.

`postReadings()` sends an HTTP POST request to the Cloud TP-API at `http://cloud-api:8081/v1/telemetry/ingest`.
It includes the `X-Qube-ID` and `Authorization: Bearer <qube_token>` HMAC headers.

The Cloud API receives this JSON array, verifies the HMAC, and does a lightning-fast bulk insert into the `sensor_readings` PostgreSQL table using `pgxpool`. 
The frontend dashboard instantly sees the new data on its charts!
