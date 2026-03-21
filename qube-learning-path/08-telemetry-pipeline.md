# 08 — Telemetry Pipeline: Device Data to Postgres

This file follows one reading from a physical power meter to the frontend dashboard query.

---

## The full path

```
Physical meter (Modbus TCP at 192.168.1.100:502)
  ↓  register 3000 = 1250 (= 125.0W with scale 0.1)
modbus-gateway   reads config.csv, polls register every 5s
  ↓  POST http://core-switch:8080/v3/batch
core-switch      routes to InfluxDB
  ↓  POST http://influxdb-relay:9096/write
influxdb-relay   fans out for HA
  ↓  POST http://influxdb:8086/write
InfluxDB v1      measurement=Measurements, device=Main_Meter, reading=active_power_w, value=125.0
  ↓  [60s poll]
enterprise-influx-to-sql  queries InfluxDB, maps to sensor_id, POSTs to TP-API
  ↓  POST http://cloud:8081/v1/telemetry/ingest
TP-API           pgx.Batch insert into sensor_readings
  ↓
Postgres         sensor_readings: (sensor_id=uuid, field_key="active_power_w", value=125.0)
  ↓  [frontend query]
Cloud API        GET /api/v1/data/sensors/{id}/latest
  ↓
Frontend dashboard
```

---

## 1. modbus-gateway reads the device

The modbus-gateway binary (existing Qube Lite) reads `configs.yml` for connection settings and `config.csv` for what to read:

```
# /opt/qube/configs/panel-a/config.csv
#Equipment,Reading,RegType,Address,type,Output,Table,Tags
Main_Meter,active_power_w,Holding,3000,uint16,influxdb,Measurements,location=panel_a
Main_Meter,voltage_ll_v,Holding,3020,uint16,influxdb,Measurements,location=panel_a
```

It sends to core-switch in batch format:
```json
{
  "data": [
    {
      "measurement": "Measurements",
      "fields": {"value": 1250},
      "tags": {"device": "Main_Meter", "reading": "active_power_w", "location": "panel_a"}
    }
  ]
}
```

---

## 2. core-switch → influxdb-relay → InfluxDB

The data arrives in InfluxDB as:
```
measurement: Measurements
tags:   device=Main_Meter, reading=active_power_w
fields: value=1250
time:   2024-01-15T10:23:05Z
```

InfluxDB v1 stores this as time-series data. The database is named `qube-db`.

---

## 3. enterprise-influx-to-sql polls and maps

File: `enterprise-influx-to-sql/main.go`

```go
func main() {
    // Connect to InfluxDB
    influxClient, _ := client.NewHTTPClient(client.HTTPConfig{
        Addr: cfg.Service.InfluxURL,  // "http://influxdb:8086"
    })
    
    ticker := time.NewTicker(time.Duration(cfg.Service.LookbackMins) * time.Minute)
    for range ticker.C {
        transferReadings(cfg, influxClient)
    }
}

func transferReadings(cfg Config, c client.Client) {
    // 1. Load sensor_map.json
    // {"Main_Meter.active_power_w": "uuid-of-sensor", ...}
    sensorMap := loadSensorMap(cfg.Service.SensorMapPath)
    
    // 2. Query InfluxDB for recent readings
    since := time.Now().Add(-time.Duration(cfg.Service.LookbackMins) * time.Minute)
    q := fmt.Sprintf(
        `SELECT * FROM "Measurements" WHERE time > '%s'`,
        since.UTC().Format(time.RFC3339))
    
    response, _ := c.Query(client.NewQuery(q, cfg.InfluxDB.DB, ""))
    
    // 3. Map each row to a sensor_id
    var readings []Reading
    for _, result := range response.Results {
        for _, series := range result.Series {
            device  := series.Tags["device"]   // "Main_Meter"
            reading := series.Tags["reading"]  // "active_power_w"
            key := device + "." + reading      // "Main_Meter.active_power_w"
            
            sensorID, ok := sensorMap[key]
            if !ok { continue }  // sensor not in map — skip
            
            for _, row := range series.Values {
                value, _ := row[1].(json.Number).Float64()
                readings = append(readings, Reading{
                    SensorID: sensorID,
                    FieldKey: reading,
                    Value:    value,
                    Unit:     "",
                })
            }
        }
    }
    
    // 4. POST to TP-API
    postToTPAPI(cfg, readings)
}
```

