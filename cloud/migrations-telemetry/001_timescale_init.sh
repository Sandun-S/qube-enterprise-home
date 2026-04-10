#!/bin/bash
# 001_timescale_init.sh — Qube Enterprise v2 Telemetry Database (qubedata)
# Runs after all management DB migrations (mounted as 010_timescale_init.sh).
# Uses a shell script instead of .sql so we can reliably switch databases.
# The postgres \c metacommand in .sql files is unreliable across PG image versions.

set -e

PGUSER="${POSTGRES_USER:-qubeadmin}"
PGPASSWORD="${POSTGRES_PASSWORD:-qubepass}"
export PGPASSWORD

# ── Create qubedata database ─────────────────────────────────────────────────
psql -v ON_ERROR_STOP=1 -U "$PGUSER" -d postgres <<-SQL
    SELECT 'CREATE DATABASE qubedata'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'qubedata')
    \gexec
SQL

# ── Enable TimescaleDB + create schema in qubedata ───────────────────────────
psql -v ON_ERROR_STOP=1 -U "$PGUSER" -d qubedata <<-SQL
    CREATE EXTENSION IF NOT EXISTS timescaledb;

    -- ── Sensor Readings Hypertable ──────────────────────────────
    -- Written by TP-API /v1/telemetry/ingest.
    -- Partitioned by time (7-day chunks) for efficient queries and retention.
    CREATE TABLE IF NOT EXISTS sensor_readings (
        time        TIMESTAMPTZ NOT NULL,
        qube_id     TEXT NOT NULL,
        sensor_id   UUID NOT NULL,
        field_key   TEXT NOT NULL,
        value       DOUBLE PRECISION NOT NULL,
        unit        TEXT NOT NULL DEFAULT '',
        tags        JSONB NOT NULL DEFAULT '{}'
    );

    -- Convert to hypertable (7-day chunks)
    SELECT create_hypertable('sensor_readings', 'time',
        chunk_time_interval => INTERVAL '7 days',
        if_not_exists => TRUE
    );

    -- Primary query pattern: "all readings for sensor X in time range"
    CREATE INDEX IF NOT EXISTS idx_readings_sensor_time
        ON sensor_readings (sensor_id, time DESC);

    -- Dashboard pattern: "all readings for qube X in last N minutes"
    CREATE INDEX IF NOT EXISTS idx_readings_qube_time
        ON sensor_readings (qube_id, time DESC);

    -- Field-specific queries: "all voltage readings across all sensors"
    CREATE INDEX IF NOT EXISTS idx_readings_field_time
        ON sensor_readings (field_key, time DESC);

    -- ── Compression Policy ──────────────────────────────────────
    ALTER TABLE sensor_readings SET (
        timescaledb.compress,
        timescaledb.compress_segmentby = 'qube_id, sensor_id',
        timescaledb.compress_orderby = 'time DESC'
    );

    SELECT add_compression_policy('sensor_readings', INTERVAL '30 days',
        if_not_exists => TRUE);

    -- ── Retention Policy ────────────────────────────────────────
    SELECT add_retention_policy('sensor_readings', INTERVAL '365 days',
        if_not_exists => TRUE);

    -- ── Continuous Aggregates (hourly rollups) ───────────────────
    CREATE MATERIALIZED VIEW IF NOT EXISTS sensor_readings_hourly
    WITH (timescaledb.continuous) AS
    SELECT
        time_bucket('1 hour', time) AS bucket,
        qube_id,
        sensor_id,
        field_key,
        AVG(value) AS avg_value,
        MIN(value) AS min_value,
        MAX(value) AS max_value,
        COUNT(*) AS sample_count
    FROM sensor_readings
    GROUP BY bucket, qube_id, sensor_id, field_key
    WITH NO DATA;

    SELECT add_continuous_aggregate_policy('sensor_readings_hourly',
        start_offset => INTERVAL '3 hours',
        end_offset => INTERVAL '1 hour',
        schedule_interval => INTERVAL '1 hour',
        if_not_exists => TRUE);

    -- ── Continuous Aggregates (daily rollups) ────────────────────
    CREATE MATERIALIZED VIEW IF NOT EXISTS sensor_readings_daily
    WITH (timescaledb.continuous) AS
    SELECT
        time_bucket('1 day', time) AS bucket,
        qube_id,
        sensor_id,
        field_key,
        AVG(value) AS avg_value,
        MIN(value) AS min_value,
        MAX(value) AS max_value,
        COUNT(*) AS sample_count
    FROM sensor_readings
    GROUP BY bucket, qube_id, sensor_id, field_key
    WITH NO DATA;

    SELECT add_continuous_aggregate_policy('sensor_readings_daily',
        start_offset => INTERVAL '3 days',
        end_offset => INTERVAL '1 day',
        schedule_interval => INTERVAL '1 day',
        if_not_exists => TRUE);
SQL

echo "qubedata database initialized with TimescaleDB hypertable."
