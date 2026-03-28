# Core-Switch v2

Data router for Qube Enterprise edge. Receives data from all reader containers and routes to InfluxDB and/or MQTT based on the `Output` field.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v3/batch` | Batch data (same Equipment) |
| POST | `/v3/data` | Single data point |
| POST | `/v3/alerts` | Alert from readers/services |
| GET | `/metrics` | Prometheus metrics |

## DataIn Format

```json
{
  "Table": "Measurements",
  "Equipment": "PM5100",
  "Reading": "active_power_w",
  "Output": "influxdb",
  "Sender": "modbus-reader",
  "Tags": "name=PM5100,location=rack1",
  "Time": 1711612345000000,
  "Value": "1250.5"
}
```

## Output Routing

- `influxdb` → writes to InfluxDB via line protocol
- `mqtt` → publishes to MQTT broker
- `live` → forwards to MQTT (picked up by conf-agent for WebSocket relay)
- `influxdb,live` → both InfluxDB + MQTT/live

## Configuration

See `configs.yml` for all options.

## Build & Run

```bash
docker build -t core-switch:dev .
docker run -p 8585:8585 core-switch:dev
```
