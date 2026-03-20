-- 001_init.sql — Qube Enterprise Phase 1 Schema
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ===================== ORGANISATIONS =====================
CREATE TABLE organisations (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name          TEXT NOT NULL,
    mqtt_namespace TEXT NOT NULL DEFAULT '',
    org_secret    TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== USERS =====================
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id        UUID NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('admin', 'editor', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== QUBES =====================
CREATE TABLE qubes (
    id             TEXT PRIMARY KEY,
    org_id         UUID REFERENCES organisations(id) ON DELETE SET NULL,
    auth_token_hash TEXT,
    last_seen      TIMESTAMPTZ,
    status         TEXT NOT NULL DEFAULT 'unclaimed'
                   CHECK (status IN ('online', 'offline', 'unclaimed')),
    location_label TEXT NOT NULL DEFAULT '',
    claimed_at     TIMESTAMPTZ,
    config_version INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== CONFIG STATE =====================
CREATE TABLE config_state (
    qube_id         TEXT PRIMARY KEY REFERENCES qubes(id) ON DELETE CASCADE,
    hash            TEXT NOT NULL DEFAULT '',
    config_snapshot JSONB NOT NULL DEFAULT '{}',
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== QUBE COMMANDS =====================
CREATE TABLE qube_commands (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    qube_id     TEXT NOT NULL REFERENCES qubes(id) ON DELETE CASCADE,
    command     TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'executed', 'failed', 'timeout')),
    result      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    executed_at TIMESTAMPTZ
);
CREATE INDEX idx_qube_commands_pending ON qube_commands(qube_id, status)
    WHERE status = 'pending';

-- ===================== PRE-REGISTERED QUBES =====================
-- Factory-provisioned devices. Not yet claimed by any org.
INSERT INTO qubes (id, register_key, maintain_key, device_type) VALUES
    ('Q-1001', 'TEST-Q1001-REG', 'TEST-Q1001-MNT', 'rasp4_v2'),
    ('Q-1002', 'TEST-Q1002-REG', 'TEST-Q1002-MNT', 'rasp4_v2'),
    ('Q-1003', 'TEST-Q1003-REG', 'TEST-Q1003-MNT', 'rasp4_v2'),
    ('Q-1004', 'TEST-Q1004-REG', 'TEST-Q1004-MNT', 'rasp4_v2'),
    ('Q-1005', 'TEST-Q1005-REG', 'TEST-Q1005-MNT', 'rasp4_v2');

-- Seed empty config state for each
INSERT INTO config_state (qube_id) VALUES
    ('Q-1001'), ('Q-1002'), ('Q-1003'),
    ('Q-1004'), ('Q-1005');

-- Update role check to include superadmin (IoT team internal role)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('superadmin', 'admin', 'editor', 'viewer'));

-- Seed a superadmin user for IoT team internal use
-- Password: iotteam2024 (bcrypt hash)
-- Change this in production!
INSERT INTO organisations (id, name) VALUES
  ('00000000-0000-0000-0000-000000000001', 'IoT Team Internal')
  ON CONFLICT DO NOTHING;

INSERT INTO users (org_id, email, password_hash, role) VALUES
  ('00000000-0000-0000-0000-000000000001',
   'iotteam@internal.local',
   '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewdBPj2NJCFCm3Gy',
   'superadmin')
  ON CONFLICT DO NOTHING;

-- ===================== SCHEMA UPDATE: register_key + maintain_key =====================
-- Matches the real Qube Lite device provisioning (image-install.sh generates these)
-- register_key = what customer enters to claim the device (was "reg_number" in MySQL)
-- maintain_key = IoT team maintenance access (was "mntn_key" in MySQL)

ALTER TABLE qubes
  ADD COLUMN IF NOT EXISTS register_key  TEXT UNIQUE,
  ADD COLUMN IF NOT EXISTS maintain_key  TEXT,
  ADD COLUMN IF NOT EXISTS device_type   TEXT NOT NULL DEFAULT 'arm64';

CREATE INDEX IF NOT EXISTS idx_qubes_register_key ON qubes(register_key);

-- Update pre-registered test qubes with dummy keys for local dev/testing
UPDATE qubes SET
  register_key = 'TEST-' || id || '-REG',
  maintain_key = 'TEST-' || id || '-MNT'
WHERE register_key IS NULL;