---

## 4. The sensor_map.json

This is the bridge between InfluxDB tag names and Postgres UUIDs:

```json
{
  "Main_Meter.active_power_w": "52e25532-2cb0-4580-898e-3593706ad11f",
  "Main_Meter.voltage_ll_v":   "52e25532-2cb0-4580-898e-3593706ad11f",
  "Sub_Meter.active_power_w":  "7f3b2a01-8d4c-4e2b-9f1a-2c45b6d7e890"
}
```

Format: `"device_name.field_key"` → `sensor_uuid`

This file is written by conf-agent when it processes `GET /v1/sync/config`. It's stored at `/opt/qube/sensor_map.json`. The enterprise-influx-to-sql reads it from the same path.

**What if a sensor isn't in the map?**
It gets silently skipped. This is intentional — maybe it's a Qube Lite sensor that predates Enterprise. The log shows `[sensor_map] key "Old_Meter.voltage" not found — skipping`.

---

## 5. Telemetry ingest — pgx.Batch

```go
// cloud/internal/tpapi/telemetry.go
batch := &pgx.Batch{}
for _, reading := range req.Readings {
    batch.Queue(
        `INSERT INTO sensor_readings (sensor_id, field_key, value, unit, recorded_at)
         VALUES ($1, $2, $3, $4, NOW())`,
        reading.SensorID, reading.FieldKey, reading.Value, reading.Unit)
}

results := pool.SendBatch(context.Background(), batch)
defer results.Close()
```

`pgx.Batch` is critical for performance. Without it, 100 readings = 100 separate SQL commands, each with a network round-trip. With it, all 100 are sent in one network packet.

---

## 6. Reading the data back

```go
// cloud/internal/api/telemetry.go
// GET /api/v1/data/sensors/:id/latest
func latestReadingHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        sensorID := chi.URLParam(r, "id")
        orgID    := r.Context().Value(ctxOrgID).(string)
        
        // Get all latest values — one per field_key
        rows, _ := pool.Query(ctx,
            `SELECT DISTINCT ON (field_key)
                 field_key, value, unit, recorded_at
             FROM sensor_readings
             WHERE sensor_id = $1
             ORDER BY field_key, recorded_at DESC`,
            sensorID)
        
        // DISTINCT ON (field_key) with ORDER BY field_key, recorded_at DESC
        // = for each field_key, return only the most recent row
        // This is more efficient than a subquery or GROUP BY
    }
}
```

The `DISTINCT ON` clause is PostgreSQL-specific and very efficient with the `idx_readings_sensor_time` index on `(sensor_id, recorded_at DESC)`.

---

## Data lag

There's inherent lag in this pipeline:
- Modbus polls every 5s → core-switch writes immediately
- enterprise-influx-to-sql polls every 60s → typically 60-120s lag from measurement to Postgres
- Frontend queries Postgres in real time → 0 additional lag

For most industrial monitoring use cases (energy meters, UPS status, environmental sensors), 1-2 minute lag is completely fine. If you need real-time, you'd modify enterprise-influx-to-sql to poll more frequently (e.g. every 10s).

---

## What if InfluxDB already has data from Qube Lite?

If Qube Lite was already running (core-switch + InfluxDB) before Enterprise was set up, InfluxDB has historical data. enterprise-influx-to-sql will pick up all data from the last `LookbackMins` minutes on each poll. Historical data older than `LookbackMins` is not backfilled — you'd need to change that setting and do a one-time historical import if needed.
