# 06 - Deployment and Testing Scenarios

You asked about how the project is run and tested. There are three main scenarios you need to know about.

## Scenario 1: `docker-compose.dev.yml` (Easiest Local Testing)
This is for developing the stack locally on your Windows machine without hardware.
- It spins up a fake unified environment containing *everything*: Postgres, Cloud API, InfluxDB, Mosquitto, `conf-agent`, `enterprise-influx-to-sql`, fake Modbus/SNMP simulators, and a Test UI.
- Normally `conf-agent` and the DB are on opposite sides of the world, but here they run on the same virtual Docker network.
- **How to run:** 
  `docker compose -f docker-compose.dev.yml up -d`
- Then open `http://localhost:8888` for the dev UI.
- Use `test/test_api.sh` or the Test UI to simulate adding devices. You can read `TESTING.md` for specific manual tests.

## Scenario 2: 2 VMs using Multipass (Network Split Testing)
This accurately tests the network gap between Cloud and Qube.
- **Multipass** is an Ubuntu tool for spinning up quick lightweight VMs.
- You spin up two VMs: `cloud-vm` and `qube-vm`.
- The `setup-cloud.sh` script installs Go, Postgres, and the Cloud Services on `cloud-vm`.
- The `setup-qube.sh` script installs Docker, Go, `conf-agent`, and `enterprise-influx-to-sql` on `qube-vm`.
- The `qube-vm` talks to `cloud-vm` strictly over HTTP just like it would over the internet.
- Essential for testing firewalls, networking connectivity drops, and testing real Swarm logic (`docker swarm init`).

## Scenario 3: Real-World Deployment
This is what happens when you ship devices to customers.
- The Cloud Service runs on AWS, Azure, or private enterprise servers.
- The Gitlab CI/CD pipeline compiles the Go binaries (`go build`) and packages them into Docker images pushed to a container registry.
- At the factory, Raspberry Pis/Kadas units are flashed with an OS image.
- A script (`write-to-database.sh`) creates a `/boot/mit.txt` with a unique ID and registers it centrally.
- When the customer powers on the Qube in their facility and plugs in an Ethernet cable, `conf-agent` boots up natively, calls out to the cloud IP, registers itself, downloads all Docker images from the private registry, and spins up the stack automatically.

Congratulations! You now understand the Qube Enterprise Architecture. Feel free to explore the Go code files (`main.go` in each directory), read through `TESTING.md` to see exactly how to test it locally.
