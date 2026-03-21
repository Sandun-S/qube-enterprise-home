# Qube Enterprise — Learning Path

This folder contains study notes for understanding how Qube Enterprise works.
Written for someone who knows basic programming but is new to Go, Docker Swarm, and IoT backend systems.

## Reading order

| # | File | What you learn |
|---|---|---|
| 1 | `01-architecture.md` | What Qube Enterprise does and how the pieces fit together |
| 2 | `02-go-basics.md` | Go concepts used throughout this codebase |
| 3 | `03-postgres-schema.md` | Database tables, why each one exists, what flows through them |
| 4 | `04-cloud-api.md` | How the Cloud API handles requests — auth, routing, handlers |
| 5 | `05-tpapi.md` | The TP-API — what Qubes talk to, how HMAC auth works |
| 6 | `06-conf-agent.md` | The conf-agent — self-registration, hash sync, compose deploy |
| 7 | `07-csv-generation.md` | How sensor templates turn into real CSV files for gateways |
| 8 | `08-telemetry-pipeline.md` | Data flow: device → gateway → core-switch → InfluxDB → Postgres |
| 9 | `09-docker-swarm.md` | Docker Swarm concepts — why Qube uses it, how stack deploy works |
| 10 | `10-cicd.md` | GitHub Actions, image registries, deploying to Azure |

---

Each file has:
- Concept explanation in plain language
- The actual code from this project that implements it
- Why it was designed this way
- Common questions answered
