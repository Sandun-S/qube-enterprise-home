# Qube Enterprise — Full Implementation Plan

> **Reference**: [qube_enterprise_feature_plan.md](file:///d:/MITesp/Projects/09_Qube_Enterprice/qube_enterprise_feature_plan.md)
> **Date**: 2026-03-19
> **Status**: Draft

---

## Overview

This document translates every module from the feature plan into concrete, actionable implementation tasks — organized by the 6 phases defined in Module 14. Each task includes the target component, files to create/modify, dependencies, and acceptance criteria.

### Existing Codebase (Go microservices)

| Service | Path | Role |
|---|---|---|
| `tp-api` | `tp-api/` | Cloud ↔ Qube bridge API |
| `conf-agent` | `conf-agent/` | On-Qube config sync daemon |
| `core-switch` | `core-switch/` | Data router on Qube |
| `modbus-gateway` | `modbus-gateway/` | Modbus TCP protocol gateway |
| `opc-ua-gateway` | `opc-ua-gateway/` | OPC-UA protocol gateway |
| `influx-to-sql` | `influx-to-sql/` | InfluxDB → Cloud Postgres relay |
| `con-checker` | `con-checker/` | Connection health checker |
| `influxdb-relay` | `influxdb-relay/` | InfluxDB write relay |
| `snmp-gateway` | `snmp-gateway/` | SNMP protocol gateway |
| `conf-api` | `conf-api/` | Cloud management API |

### New Components to Build

| Component | Tech | Purpose |
|---|---|---|
| **Cloud API** | Go + Gin/Echo + GORM | REST API for frontend (Module 13) |
| **Cloud DB** | PostgreSQL + golang-migrate | All state tables (Module 1) |
| **Frontend** | React/Next.js + TypeScript | Dashboard UI (Module 11) |
| **MQTT Gateway** | Go | MQTT protocol gateway (Module 5) |

---

## Phase 1 — Foundation (Weeks 1–3)

> **Goal**: Data model, Qube claiming, TP-API sync, Conf-Agent polling, basic frontend.

### 1.1 Database Schema & Migrations

**Component**: `cloud-api/migrations/`
**Ref**: Module 1 — Data Model

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.1.1 | Set up PostgreSQL database + migration tooling | `docker-compose.yml`, `cloud-api/migrations/000001_init.up.sql` | `golang-migrate` runs clean up/down |
| 1.1.2 | Create `organisations` table | `000002_organisations.up.sql` | UUID PK, name, mqtt_namespace, created_at |
| 1.1.3 | Create `users` table | `000003_users.up.sql` | UUID PK, org_id FK, email unique, role enum (admin/editor/viewer), password_hash |
| 1.1.4 | Create `qubes` table | `000004_qubes.up.sql` | text PK (Q-XXXX), org_id FK nullable, auth_token_hash, status enum, last_seen |
| 1.1.5 | Create `gateways` table | `000005_gateways.up.sql` | UUID PK, qube_id FK, protocol enum, config_json JSONB |
| 1.1.6 | Create `sensors` table | `000006_sensors.up.sql` | UUID PK, gateway_id FK, template_id FK, address_params JSONB, tags_json JSONB |
| 1.1.7 | Create `sensor_templates` table | `000007_sensor_templates.up.sql` | UUID PK, org_id nullable, config_json, ui_mapping_json, influx_fields_json |
| 1.1.8 | Create `services` table | `000008_services.up.sql` | UUID PK, qube_id FK, image, port, env_json JSONB, gateway_id FK nullable |
| 1.1.9 | Create `service_csv_rows` table | `000009_service_csv_rows.up.sql` | UUID PK, service_id FK, sensor_id FK nullable, csv_type enum, row_data JSONB |
| 1.1.10 | Create `config_state` table | `000010_config_state.up.sql` | UUID PK, qube_id FK, hash text, config_snapshot JSONB |
| 1.1.11 | Create `qube_commands` table | `000011_qube_commands.up.sql` | UUID PK, command enum, payload JSONB, status enum, result JSONB |
| 1.1.12 | Create `sensor_readings` table (hypertable-ready) | `000012_sensor_readings.up.sql` | time, qube_id, sensor_id, field_key, value, unit — partitioned by time |
| 1.1.13 | Seed global sensor templates | `000013_seed_templates.up.sql` | Schneider PM5100 Modbus template seeded |

### 1.2 Cloud API Scaffold

**Component**: `cloud-api/`
**Ref**: Module 12, Module 13

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.2.1 | Initialize Go module with Gin/Echo, GORM, config loader | `cloud-api/main.go`, `go.mod`, `internal/config/` | App starts, connects to Postgres |
| 1.2.2 | Define GORM models for all tables | `internal/models/*.go` | Models match migration schema exactly |
| 1.2.3 | Implement JWT auth middleware | `internal/middleware/auth.go` | JWT validate, extract user_id + org_id + role |
| 1.2.4 | Implement role-based authorization middleware | `internal/middleware/rbac.go` | Admin/Editor/Viewer permissions per Module 12 |
| 1.2.5 | Implement org-scoping middleware | `internal/middleware/org_scope.go` | Every query auto-filters by org_id |
| 1.2.6 | Set up error handling & response format | `internal/response/` | Consistent `{data, error, message}` envelope |
| 1.2.7 | Add Dockerfile + docker-compose service | `cloud-api/Dockerfile`, update `docker-compose.yml` | Builds and runs in Docker |

### 1.3 Auth & User Management

**Component**: `cloud-api/internal/handlers/auth.go`
**Ref**: Module 12 — Security & Auth

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.3.1 | `POST /api/v1/auth/register` — create org + admin user | `handlers/auth.go`, `services/auth_service.go` | Org created, user with role=admin, JWT returned |
| 1.3.2 | `POST /api/v1/auth/login` — email/password → JWT + refresh | `handlers/auth.go` | bcrypt verify, JWT (1h) + refresh (7d) |
| 1.3.3 | `POST /api/v1/auth/refresh` — refresh token rotation | `handlers/auth.go` | New JWT issued, old refresh invalidated |
| 1.3.4 | `GET /api/v1/orgs/me` — get current org | `handlers/org.go` | Returns org details from JWT org_id |
| 1.3.5 | `PUT /api/v1/orgs/me` — update org | `handlers/org.go` | Admin-only, updates name/mqtt_namespace |
| 1.3.6 | `GET/POST/DELETE /api/v1/orgs/me/users` — user invite CRUD | `handlers/users.go` | Admin can invite/remove, emails must be unique |

### 1.4 Qube Claiming System

**Component**: `cloud-api/internal/handlers/qubes.go`
**Ref**: Module 2 — Qube Claiming & Org Management

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.4.1 | Factory pre-register endpoint (internal) | `handlers/qubes.go` | `POST /internal/qubes/register` — inserts unclaimed Qube |
| 1.4.2 | `POST /api/v1/qubes/claim` | `handlers/qubes.go`, `services/qube_service.go` | Validates unclaimed, sets org_id, generates auth_token_hash via HMAC |
| 1.4.3 | `DELETE /api/v1/qubes/:id/claim` — unclaim | `handlers/qubes.go` | Admin-only, clears org_id, invalidates token |
| 1.4.4 | `GET /api/v1/qubes` — list org's qubes | `handlers/qubes.go` | Filtered by org_id, includes status pill |
| 1.4.5 | `GET /api/v1/qubes/:id` — qube detail | `handlers/qubes.go` | Status, last_seen, config hash, location |
| 1.4.6 | `GET /api/v1/qubes/:id/status` — online/offline check | `handlers/qubes.go` | Based on last_seen vs 2min threshold |
| 1.4.7 | Background job: mark offline qubes | `internal/jobs/qube_status.go` | Runs every 60s, sets status=offline if last_seen > 2min |

### 1.5 TP-API Sync Endpoints

**Component**: `tp-api/`
**Ref**: Module 8 — TP-API Sync Engine

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.5.1 | Qube auth token validation middleware | `tp-api/internal/middleware/qube_auth.go` | HMAC recompute + compare, 401 on mismatch |
| 1.5.2 | `GET /v1/sync/state` — return current hash | `tp-api/internal/handlers/sync.go` | Returns `{qube_id, hash, updated_at}` |
| 1.5.3 | `GET /v1/sync/config` — return full config | `tp-api/internal/handlers/sync.go` | Returns docker_compose_yml, csv_files map, env_files map |
| 1.5.4 | `POST /v1/heartbeat` — update last_seen | `tp-api/internal/handlers/heartbeat.go` | Sets `last_seen = now()`, returns 200 |
| 1.5.5 | Config hash computation service | `cloud-api/internal/services/hash_service.go` | SHA-256 of sorted services + gateways + csv_rows |
| 1.5.6 | Docker Compose YAML generator | `cloud-api/internal/services/compose_generator.go` | Builds valid YAML from services table for a Qube |

### 1.6 Conf-Agent Enhancement

**Component**: `conf-agent/`
**Ref**: Module 8

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.6.1 | Poll loop: call `/v1/sync/state` every 60s | `conf-agent/internal/sync/poller.go` | Compares remote hash with local hash file |
| 1.6.2 | On hash mismatch: call `/v1/sync/config` | `conf-agent/internal/sync/config.go` | Downloads full config payload |
| 1.6.3 | Write docker-compose.yml + CSV files to disk | `conf-agent/internal/sync/writer.go` | Files written atomically (write to .tmp then rename) |
| 1.6.4 | Run `docker stack deploy` after write | `conf-agent/internal/sync/deployer.go` | Stack deployed, local hash updated |
| 1.6.5 | Heartbeat on every poll cycle | `conf-agent/internal/sync/heartbeat.go` | `POST /v1/heartbeat` called successfully |
| 1.6.6 | Auth token from env/config | `conf-agent/internal/config/config.go` | `QUBE_AUTH_TOKEN` env var used in Bearer header |

### 1.7 Basic Frontend

**Component**: `frontend/`
**Ref**: Module 11

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 1.7.1 | Initialize React/Next.js project with TypeScript | `frontend/` scaffold | App runs on `npm run dev` |
| 1.7.2 | Auth pages: login, register | `pages/auth/` | JWT stored in httpOnly cookie or localStorage |
| 1.7.3 | Org dashboard: list claimed Qubes | `pages/dashboard/` | Shows Qube list with online/offline status pills |
| 1.7.4 | Claim Qube modal | `components/ClaimQubeModal.tsx` | Enter Qube ID → POST claim → success/error |
| 1.7.5 | Qube detail page (overview tab) | `pages/qubes/[id]/` | Status, location, last_seen, config hash |
| 1.7.6 | API client service with JWT interceptor | `lib/api.ts` | Axios/fetch wrapper, auto-attach JWT, handle 401 refresh |

---

## Phase 2 — Gateway & Sensor Automation (Weeks 4–6)

> **Goal**: Gateway CRUD, auto-service creation, Modbus templates, sensor CRUD, CSV auto-generation.

### 2.1 Gateway Management

**Component**: `cloud-api/internal/handlers/gateways.go`
**Ref**: Module 3

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 2.1.1 | `POST /api/v1/qubes/:id/gateways` — create gateway | `handlers/gateways.go`, `services/gateway_service.go` | Creates gateway + auto-creates service record with protocol-default image |
| 2.1.2 | `GET /api/v1/qubes/:id/gateways` — list gateways | `handlers/gateways.go` | Filtered by qube, includes protocol badge |
| 2.1.3 | `GET /api/v1/gateways/:id` — detail | `handlers/gateways.go` | Full gateway with associated sensors count |
| 2.1.4 | `PUT /api/v1/gateways/:id` — update | `handlers/gateways.go` | Updates config, triggers hash recalc |
| 2.1.5 | `DELETE /api/v1/gateways/:id` — delete cascade | `handlers/gateways.go` | Removes gateway + service + sensors + csv_rows, hash recalc |
| 2.1.6 | Auto-service creation per protocol | `services/gateway_service.go` | modbus→modbus-gateway image, mqtt→mqtt-gateway image, opcua→opcua-gateway image |
| 2.1.7 | Protocol-specific config validation | `internal/validators/gateway_config.go` | Validate Modbus (host/port), MQTT (broker_url), OPC-UA (endpoint_url) |

### 2.2 Sensor Template System (Modbus First)

**Component**: `cloud-api/internal/handlers/templates.go`
**Ref**: Module 6

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 2.2.1 | `GET /api/v1/templates` — list (global + org) | `handlers/templates.go` | Filter by protocol, show global + org-owned |
| 2.2.2 | `POST /api/v1/templates` — create org template | `handlers/templates.go` | Validates config_json structure per protocol |
| 2.2.3 | `PUT /api/v1/templates/:id` — update (org only) | `handlers/templates.go` | Bumps version, org ownership check |
| 2.2.4 | `DELETE /api/v1/templates/:id` — delete (org only) | `handlers/templates.go` | Cannot delete if sensors reference it |
| 2.2.5 | `POST /api/v1/templates/:id/clone` — clone global→org | `handlers/templates.go` | Deep copies template with new UUID, org_id set |
| 2.2.6 | `GET /api/v1/templates/:id/preview-csv` — preview | `handlers/templates.go` | Returns CSV text for given address_params |

### 2.3 Sensor CRUD & CSV Auto-Generation

**Component**: `cloud-api/internal/handlers/sensors.go`
**Ref**: Module 4

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 2.3.1 | `POST /api/v1/gateways/:id/sensors` — create sensor | `handlers/sensors.go`, `services/sensor_service.go` | Creates sensor + auto-generates service_csv_rows from template |
| 2.3.2 | CSV row generation from Modbus template | `services/csv_generator.go` | Produces rows: Equipment, Reading, RegType, Address+offset, type, Output, Table, Tags |
| 2.3.3 | `GET /api/v1/gateways/:id/sensors` — list sensors | `handlers/sensors.go` | Per-gateway, includes template name and variable count |
| 2.3.4 | `PUT /api/v1/sensors/:id` — update sensor | `handlers/sensors.go` | Updates params, regenerates csv_rows, hash recalc |
| 2.3.5 | `DELETE /api/v1/sensors/:id` — delete sensor | `handlers/sensors.go` | Removes sensor + csv_rows, hash recalc |
| 2.3.6 | `POST /api/v1/sensors/:id/sync-template` — resync | `handlers/sensors.go` | Regenerates csv_rows from latest template version |
| 2.3.7 | Hash recalculation trigger on every mutation | `services/hash_service.go` | Auto-called after any gateway/sensor/csv change |

### 2.4 Service & CSV Row Management

**Component**: `cloud-api/internal/handlers/services.go`
**Ref**: Module 7

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 2.4.1 | `GET /api/v1/qubes/:id/services` — list services | `handlers/services.go` | All containers for a Qube |
| 2.4.2 | `POST /api/v1/qubes/:id/services` — add custom service | `handlers/services.go` | For non-gateway containers |
| 2.4.3 | `GET /api/v1/services/:id/csv` — render CSV text | `handlers/services.go` | Returns assembled CSV from service_csv_rows |
| 2.4.4 | `GET /api/v1/services/:id/csv/rows` — rows as JSON | `handlers/services.go` | JSON array of row_data |
| 2.4.5 | `POST/PUT/DELETE /api/v1/services/:id/csv/rows` — manual CRUD | `handlers/services.go` | Manual row management, hash recalc |
| 2.4.6 | `POST /api/v1/services/:id/csv/import` — bulk CSV upload | `handlers/services.go` | Parse CSV file → insert rows → hash recalc |

### 2.5 Frontend — Gateway & Sensor UI

**Component**: `frontend/`
**Ref**: Module 11

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 2.5.1 | Qube detail — Gateways tab | `pages/qubes/[id]/gateways/` | List gateways with protocol badges |
| 2.5.2 | Add Gateway modal with protocol-specific fields | `components/AddGatewayModal.tsx` | Dynamic fields per protocol |
| 2.5.3 | Gateway detail page → sensor list | `pages/gateways/[id]/` | List sensors under gateway |
| 2.5.4 | Add Sensor flow: select template → enter params → preview CSV | `components/AddSensorWizard.tsx` | 3-step wizard with CSV preview before confirm |
| 2.5.5 | Manual CSV editor in Services tab | `components/CsvEditor.tsx` | Table view, add/edit/delete rows |
| 2.5.6 | "Deploying in ≤60s" status indicator | `components/DeployStatusBadge.tsx` | Shows after any config change, clears on next sync |

---

## Phase 3 — MQTT & OPC-UA Support (Weeks 7–8)

> **Goal**: MQTT and OPC-UA gateway types, template editor for all protocols.

### 3.1 MQTT Gateway Service

**Component**: `mqtt-gateway/` (NEW)
**Ref**: Module 5 — MQTT

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 3.1.1 | Initialize Go service scaffold | `mqtt-gateway/main.go`, `Dockerfile` | Connects to MQTT broker, reads topics.csv |
| 3.1.2 | Topics.csv parser | `mqtt-gateway/internal/csv/parser.go` | Parses topic, variable, json_path, output, table, tags |
| 3.1.3 | MQTT subscription manager | `mqtt-gateway/internal/mqtt/subscriber.go` | Subscribes to all topics from CSV, handles reconnect |
| 3.1.4 | JSON path value extractor | `mqtt-gateway/internal/extract/jsonpath.go` | Extracts values using `$.data.xxx` paths |
| 3.1.5 | Coreswitch output sender | `mqtt-gateway/internal/output/coreswitch.go` | Sends structured JSON to coreswitch HTTP endpoint |

### 3.2 MQTT CSV Generation

**Component**: `cloud-api/`
**Ref**: Module 5

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 3.2.1 | MQTT template config_json validator | `validators/mqtt_template.go` | Validates variables array with name, json_path, unit, influx_field |
| 3.2.2 | MQTT CSV row generator | `services/csv_generator.go` | Generates topics.csv rows from template + address_params (topic_suffix) |
| 3.2.3 | MQTT sensor address_params schema | `validators/sensor_params.go` | Validates topic_suffix, full_topic fields |

### 3.3 OPC-UA CSV Generation

**Component**: `cloud-api/`, `opc-ua-gateway/`
**Ref**: Module 5

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 3.3.1 | OPC-UA template config_json validator | `validators/opcua_template.go` | Validates nodes array with reading, output, table, type |
| 3.3.2 | OPC-UA CSV row generator | `services/csv_generator.go` | Generates nodes.csv from template + address_params (node_ids) |
| 3.3.3 | OPC-UA sensor address_params schema | `validators/sensor_params.go` | Validates node_ids array |
| 3.3.4 | Verify existing `opc-ua-gateway` reads nodes.csv | `opc-ua-gateway/` | Confirm or refactor CSV parsing |

### 3.4 Template Editor UI

**Component**: `frontend/`
**Ref**: Module 11

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 3.4.1 | Template Manager page | `pages/templates/` | Global (read-only) + org (CRUD), clone button |
| 3.4.2 | Template editor — Variables tab | `components/TemplateEditor/VariablesTab.tsx` | Add/edit/delete registers (Modbus), variables (MQTT), nodes (OPC-UA) |
| 3.4.3 | Template editor — InfluxDB Fields tab | `components/TemplateEditor/InfluxFieldsTab.tsx` | Map field keys → units, display labels |
| 3.4.4 | Template editor — Dashboard Layout tab | `components/TemplateEditor/DashboardTab.tsx` | Define panel types (gauge, timeseries, stat) with field bindings |
| 3.4.5 | Protocol-specific form rendering | `components/TemplateEditor/ProtocolForms.tsx` | Dynamically shows correct fields per protocol |

---

## Phase 4 — Commands & Telemetry (Weeks 9–10)

> **Goal**: Command queue, telemetry pipeline, influx-to-sql sensor mapping.

### 4.1 Command Queue System

**Component**: `cloud-api/`, `tp-api/`, `conf-agent/`
**Ref**: Module 9

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 4.1.1 | `POST /api/v1/qubes/:id/commands` — dispatch command | `cloud-api/handlers/commands.go` | Validates command enum, inserts with status=pending |
| 4.1.2 | `GET /api/v1/qubes/:id/commands` — list recent | `cloud-api/handlers/commands.go` | Paginated, ordered by created_at desc |
| 4.1.3 | `GET /api/v1/commands/:id` — poll single result | `cloud-api/handlers/commands.go` | Frontend polls every 2s until executed/failed |
| 4.1.4 | `POST /v1/commands/poll` — TP-API returns pending cmds | `tp-api/handlers/commands.go` | Returns batch of pending commands for the Qube |
| 4.1.5 | `POST /v1/commands/:id/ack` — Qube reports result | `tp-api/handlers/commands.go` | Updates status + result + executed_at |
| 4.1.6 | Conf-Agent: poll + execute commands | `conf-agent/internal/commands/executor.go` | Executes ping, restart_service, restart_qube, get_logs, list_containers |
| 4.1.7 | Command timeout job (120s) | `cloud-api/jobs/command_timeout.go` | Marks pending commands as timeout if not acked in 120s |

### 4.2 Telemetry Pipeline

**Component**: `tp-api/`, `influx-to-sql/`, `cloud-api/`
**Ref**: Module 10

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 4.2.1 | `POST /v1/telemetry/ingest` — bulk readings insert | `tp-api/handlers/telemetry.go` | Batch insert into sensor_readings, max 1000 rows/call |
| 4.2.2 | Sensor map in `/v1/sync/config` response | `tp-api/handlers/sync.go` | Include `sensor_map: {"Equipment.Reading": "sensor_uuid"}` |
| 4.2.3 | `influx-to-sql`: read sensor_map from config | `influx-to-sql/internal/config/` | Parses sensor_map for ID resolution |
| 4.2.4 | `influx-to-sql`: query InfluxDB last 60s + push | `influx-to-sql/internal/pipeline/` | Reads measurements, maps to sensor_id, calls /v1/telemetry/ingest |
| 4.2.5 | `GET /api/v1/data/readings` — query historical data | `cloud-api/handlers/data.go` | Filter by sensor_id, field, time range, aggregation interval |
| 4.2.6 | `GET /api/v1/data/sensors/:id/latest` — last values | `cloud-api/handlers/data.go` | Latest value for each field_key of a sensor |
| 4.2.7 | `GET /api/v1/data/qubes/:id/summary` — all sensors | `cloud-api/handlers/data.go` | All sensors + their latest values for a Qube |

### 4.3 Frontend — Commands & Status

**Component**: `frontend/`
**Ref**: Module 11

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 4.3.1 | Commands tab in Qube detail | `pages/qubes/[id]/commands/` | Command history list with status badges |
| 4.3.2 | Send command panel | `components/SendCommandPanel.tsx` | Dropdown for command type, payload fields, submit |
| 4.3.3 | Command result polling & display | `components/CommandResult.tsx` | Polls every 2s, shows result JSON or timeout |
| 4.3.4 | Quick actions on dashboard (ping, reload) | `components/QuickActions.tsx` | One-click ping/reload on Qube card |

---

## Phase 5 — Dashboards & Templates (Weeks 11–12)

> **Goal**: Template manager polish, Grafana auto-provisioning, telemetry sparklines.

### 5.1 Grafana Auto-Provisioning

**Component**: `cloud-api/internal/services/grafana.go`
**Ref**: Module 10

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 5.1.1 | Grafana API client | `services/grafana/client.go` | Create/update/delete dashboards via Grafana HTTP API |
| 5.1.2 | Dashboard builder from ui_mapping_json | `services/grafana/builder.go` | Converts panel definitions to Grafana JSON model |
| 5.1.3 | Auto-provision on sensor create | `services/sensor_service.go` | If org has Grafana API key, create/update dashboard |
| 5.1.4 | Org Grafana settings endpoint | `handlers/org.go` | `PUT /api/v1/orgs/me/grafana` — store API key + URL |

### 5.2 Telemetry Visualization

**Component**: `frontend/`
**Ref**: Module 11

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 5.2.1 | Sensor detail page with data charts | `pages/sensors/[id]/` | Time-series chart (Recharts/Chart.js), date range picker |
| 5.2.2 | Sparklines on Qube summary page | `components/SensorSparkline.tsx` | Mini charts showing last 24h trend |
| 5.2.3 | Real-time value indicators | `components/LiveValue.tsx` | Polls latest value, shows with unit and trend arrow |
| 5.2.4 | Data export (CSV download) | `components/DataExport.tsx` | Download readings as CSV for selected time range |

### 5.3 Template Editor Polish

**Component**: `frontend/`

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 5.3.1 | Template import/export (JSON) | `components/TemplateImportExport.tsx` | Upload JSON → create template, download as JSON |
| 5.3.2 | Template usage stats | `pages/templates/[id]/` | Shows count of sensors using this template |
| 5.3.3 | Bulk template sync for sensors | `components/BulkSyncTemplate.tsx` | "Sync all sensors" button when template updated |

---

## Phase 6 — Polish & Scale (Weeks 13–14)

> **Goal**: RBAC enforcement, rate limiting, audit logs, transfer, bulk ops.

### 6.1 Security Hardening

**Component**: `cloud-api/`, `tp-api/`
**Ref**: Module 12

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 6.1.1 | RBAC enforcement on all endpoints | `middleware/rbac.go` | Permission checks match Module 12 matrix |
| 6.1.2 | Rate limiting: TP-API (10 req/min/qube) | `tp-api/middleware/rate_limit.go` | Redis-backed rate limiter |
| 6.1.3 | Rate limiting: Commands (30/min/org) | `cloud-api/middleware/rate_limit.go` | Per-org rate limit on command dispatch |
| 6.1.4 | Rate limiting: Telemetry (1000 rows, 60 calls/min) | `tp-api/middleware/rate_limit.go` | Per-qube telemetry rate limit |
| 6.1.5 | Request logging middleware | `middleware/logging.go` | Structured logs with user_id, org_id, method, path, status, duration |

### 6.2 Qube Transfer & Audit

**Component**: `cloud-api/`
**Ref**: Module 2

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 6.2.1 | `POST /api/v1/qubes/:id/transfer` — transfer Qube | `handlers/qubes.go` | Admin-only, moves org_id, rotates auth_token_hash, logs event |
| 6.2.2 | Create `audit_log` table + migration | `migrations/000014_audit_log.up.sql` | who (user_id), what (action), target (entity_id), when |
| 6.2.3 | Audit logging service | `services/audit.go` | Log claim, unclaim, transfer, gateway/sensor add/delete |
| 6.2.4 | `GET /api/v1/audit-logs` — view audit trail | `handlers/audit.go` | Paginated, filterable by action/user/time |

### 6.3 Bulk Operations

**Component**: `cloud-api/`

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 6.3.1 | `POST /api/v1/qubes/bulk/command` — send cmd to N qubes | `handlers/bulk.go` | Accepts array of qube_ids + command |
| 6.3.2 | `POST /api/v1/qubes/bulk/deploy-config` — push same config | `handlers/bulk.go` | Apply gateway+sensor config from source Qube to targets |
| 6.3.3 | Bulk operations UI | `components/BulkOperations.tsx` | Multi-select qubes, choose action, confirm |

### 6.4 Frontend Polish

**Component**: `frontend/`

| # | Task | Files | Acceptance Criteria |
|---|---|---|---|
| 6.4.1 | Responsive design & mobile support | All pages | Works on tablet + mobile |
| 6.4.2 | Dark/light theme toggle | `lib/theme.ts` | System preference detection + manual toggle |
| 6.4.3 | Toast notifications for async operations | `components/Toasts.tsx` | Success/error/deploying toasts |
| 6.4.4 | Loading skeletons & error boundaries | All pages | Graceful loading states and error displays |
| 6.4.5 | Search & filter across all list views | All list pages | Debounced search, protocol/status filters |

---

## Infrastructure & DevOps

### Docker Compose (Development)

| Service | Image | Port |
|---|---|---|
| `postgres` | `postgres:16` | 5432 |
| `redis` | `redis:7` | 6379 |
| `cloud-api` | Build from `cloud-api/` | 8000 |
| `tp-api` | Build from `tp-api/` | 8001 |
| `frontend` | Build from `frontend/` | 3000 |
| `grafana` | `grafana/grafana:latest` | 3001 |

### CI/CD Pipeline

| Step | Tool | Trigger |
|---|---|---|
| Lint + Test | GitHub Actions / GitLab CI | Every push |
| Build Docker images | Docker Build | Merge to main |
| Run migrations | `golang-migrate` | Pre-deploy hook |
| Deploy | Docker Compose / K8s | Tag release |

---

## Task Summary

| Phase | Tasks | Weeks | Key Deliverables |
|---|---|---|---|
| **Phase 1** | 37 tasks | 1–3 | DB, Auth, Claiming, TP-API sync, basic frontend |
| **Phase 2** | 26 tasks | 4–6 | Gateways, Modbus sensors, CSV generation, sensor UI |
| **Phase 3** | 14 tasks | 7–8 | MQTT gateway, OPC-UA CSV, template editor |
| **Phase 4** | 14 tasks | 9–10 | Command queue, telemetry pipeline, data API |
| **Phase 5** | 11 tasks | 11–12 | Grafana auto-provision, charts, template polish |
| **Phase 6** | 13 tasks | 13–14 | RBAC, rate limits, audit logs, bulk ops, UI polish |
| **Total** | **115 tasks** | **14 weeks** | Full Qube Enterprise platform |

---

> **Next Step**: Review this plan and confirm Phase 1 to begin implementation.
