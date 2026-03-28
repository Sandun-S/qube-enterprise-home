-- 003_test_seeds.sql — Qube Enterprise v2 Dev/Test Data
-- Superadmin org, test user, pre-registered Qubes.
-- Only runs in dev (docker-compose.dev.yml mounts this).

-- ===================== SUPERADMIN ORG =====================
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

-- Config state entries for each Qube
INSERT INTO config_state (qube_id) VALUES
    ('Q-1001'), ('Q-1002'), ('Q-1003'), ('Q-1004'), ('Q-1005'),
    ('Q-1006'), ('Q-1007'), ('Q-1008'), ('Q-1009'), ('Q-1010'),
    ('Q-1011'), ('Q-1012'), ('Q-1013'), ('Q-1014'), ('Q-1015'),
    ('Q-1016'), ('Q-1017'), ('Q-1018'), ('Q-1019'), ('Q-1020');

-- Default coreswitch settings for Q-1001 (dev testing)
INSERT INTO coreswitch_settings (qube_id, key, value_json) VALUES
    ('Q-1001', 'outputs',           '{"influxdb": true, "live": false}'),
    ('Q-1001', 'batch_size',        '100'),
    ('Q-1001', 'flush_interval_ms', '5000');
