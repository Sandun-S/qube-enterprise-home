-- 001_init.sql — Qube Enterprise Schema
-- Everything in one place, no ALTER TABLE patches.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ===================== ORGANISATIONS =====================
CREATE TABLE organisations (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name           TEXT NOT NULL,
    mqtt_namespace TEXT NOT NULL DEFAULT '',
    org_secret     TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== USERS =====================
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id        UUID NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('superadmin', 'admin', 'editor', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===================== QUBES =====================
CREATE TABLE qubes (
    id              TEXT PRIMARY KEY,
    org_id          UUID REFERENCES organisations(id) ON DELETE SET NULL,
    auth_token_hash TEXT,
    last_seen       TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'unclaimed'
                    CHECK (status IN ('online', 'offline', 'unclaimed')),
    location_label  TEXT NOT NULL DEFAULT '',
    claimed_at      TIMESTAMPTZ,
    config_version  INT NOT NULL DEFAULT 0,
    -- Device identity — written by image-install.sh at flash time
    -- register_key: customer enters this to claim device (was reg_number in MySQL)
    -- maintain_key: IoT team maintenance access (was mntn_key in MySQL)
    -- device_type:  hardware variant (rasp4, rasp4_v2, neo5, etc.)
    register_key    TEXT UNIQUE,
    maintain_key    TEXT,
    device_type     TEXT NOT NULL DEFAULT 'arm64',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_qubes_register_key ON qubes(register_key);

-- ===================== CONFIG STATE =====================
CREATE TABLE config_state (
    qube_id          TEXT PRIMARY KEY REFERENCES qubes(id) ON DELETE CASCADE,
    hash             TEXT NOT NULL DEFAULT '',
    config_snapshot  JSONB NOT NULL DEFAULT '{}',
    generated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

-- ===================== SUPERADMIN ORG (IoT team internal) =====================
INSERT INTO organisations (id, name) VALUES
    ('00000000-0000-0000-0000-000000000001', 'IoT Team Internal');

-- Password: iotteam2024
INSERT INTO users (org_id, email, password_hash, role) VALUES
    ('00000000-0000-0000-0000-000000000001',
     'iotteam@internal.local',
     crypt('iotteam2024', gen_salt('bf', 12)),
     'superadmin');

-- ===================== PRE-REGISTERED TEST QUBES =====================
-- Factory-provisioned devices — not yet claimed by any org.
-- In production these are inserted by write-to-database.sh at flash time.
-- These test keys match test/mit.txt for local dev.
INSERT INTO qubes (id, register_key, maintain_key, device_type) VALUES
    ('Q-1001', 'TEST-Q1001-REG', 'TEST-Q1001-MNT', 'rasp4_v2'),
    ('Q-1002', 'TEST-Q1002-REG', 'TEST-Q1002-MNT', 'rasp4_v2'),
    ('Q-1003', 'TEST-Q1003-REG', 'TEST-Q1003-MNT', 'rasp4_v2'),
    ('Q-1004', 'TEST-Q1004-REG', 'TEST-Q1004-MNT', 'rasp4_v2'),
    ('Q-1005', 'TEST-Q1005-REG', 'TEST-Q1005-MNT', 'rasp4_v2'),
    ('Q-1006', 'TEST-Q1006-REG', 'TEST-Q1006-MNT', 'rasp4_v2'),
    ('Q-1007', 'TEST-Q1007-REG', 'TEST-Q1007-MNT', 'rasp4_v2'),
    ('Q-1008', 'TEST-Q1008-REG', 'TEST-Q1008-MNT', 'rasp4_v2'),
    ('Q-1009', 'TEST-Q1009-REG', 'TEST-Q1009-MNT', 'rasp4_v2'),
    ('Q-1010', 'TEST-Q1010-REG', 'TEST-Q1010-MNT', 'rasp4_v2'),
    ('Q-1011', 'TEST-Q1011-REG', 'TEST-Q1011-MNT', 'rasp4_v2'),
    ('Q-1012', 'TEST-Q1012-REG', 'TEST-Q1012-MNT', 'rasp4_v2'),
    ('Q-1013', 'TEST-Q1013-REG', 'TEST-Q1013-MNT', 'rasp4_v2'),
    ('Q-1014', 'TEST-Q1014-REG', 'TEST-Q1014-MNT', 'rasp4_v2'),
    ('Q-1015', 'TEST-Q1015-REG', 'TEST-Q1015-MNT', 'rasp4_v2'),
    ('Q-1016', 'TEST-Q1016-REG', 'TEST-Q1016-MNT', 'rasp4_v2'),
    ('Q-1017', 'TEST-Q1017-REG', 'TEST-Q1017-MNT', 'rasp4_v2'),
    ('Q-1018', 'TEST-Q1018-REG', 'TEST-Q1018-MNT', 'rasp4_v2'),
    ('Q-1019', 'TEST-Q1019-REG', 'TEST-Q1019-MNT', 'rasp4_v2'),
    ('Q-1020', 'TEST-Q1020-REG', 'TEST-Q1020-MNT', 'rasp4_v2');

INSERT INTO config_state (qube_id) VALUES
    ('Q-1001'),
    ('Q-1002'),
    ('Q-1003'),
    ('Q-1004'),
    ('Q-1005'),
    ('Q-1006'),
    ('Q-1007'),
    ('Q-1008'),
    ('Q-1009'),
    ('Q-1010'),
    ('Q-1011'),
    ('Q-1012'),
    ('Q-1013'),
    ('Q-1014'),
    ('Q-1015'),
    ('Q-1016'),
    ('Q-1017'),
    ('Q-1018'),
    ('Q-1019'),
    ('Q-1020');
