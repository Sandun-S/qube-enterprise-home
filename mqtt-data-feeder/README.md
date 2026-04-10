# MQTT Data Feeder

This script simulates a sensor by publishing random environment data (temperature, humidity, pressure) to an MQTT broker. Use it to test the **MQTT Reader** in your Qube Enterprise edge stack.

## Installation

Ensure you have Python 3 installed, then install the `paho-mqtt` dependency:

```bash
pip install paho-mqtt
```

## Usage

Run the script from your desktop:

```bash
python mqtt_feeder.py --host localhost --topic qube/sensors/environment
```

### Options:
- `--host`: The IP address of the MQTT broker. Use `localhost` if running Mosquitto on your desktop.
- `--port`: The port of the MQTT broker (default: 1883).
- `--topic`: The MQTT topic to publish to (default: `qube/sensors/environment`).
- `--interval`: Seconds between each message (default: 5).

## Testing with Multipass

If you are running the Qube edge stack in a Multipass VM:

1. **Start a Broker on your desktop**: Use Docker for the easiest setup:
   ```bash
   docker run -d -p 1883:1883 -v ./test/mosquitto/mosquitto.conf:/mosquitto/config/mosquitto.conf eclipse-mosquitto
   ```
2. **Run the Feeder**:
   ```bash
   python mqtt_feeder.py --host localhost
   ```
3. **Configure the MQTT Reader**:
   In the Qube portal, add an MQTT Reader and set the **Broker Host** to your **Desktop IP** (usually the gateway IP for the VM, e.g., `172.x.x.1`).
4. **Subscribe**:
   Add a sensor using the topic `qube/sensors/environment` and JSON paths like `$.temperature`, `$.humidity`, etc.
