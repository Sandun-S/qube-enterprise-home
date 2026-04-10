import time
import json
import random
import argparse
import paho.mqtt.client as mqtt

def generate_data():
    """Generates random environment telemetry data."""
    value = round(random.uniform(0.0, 100.0), 2)
    temperature = round(random.uniform(20.0, 35.0), 2)
    humidity = round(random.uniform(30.0, 80.0), 1)
    pressure = round(random.uniform(980.0, 1030.0), 1)
    
    return {
        "value": value,
        "temperature": temperature,
        "humidity": humidity,
        "pressure": pressure,
        "timestamp": int(time.time())
    }

def main():
    parser = argparse.ArgumentParser(description="Qube Enterprise MQTT Data Feeder")
    parser.add_argument("--host", default="localhost", help="MQTT broker host (default: localhost)")
    parser.add_argument("--port", type=int, default=1883, help="MQTT broker port (default: 1883)")
    parser.add_argument("--topic", default="qube/sensors/environment", help="MQTT topic to publish to (default: qube/sensors/environment)")
    parser.add_argument("--interval", type=int, default=5, help="Interval between publishes in seconds (default: 5)")
    
    args = parser.parse_args()

    client = mqtt.Client()
    
    print(f"Connecting to MQTT broker at {args.host}:{args.port}...")
    try:
        client.connect(args.host, args.port, 60)
    except Exception as e:
        print(f"Failed to connect: {e}")
        return

    client.loop_start()

    print(f"Starting feeder. Publishing to topic '{args.topic}' every {args.interval}s.")
    print("Press Ctrl+C to stop.")

    try:
        while True:
            data = generate_data()
            payload = json.dumps(data)
            
            result = client.publish(args.topic, payload)
            status = result[0]
            if status == 0:
                print(f"Published: {payload}")
            else:
                print(f"Failed to send message to topic {args.topic}")
            
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\nStopping feeder...")
    finally:
        client.loop_stop()
        client.disconnect()

if __name__ == "__main__":
    main()
