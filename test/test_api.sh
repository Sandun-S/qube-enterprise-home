#!/usr/bin/env bash
# =============================================================================
# Qube Enterprise v2 — Full API Test Suite
# Tests every endpoint in order, checks every response, covers edge cases.
#
# Usage:
#   chmod +x test_api.sh
#   ./test_api.sh                        # run against localhost
#   ./test_api.sh http://192.168.1.10    # run against remote cloud VM
#
# Re-run safe: uses a unique RUN_ID timestamp so emails/names never conflict.
# Each run claims 3 qubes from the pool (Q-1001..Q-1020).
# If all qubes are claimed after many runs, reset with:
#   docker compose -f docker-compose.dev.yml down -v && docker compose -f docker-compose.dev.yml up -d
#
# Prerequisites: curl, jq
# =============================================================================

BASE="${1:-http://localhost:8080}"
TP_BASE="${BASE%:8080}:8081"
PASS=0; FAIL=0; SKIP=0

# Unique run ID — makes emails/names unique so tests are idempotent across re-runs
RUN_ID=$(date +%s)
TEST_EMAIL="admin_${RUN_ID}@test.com"
TEST_EMAIL_B="orgb_${RUN_ID}@test.com"
TEST_EMAIL_VIEWER="viewer_${RUN_ID}@test.com"

# ── helpers ──────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[0;33m'; CYAN='\033[0;36m'; NC='\033[0m'
section() { echo; echo -e "${CYAN}══ $1 ══${NC}"; }
ok()  { echo -e "  ${GREEN}✓${NC} $1"; ((PASS++)); }
fail(){ echo -e "  ${RED}✗${NC} $1"; ((FAIL++)); }
skip(){ echo -e "  ${YELLOW}○${NC} $1 (skipped)"; ((SKIP++)); }

assert_status() {
  local label=$1 expected=$2 actual=$3
  if [ "$actual" = "$expected" ]; then ok "$label (HTTP $actual)"
  else fail "$label — expected HTTP $expected, got HTTP $actual"; fi
}

assert_field() {
  local label=$1 value=$2
  if [ -n "$value" ] && [ "$value" != "null" ]; then ok "$label"
  else fail "$label — field empty or null"; fi
}

api() {
  local method=$1 path=$2 body=$3 token=$4
  local headers=(-s -w "\n%{http_code}" -H "Content-Type: application/json")
  [ -n "$token" ] && headers+=(-H "Authorization: Bearer $token")
  if [ -n "$body" ]; then
    curl "${headers[@]}" -X "$method" "$BASE$path" -d "$body"
  else
    curl "${headers[@]}" -X "$method" "$BASE$path"
  fi
}

tp_api() {
  local method=$1 path=$2 body=$3 qube_id=$4 token=$5
  local headers=(-s -w "\n%{http_code}" -H "Content-Type: application/json")
  [ -n "$qube_id" ] && headers+=(-H "X-Qube-ID: $qube_id")
  [ -n "$token"   ] && headers+=(-H "Authorization: Bearer $token")
  if [ -n "$body" ]; then
    curl "${headers[@]}" -X "$method" "$TP_BASE$path" -d "$body"
  else
    curl "${headers[@]}" -X "$method" "$TP_BASE$path"
  fi
}

body()  { echo "${1%$'\n'*}"; }   # response body
code()  { echo "${1##*$'\n'}"; }  # HTTP status code

echo ""
echo "Qube Enterprise v2 — Full API Test Suite"
echo "Cloud API: $BASE"
echo "TP-API:    $TP_BASE"
echo ""

# =============================================================================
section "0. Health checks"
# =============================================================================

R=$(curl -s -w "\n%{http_code}" "$BASE/health")
assert_status "Cloud API /health" "200" "$(code "$R")"
VERSION=$(body "$R" | jq -r .version)
[ "$VERSION" = "2" ] && ok "health version = 2" || fail "health version expected 2, got $VERSION"

R=$(curl -s -w "\n%{http_code}" "$TP_BASE/health")
assert_status "TP-API /health" "200" "$(code "$R")"

# =============================================================================
section "1. Auth — register org A"
# =============================================================================

R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Test Org $RUN_ID\",\"email\":\"$TEST_EMAIL\",\"password\":\"testpass123\"}")
assert_status "register new org" "201" "$(code "$R")"
TOKEN=$(body "$R" | jq -r .token)
assert_field "got JWT token" "$TOKEN"

# Duplicate registration should fail
R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Dup Org\",\"email\":\"$TEST_EMAIL\",\"password\":\"testpass123\"}")
assert_status "duplicate email rejected" "409" "$(code "$R")"

# =============================================================================
section "2. Auth — login"
# =============================================================================

R=$(api POST /api/v1/auth/login \
  "{\"email\":\"$TEST_EMAIL\",\"password\":\"testpass123\"}")
assert_status "login" "200" "$(code "$R")"
TOKEN=$(body "$R" | jq -r .token)
assert_field "login returns token" "$TOKEN"

R=$(api POST /api/v1/auth/login \
  "{\"email\":\"$TEST_EMAIL\",\"password\":\"wrongpass\"}")
assert_status "wrong password rejected" "401" "$(code "$R")"

# =============================================================================
section "3. Auth — superadmin login (IoT team)"
# =============================================================================

R=$(api POST /api/v1/auth/login \
  '{"email":"iotteam@internal.local","password":"iotteam2024"}')
assert_status "superadmin login" "200" "$(code "$R")"
SA_TOKEN=$(body "$R" | jq -r .token)
assert_field "superadmin token" "$SA_TOKEN"

ROLE=$(body "$R" | jq -r .role)
[ "$ROLE" = "superadmin" ] && ok "superadmin role confirmed" || fail "expected role=superadmin, got $ROLE"

# =============================================================================
section "4. Protected routes — no token"
# =============================================================================

R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/qubes")
assert_status "list qubes without token" "401" "$(code "$R")"

R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/readers/fake-id")
assert_status "get reader without token" "401" "$(code "$R")"

R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/device-templates")
assert_status "list device-templates without token" "401" "$(code "$R")"

R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/reader-templates")
assert_status "list reader-templates without token" "401" "$(code "$R")"

# =============================================================================
section "5. Qubes — list (empty for new org)"
# =============================================================================

R=$(api GET /api/v1/qubes "" "$TOKEN")
assert_status "list qubes" "200" "$(code "$R")"
QUBE_COUNT=$(body "$R" | jq '. | length')
[ "$QUBE_COUNT" = "0" ] && ok "new org has 0 qubes" || fail "expected 0 qubes, got $QUBE_COUNT"

# =============================================================================
section "6. Qubes — claim by register_key (production flow)"
# =============================================================================

# Claim Q-1001
R=$(api POST /api/v1/qubes/claim \
  '{"register_key":"TEST-Q1001-REG"}' "$TOKEN")
assert_status "claim Q-1001" "200" "$(code "$R")"
QUBE_ID=$(body "$R" | jq -r .qube_id)
QUBE_TOKEN=$(body "$R" | jq -r .auth_token)
assert_field "claimed qube_id" "$QUBE_ID"
assert_field "claimed auth_token" "$QUBE_TOKEN"
[ "$QUBE_ID" = "Q-1001" ] && ok "correct qube ID returned" || fail "expected Q-1001, got $QUBE_ID"

# Claim Q-1002 for isolation testing
R=$(api POST /api/v1/qubes/claim \
  '{"register_key":"TEST-Q1002-REG"}' "$TOKEN")
assert_status "claim Q-1002" "200" "$(code "$R")"
QUBE_ID_B=$(body "$R" | jq -r .qube_id)
QUBE_TOKEN_B=$(body "$R" | jq -r .auth_token)
assert_field "Q-1002 auth_token" "$QUBE_TOKEN_B"

# Duplicate claim must fail
R=$(api POST /api/v1/qubes/claim \
  '{"register_key":"TEST-Q1001-REG"}' "$TOKEN")
assert_status "double claim rejected" "409" "$(code "$R")"

# Unauthenticated claim must fail
R=$(curl -s -w "\n%{http_code}" -X POST -H "Content-Type: application/json" \
  "$BASE/api/v1/qubes/claim" -d '{"register_key":"TEST-Q1003-REG"}')
assert_status "claim without token rejected" "401" "$(code "$R")"

# List qubes — should have 2
R=$(api GET /api/v1/qubes "" "$TOKEN")
assert_status "list qubes after claim" "200" "$(code "$R")"
QUBE_COUNT=$(body "$R" | jq '. | length')
[ "$QUBE_COUNT" = "2" ] && ok "2 qubes after claim" || fail "expected 2 qubes, got $QUBE_COUNT"

# =============================================================================
section "6b. User Management — invite and roles"
# =============================================================================

R=$(api POST /api/v1/users \
  "{\"email\":\"$TEST_EMAIL_VIEWER\",\"password\":\"viewpass123\",\"role\":\"viewer\"}" "$TOKEN")
assert_status "invite viewer" "201" "$(code "$R")"
VIEWER_ID=$(body "$R" | jq -r .user_id)
assert_field "viewer user_id" "$VIEWER_ID"

R=$(api POST /api/v1/auth/login \
  "{\"email\":\"$TEST_EMAIL_VIEWER\",\"password\":\"viewpass123\"}")
assert_status "viewer login" "200" "$(code "$R")"
VIEWER_TOKEN=$(body "$R" | jq -r .token)

# Viewer cannot claim qubes (admin-only)
R=$(api POST /api/v1/qubes/claim \
  '{"register_key":"TEST-Q1003-REG"}' "$VIEWER_TOKEN")
assert_status "viewer cannot claim qube" "403" "$(code "$R")"

# Viewer cannot create readers (editor+)
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  '{"name":"Test","protocol":"modbus_tcp","config_json":{}}' "$VIEWER_TOKEN")
assert_status "viewer cannot create reader" "403" "$(code "$R")"

# Update viewer → editor
R=$(api PATCH "/api/v1/users/$VIEWER_ID" \
  '{"role":"editor"}' "$TOKEN")
assert_status "promote viewer to editor" "200" "$(code "$R")"

# List users
R=$(api GET /api/v1/users "" "$TOKEN")
assert_status "list users" "200" "$(code "$R")"
USER_COUNT=$(body "$R" | jq '. | length')
[ "$USER_COUNT" -ge "2" ] && ok "at least 2 users in org" || fail "expected >=2, got $USER_COUNT"

# Remove user
R=$(api DELETE "/api/v1/users/$VIEWER_ID" "" "$TOKEN")
assert_status "remove user" "200" "$(code "$R")"

# =============================================================================
section "7. Qubes — get and update"
# =============================================================================

R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN")
assert_status "get qube" "200" "$(code "$R")"
assert_field "qube has id" "$(body "$R" | jq -r .id)"
assert_field "qube has config_version" "$(body "$R" | jq -r .config_version)"
assert_field "qube has status" "$(body "$R" | jq -r .status)"

R=$(api PUT "/api/v1/qubes/$QUBE_ID" \
  '{"location_label":"Server Room A, Rack 3"}' "$TOKEN")
assert_status "update qube location" "200" "$(code "$R")"

# Verify persisted
R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN")
LOC=$(body "$R" | jq -r .location_label)
[ "$LOC" = "Server Room A, Rack 3" ] && ok "location persisted" \
  || fail "location not persisted: $LOC"

# =============================================================================
section "8. TP-API — device self-registration"
# =============================================================================

# Unclaimed device → pending
R=$(tp_api POST /v1/device/register \
  '{"device_id":"Q-1003","register_key":"TEST-Q1003-REG"}')
assert_status "unclaimed device returns pending" "202" "$(code "$R")"
STATUS=$(body "$R" | jq -r .status)
[ "$STATUS" = "pending" ] && ok "status=pending for unclaimed device" \
  || fail "expected pending, got $STATUS"

# Claimed device → token
R=$(tp_api POST /v1/device/register \
  '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}')
assert_status "claimed device returns token" "200" "$(code "$R")"
STATUS=$(body "$R" | jq -r .status)
[ "$STATUS" = "claimed" ] && ok "status=claimed" || fail "expected claimed, got $STATUS"
TP_TOKEN=$(body "$R" | jq -r .qube_token)
assert_field "qube_token in response" "$TP_TOKEN"
[ "$TP_TOKEN" = "$QUBE_TOKEN" ] && ok "token matches claim endpoint" \
  || fail "token mismatch: register=$TP_TOKEN, claim=$QUBE_TOKEN"

# Wrong key → rejected
R=$(tp_api POST /v1/device/register \
  '{"device_id":"Q-1001","register_key":"WRONG-KEY"}')
assert_status "wrong register_key rejected" "401" "$(code "$R")"

# =============================================================================
section "9. TP-API — heartbeat"
# =============================================================================

R=$(tp_api POST /v1/heartbeat \
  '{"status":"online","mem_free_mb":512,"disk_free_gb":20}' \
  "Q-1001" "$QUBE_TOKEN")
assert_status "heartbeat accepted" "200" "$(code "$R")"

R=$(tp_api POST /v1/heartbeat '{}' "Q-1001" "wrongtoken")
assert_status "heartbeat bad token rejected" "401" "$(code "$R")"

# =============================================================================
section "10. TP-API — sync state"
# =============================================================================

R=$(tp_api GET /v1/sync/state "" "Q-1001" "$QUBE_TOKEN")
assert_status "sync state" "200" "$(code "$R")"
assert_field "hash field" "$(body "$R" | jq -r .hash)"
assert_field "config_version" "$(body "$R" | jq -r .config_version)"
INITIAL_HASH=$(body "$R" | jq -r .hash)

# =============================================================================
section "10b. Protocols — list"
# =============================================================================

R=$(api GET /api/v1/protocols "" "$TOKEN")
assert_status "list protocols" "200" "$(code "$R")"
PROTO_COUNT=$(body "$R" | jq '. | length')
[ "$PROTO_COUNT" -ge "5" ] && ok "at least 5 protocols" || fail "expected >=5, got $PROTO_COUNT"

HAS_MODBUS=$(body "$R" | jq -r '[.[].id] | contains(["modbus_tcp"])')
[ "$HAS_MODBUS" = "true" ] && ok "modbus_tcp protocol present" || fail "modbus_tcp missing"

HAS_SNMP=$(body "$R" | jq -r '[.[].id] | contains(["snmp"])')
[ "$HAS_SNMP" = "true" ] && ok "snmp protocol present" || fail "snmp missing"

HAS_HTTP=$(body "$R" | jq -r '[.[].id] | contains(["http"])')
[ "$HAS_HTTP" = "true" ] && ok "http protocol present" || fail "http missing"

# =============================================================================
section "11. Device Templates — list and get"
# =============================================================================

R=$(api GET /api/v1/device-templates "" "$TOKEN")
assert_status "list device templates" "200" "$(code "$R")"
# New org may have 0 org-specific templates; global ones may be seeded

# =============================================================================
section "11b. Reader Templates — list and get"
# =============================================================================

R=$(api GET /api/v1/reader-templates "" "$TOKEN")
assert_status "list reader templates" "200" "$(code "$R")"
RT_COUNT=$(body "$R" | jq '. | length')
[ "$RT_COUNT" -ge "5" ] && ok "at least 5 reader templates seeded" \
  || fail "expected >=5 reader templates, got $RT_COUNT"

MODBUS_RT_ID=$(body "$R" | jq -r '.[] | select(.protocol == "modbus_tcp") | .id' | head -1)
assert_field "modbus_tcp reader template id" "$MODBUS_RT_ID"

SNMP_RT_ID=$(body "$R" | jq -r '.[] | select(.protocol == "snmp") | .id' | head -1)
assert_field "snmp reader template id" "$SNMP_RT_ID"

MQTT_RT_ID=$(body "$R" | jq -r '.[] | select(.protocol == "mqtt") | .id' | head -1)
assert_field "mqtt reader template id" "$MQTT_RT_ID"

# Get reader template by ID
R=$(api GET "/api/v1/reader-templates/$MODBUS_RT_ID" "" "$TOKEN")
assert_status "get reader template by id" "200" "$(code "$R")"
assert_field "reader template name" "$(body "$R" | jq -r .name)"
assert_field "reader template image_suffix" "$(body "$R" | jq -r .image_suffix)"
assert_field "reader template connection_schema" "$(body "$R" | jq -r .connection_schema)"

# =============================================================================
section "12. Device Templates — create org template"
# =============================================================================

R=$(api POST /api/v1/device-templates \
  "{
    \"name\": \"Janitza UMG96 $RUN_ID\",
    \"protocol\": \"modbus_tcp\",
    \"manufacturer\": \"Janitza\",
    \"model\": \"UMG-96RM\",
    \"description\": \"3-phase energy meter via Modbus TCP\",
    \"sensor_config\": {
      \"registers\": [
        {\"name\": \"active_power_w\", \"address\": 1294, \"type\": \"float32\", \"unit\": \"W\"},
        {\"name\": \"voltage_v\", \"address\": 1290, \"type\": \"float32\", \"unit\": \"V\"}
      ]
    },
    \"sensor_params_schema\": {
      \"type\": \"object\",
      \"properties\": {
        \"unit_id\": {\"type\": \"integer\", \"title\": \"Unit ID\", \"default\": 1}
      }
    }
  }" "$TOKEN")
assert_status "create org device template" "201" "$(code "$R")"
DT_ID=$(body "$R" | jq -r .id)
assert_field "device template id" "$DT_ID"
IS_GLOBAL=$(body "$R" | jq -r .is_global)
[ "$IS_GLOBAL" = "false" ] && ok "org template is_global=false" \
  || fail "expected is_global=false, got $IS_GLOBAL"

# Superadmin creates global SNMP template
R=$(api POST /api/v1/device-templates \
  "{
    \"name\": \"APC Smart-UPS $RUN_ID\",
    \"protocol\": \"snmp\",
    \"manufacturer\": \"APC\",
    \"model\": \"SMT1500\",
    \"sensor_config\": {
      \"oids\": [
        {\"name\": \"battery_percent\", \"oid\": \".1.3.6.1.4.1.318.1.1.1.2.2.1.0\", \"unit\": \"%\"},
        {\"name\": \"output_load_pct\", \"oid\": \".1.3.6.1.4.1.318.1.1.1.4.2.3.0\", \"unit\": \"%\"}
      ]
    }
  }" "$SA_TOKEN")
assert_status "superadmin creates global template" "201" "$(code "$R")"
SA_DT_ID=$(body "$R" | jq -r .id)
assert_field "global template id" "$SA_DT_ID"
IS_GLOBAL=$(body "$R" | jq -r .is_global)
[ "$IS_GLOBAL" = "true" ] && ok "superadmin template is_global=true" \
  || fail "expected is_global=true for superadmin"

# =============================================================================
section "13. Device Templates — update and patch"
# =============================================================================

R=$(api PUT "/api/v1/device-templates/$DT_ID" \
  '{"description":"Updated — adds frequency reading"}' "$TOKEN")
assert_status "update device template" "200" "$(code "$R")"

R=$(api PATCH "/api/v1/device-templates/$DT_ID/config" \
  "{\"sensor_config\":{\"registers\":[
    {\"name\":\"active_power_w\",\"address\":1294,\"type\":\"float32\",\"unit\":\"W\"},
    {\"name\":\"voltage_v\",\"address\":1290,\"type\":\"float32\",\"unit\":\"V\"},
    {\"name\":\"frequency_hz\",\"address\":1300,\"type\":\"float32\",\"unit\":\"Hz\"}
  ]}}" "$TOKEN")
assert_status "patch template sensor_config" "200" "$(code "$R")"

# Org user cannot edit superadmin global template
R=$(api PUT "/api/v1/device-templates/$SA_DT_ID" \
  '{"description":"Hacked"}' "$TOKEN")
assert_status "org user cannot edit global template" "403" "$(code "$R")"

# =============================================================================
section "13b. Reader Templates — superadmin CRUD"
# =============================================================================

R=$(api POST /api/v1/reader-templates \
  "{
    \"protocol\": \"modbus_tcp\",
    \"name\": \"Custom Modbus Reader $RUN_ID\",
    \"description\": \"Test reader template\",
    \"image_suffix\": \"modbus-reader\",
    \"connection_schema\": {\"type\":\"object\",\"properties\":{\"host\":{\"type\":\"string\"}}},
    \"env_defaults\": {\"LOG_LEVEL\":\"debug\"}
  }" "$SA_TOKEN")
assert_status "superadmin creates reader template" "201" "$(code "$R")"
NEW_RT_ID=$(body "$R" | jq -r .id)
assert_field "new reader template id" "$NEW_RT_ID"

R=$(api PUT "/api/v1/reader-templates/$NEW_RT_ID" \
  '{"description":"Updated description"}' "$SA_TOKEN")
assert_status "superadmin updates reader template" "200" "$(code "$R")"

# Non-superadmin cannot create reader templates
R=$(api POST /api/v1/reader-templates \
  '{"protocol":"snmp","name":"Hack","image_suffix":"evil"}' "$TOKEN")
assert_status "org admin cannot create reader template" "403" "$(code "$R")"

# =============================================================================
section "13c. Registry — view and update (superadmin only)"
# =============================================================================

R=$(api GET /api/v1/admin/registry "" "$SA_TOKEN")
assert_status "get registry settings" "200" "$(code "$R")"
assert_field "registry mode" "$(body "$R" | jq -r .mode)"

R=$(api PUT /api/v1/admin/registry \
  '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}' "$SA_TOKEN")
assert_status "update registry settings" "200" "$(code "$R")"
UPDATED_MODE=$(body "$R" | jq -r .settings.mode)
[ "$UPDATED_MODE" = "github" ] && ok "registry mode updated to github" \
  || fail "expected mode=github, got $UPDATED_MODE"

# Org admin cannot update registry
R=$(api PUT /api/v1/admin/registry '{"mode":"local"}' "$TOKEN")
assert_status "org admin cannot update registry" "403" "$(code "$R")"

# =============================================================================
section "14. Readers — all protocols"
# =============================================================================

# ── Modbus TCP reader ──
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  "{
    \"name\": \"Main PLC Reader\",
    \"protocol\": \"modbus_tcp\",
    \"template_id\": \"$MODBUS_RT_ID\",
    \"config_json\": {\"host\": \"192.168.10.1\", \"port\": 502, \"poll_interval_sec\": 20}
  }" "$TOKEN")
assert_status "create modbus_tcp reader" "201" "$(code "$R")"
MODBUS_READER_ID=$(body "$R" | jq -r .reader_id)
MODBUS_CONTAINER_ID=$(body "$R" | jq -r .container_id)
assert_field "modbus reader_id" "$MODBUS_READER_ID"
assert_field "modbus container_id" "$MODBUS_CONTAINER_ID"
HASH_AFTER_READER=$(body "$R" | jq -r .new_hash)
assert_field "reader creation returns new_hash" "$HASH_AFTER_READER"

# ── SNMP reader ──
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  "{
    \"name\": \"SNMP Network Reader\",
    \"protocol\": \"snmp\",
    \"template_id\": \"$SNMP_RT_ID\",
    \"config_json\": {\"fetch_interval_sec\": 30, \"timeout_sec\": 10, \"worker_count\": 2}
  }" "$TOKEN")
assert_status "create snmp reader" "201" "$(code "$R")"
SNMP_READER_ID=$(body "$R" | jq -r .reader_id)
assert_field "snmp reader_id" "$SNMP_READER_ID"

# ── MQTT reader ──
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  "{
    \"name\": \"MQTT Broker Reader\",
    \"protocol\": \"mqtt\",
    \"template_id\": \"$MQTT_RT_ID\",
    \"config_json\": {\"broker_host\": \"192.168.1.20\", \"broker_port\": 1883}
  }" "$TOKEN")
assert_status "create mqtt reader" "201" "$(code "$R")"
MQTT_READER_ID=$(body "$R" | jq -r .reader_id)
assert_field "mqtt reader_id" "$MQTT_READER_ID"

# Unknown protocol must be rejected
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  '{"name":"Bad Reader","protocol":"lora_wan","config_json":{}}' "$TOKEN")
assert_status "unknown protocol rejected" "400" "$(code "$R")"

# List readers — should have 3
R=$(api GET "/api/v1/qubes/$QUBE_ID/readers" "" "$TOKEN")
assert_status "list readers" "200" "$(code "$R")"
READER_COUNT=$(body "$R" | jq '. | length')
[ "$READER_COUNT" = "3" ] && ok "3 readers listed" || fail "expected 3 readers, got $READER_COUNT"

# Each reader has sensor_count
HAS_SENSOR_COUNT=$(body "$R" | jq '[.[].sensor_count] | all(. != null)')
[ "$HAS_SENSOR_COUNT" = "true" ] && ok "all readers have sensor_count" \
  || fail "some readers missing sensor_count"

# Get reader by ID
R=$(api GET "/api/v1/readers/$MODBUS_READER_ID" "" "$TOKEN")
assert_status "get reader by id" "200" "$(code "$R")"
assert_field "reader name" "$(body "$R" | jq -r .name)"
assert_field "reader protocol" "$(body "$R" | jq -r .protocol)"
assert_field "reader config_json host" "$(body "$R" | jq -r .config_json.host)"

# Update reader
R=$(api PUT "/api/v1/readers/$MODBUS_READER_ID" \
  '{"config_json":{"host":"192.168.10.2","port":502,"poll_interval_sec":30}}' "$TOKEN")
assert_status "update reader config" "200" "$(code "$R")"
assert_field "update returns new_hash" "$(body "$R" | jq -r .new_hash)"

# =============================================================================
section "15. Sensors — add for each protocol"
# =============================================================================

# Modbus sensor with device template
R=$(api POST "/api/v1/readers/$MODBUS_READER_ID/sensors" \
  "{
    \"name\": \"Main Energy Meter\",
    \"template_id\": \"$DT_ID\",
    \"params\": {\"unit_id\": 1},
    \"tags_json\": {\"location\": \"MDB\", \"phase\": \"3P\"},
    \"output\": \"influxdb\",
    \"table_name\": \"Measurements\"
  }" "$TOKEN")
assert_status "create modbus sensor with template" "201" "$(code "$R")"
MODBUS_SENSOR_ID=$(body "$R" | jq -r .sensor_id)
assert_field "modbus sensor_id" "$MODBUS_SENSOR_ID"
assert_field "sensor returns new_hash" "$(body "$R" | jq -r .new_hash)"

# Second modbus sensor without template
R=$(api POST "/api/v1/readers/$MODBUS_READER_ID/sensors" \
  "{
    \"name\": \"Panel B Meter\",
    \"params\": {
      \"unit_id\": 2,
      \"registers\": [
        {\"name\": \"active_power_w\", \"address\": 1294, \"type\": \"float32\", \"unit\": \"W\"}
      ]
    },
    \"output\": \"influxdb,live\"
  }" "$TOKEN")
assert_status "create modbus sensor without template" "201" "$(code "$R")"
MODBUS_SENSOR_B_ID=$(body "$R" | jq -r .sensor_id)
assert_field "modbus sensor B id" "$MODBUS_SENSOR_B_ID"

# SNMP sensor using global template
R=$(api POST "/api/v1/readers/$SNMP_READER_ID/sensors" \
  "{
    \"name\": \"Main UPS\",
    \"template_id\": \"$SA_DT_ID\",
    \"params\": {\"ip_address\": \"192.168.1.100\", \"community\": \"public\", \"version\": \"2c\"},
    \"output\": \"influxdb\"
  }" "$TOKEN")
assert_status "create snmp sensor with global template" "201" "$(code "$R")"
SNMP_SENSOR_ID=$(body "$R" | jq -r .sensor_id)
assert_field "snmp sensor_id" "$SNMP_SENSOR_ID"

# MQTT sensor
R=$(api POST "/api/v1/readers/$MQTT_READER_ID/sensors" \
  "{
    \"name\": \"Temp Sensor 01\",
    \"params\": {
      \"topic\": \"sensors/temp/01\",
      \"qos\": 0,
      \"json_path\": \"$.value\"
    },
    \"output\": \"live\"
  }" "$TOKEN")
assert_status "create mqtt sensor" "201" "$(code "$R")"
MQTT_SENSOR_ID=$(body "$R" | jq -r .sensor_id)
assert_field "mqtt sensor_id" "$MQTT_SENSOR_ID"

# Protocol mismatch must be rejected
R=$(api POST "/api/v1/readers/$SNMP_READER_ID/sensors" \
  "{\"name\":\"Bad\",\"template_id\":\"$DT_ID\"}" "$TOKEN")
assert_status "modbus template on snmp reader rejected" "400" "$(code "$R")"

# List sensors for modbus reader
R=$(api GET "/api/v1/readers/$MODBUS_READER_ID/sensors" "" "$TOKEN")
assert_status "list modbus sensors" "200" "$(code "$R")"
SENSOR_COUNT=$(body "$R" | jq '. | length')
[ "$SENSOR_COUNT" = "2" ] && ok "2 sensors on modbus reader" \
  || fail "expected 2, got $SENSOR_COUNT"

# List all sensors for qube
R=$(api GET "/api/v1/qubes/$QUBE_ID/sensors" "" "$TOKEN")
assert_status "list all qube sensors" "200" "$(code "$R")"
TOTAL_SENSORS=$(body "$R" | jq '. | length')
[ "$TOTAL_SENSORS" -ge "4" ] && ok ">=4 sensors across all readers" \
  || fail "expected >=4, got $TOTAL_SENSORS"

# Update sensor
R=$(api PUT "/api/v1/sensors/$MODBUS_SENSOR_ID" \
  '{"tags_json":{"location":"MDB","phase":"3P","feeder":"F01"}}' "$TOKEN")
assert_status "update sensor tags" "200" "$(code "$R")"

# =============================================================================
section "16. Config hash — verify changes propagate"
# =============================================================================

R=$(tp_api GET /v1/sync/state "" "Q-1001" "$QUBE_TOKEN")
assert_status "sync state after config additions" "200" "$(code "$R")"
NEW_HASH=$(body "$R" | jq -r .hash)
NEW_VERSION=$(body "$R" | jq -r .config_version)
[ "$NEW_HASH" != "$INITIAL_HASH" ] && ok "hash changed after config updates" \
  || fail "hash did not change (initial=$INITIAL_HASH, new=$NEW_HASH)"
[ "$NEW_VERSION" -gt "1" ] && ok "config_version > 1 after changes" \
  || fail "config_version should be >1, got $NEW_VERSION"

# =============================================================================
section "17. TP-API — sync config (v2: JSON with embedded SQLite data)"
# =============================================================================

R=$(tp_api GET /v1/sync/config "" "Q-1001" "$QUBE_TOKEN")
assert_status "sync config download" "200" "$(code "$R")"
CONFIG=$(body "$R")

assert_field "config has hash" "$(echo "$CONFIG" | jq -r .hash)"
assert_field "config has docker_compose_yml" "$(echo "$CONFIG" | jq -r .docker_compose_yml)"

READER_COUNT_SYNC=$(echo "$CONFIG" | jq '.readers | length')
[ "$READER_COUNT_SYNC" = "3" ] && ok "3 readers in sync config" \
  || fail "expected 3 readers in sync, got $READER_COUNT_SYNC"

CONT_COUNT_SYNC=$(echo "$CONFIG" | jq '.containers | length')
[ "$CONT_COUNT_SYNC" = "3" ] && ok "3 containers in sync config" \
  || fail "expected 3 containers in sync, got $CONT_COUNT_SYNC"

# Modbus reader should embed its 2 sensors
MODBUS_SENSORS_SYNC=$(echo "$CONFIG" | \
  jq '.readers[] | select(.protocol=="modbus_tcp") | .sensors | length')
[ "$MODBUS_SENSORS_SYNC" = "2" ] && ok "modbus reader has 2 sensors in sync" \
  || fail "expected 2 sensors in modbus reader, got $MODBUS_SENSORS_SYNC"

# docker-compose.yml should have services
HAS_SERVICES=$(echo "$CONFIG" | jq -r '.docker_compose_yml | contains("services:")')
[ "$HAS_SERVICES" = "true" ] && ok "docker_compose_yml has services block" \
  || fail "docker_compose_yml missing services"

# coreswitch_settings should be present (even if empty map)
CSKEY=$(echo "$CONFIG" | jq -r '.coreswitch_settings | type')
[ "$CSKEY" = "object" ] && ok "coreswitch_settings is an object" \
  || fail "coreswitch_settings missing or wrong type"

# =============================================================================
section "18. Containers — list"
# =============================================================================

R=$(api GET "/api/v1/qubes/$QUBE_ID/containers" "" "$TOKEN")
assert_status "list containers" "200" "$(code "$R")"
CONT_COUNT=$(body "$R" | jq '. | length')
[ "$CONT_COUNT" = "3" ] && ok "3 containers for qube" || fail "expected 3, got $CONT_COUNT"

HAS_IMAGE=$(body "$R" | jq '[.[].image] | all(. != null and . != "")')
[ "$HAS_IMAGE" = "true" ] && ok "all containers have image field" \
  || fail "some containers missing image"

# =============================================================================
section "19. Commands — send and poll"
# =============================================================================

R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"ping","payload":{"target":"cloud-api"}}' "$TOKEN")
assert_status "send ping command" "200" "$(code "$R")"
CMD_ID=$(body "$R" | jq -r .command_id)
assert_field "command_id returned" "$CMD_ID"

R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"reload_config","payload":{}}' "$TOKEN")
assert_status "send reload_config command" "200" "$(code "$R")"

R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"get_logs","payload":{"service":"modbus-reader","lines":100}}' "$TOKEN")
assert_status "send get_logs command" "200" "$(code "$R")"

# Unknown command must be rejected
R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"rm_rf","payload":{}}' "$TOKEN")
assert_status "unknown command rejected" "400" "$(code "$R")"

# Get command by ID
R=$(api GET "/api/v1/commands/$CMD_ID" "" "$TOKEN")
assert_status "get command by id" "200" "$(code "$R")"
assert_field "command field" "$(body "$R" | jq -r .command)"

# =============================================================================
section "20. TP-API — commands poll and ack"
# =============================================================================

R=$(tp_api POST /v1/commands/poll "" "Q-1001" "$QUBE_TOKEN")
assert_status "commands poll" "200" "$(code "$R")"
CMD_COUNT=$(body "$R" | jq '.commands | length')
[ "$CMD_COUNT" -ge "3" ] && ok "at least 3 commands pending" \
  || fail "expected >=3 commands, got $CMD_COUNT"

# Ack first command
FIRST_CMD_ID=$(body "$R" | jq -r '.commands[0].id')
R=$(tp_api POST "/v1/commands/$FIRST_CMD_ID/ack" \
  '{"result":"pong","success":true}' "Q-1001" "$QUBE_TOKEN")
assert_status "ack command" "200" "$(code "$R")"

# Re-poll — acked command gone
R=$(tp_api POST /v1/commands/poll "" "Q-1001" "$QUBE_TOKEN")
REMAINING=$(body "$R" | jq '.commands | length')
[ "$REMAINING" -lt "$CMD_COUNT" ] && ok "acked command removed from poll queue" \
  || fail "acked command still in queue (before=$CMD_COUNT, after=$REMAINING)"

# =============================================================================
section "21. TP-API — telemetry ingest"
# =============================================================================

NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
R=$(tp_api POST /v1/telemetry/ingest \
  "{\"readings\":[
    {\"time\":\"$NOW\",\"sensor_id\":\"$MODBUS_SENSOR_ID\",\"field_key\":\"active_power_w\",\"value\":1250.5,\"unit\":\"W\"},
    {\"time\":\"$NOW\",\"sensor_id\":\"$MODBUS_SENSOR_ID\",\"field_key\":\"voltage_v\",\"value\":231.2,\"unit\":\"V\"},
    {\"time\":\"$NOW\",\"sensor_id\":\"$MODBUS_SENSOR_ID\",\"field_key\":\"current_a\",\"value\":5.4,\"unit\":\"A\"},
    {\"time\":\"$NOW\",\"sensor_id\":\"$SNMP_SENSOR_ID\",\"field_key\":\"battery_percent\",\"value\":98.0,\"unit\":\"%\"},
    {\"time\":\"$NOW\",\"sensor_id\":\"$SNMP_SENSOR_ID\",\"field_key\":\"output_load_pct\",\"value\":42.5,\"unit\":\"%\"}
  ]}" "Q-1001" "$QUBE_TOKEN")
assert_status "telemetry ingest" "200" "$(code "$R")"
INSERTED=$(body "$R" | jq -r .inserted)
[ "$INSERTED" = "5" ] && ok "5 readings inserted" || fail "expected 5 inserted, got $INSERTED"
FAILED=$(body "$R" | jq -r .failed)
[ "$FAILED" = "0" ] && ok "0 failed readings" || fail "expected 0 failed, got $FAILED"

# Empty batch is valid
R=$(tp_api POST /v1/telemetry/ingest '{"readings":[]}' "Q-1001" "$QUBE_TOKEN")
assert_status "empty batch accepted" "200" "$(code "$R")"
[ "$(body "$R" | jq -r .inserted)" = "0" ] && ok "0 inserted for empty batch" \
  || fail "expected 0 for empty batch"

# Oversized batch rejected
if command -v python3 &>/dev/null; then
  BIG_BATCH=$(python3 -c "
import json
r = [{'sensor_id':'s','field_key':'v','value':1.0}] * 5001
print(json.dumps({'readings': r}))")
  R=$(tp_api POST /v1/telemetry/ingest "$BIG_BATCH" "Q-1001" "$QUBE_TOKEN")
  assert_status "oversized batch rejected (>5000)" "400" "$(code "$R")"
else
  skip "python3 not available — skipping batch size test"
fi

# =============================================================================
section "22. Telemetry — query"
# =============================================================================

sleep 1  # TimescaleDB needs a moment to make data queryable

R=$(api GET "/api/v1/data/sensors/$MODBUS_SENSOR_ID/latest" "" "$TOKEN")
assert_status "latest reading for sensor" "200" "$(code "$R")"
assert_field "sensor_id in response" "$(body "$R" | jq -r .sensor_id)"
FIELD_COUNT=$(body "$R" | jq '.fields | length')
[ "$FIELD_COUNT" -ge "2" ] && ok ">=2 fields in latest reading" \
  || fail "expected >=2 fields, got $FIELD_COUNT"

# Query all readings
R=$(api GET "/api/v1/data/readings?sensor_id=$MODBUS_SENSOR_ID" "" "$TOKEN")
assert_status "readings query" "200" "$(code "$R")"
READING_COUNT=$(body "$R" | jq -r .count)
[ "$READING_COUNT" -ge "3" ] && ok ">=3 readings returned" \
  || fail "expected >=3 readings, got $READING_COUNT"

# Filter by field_key
R=$(api GET "/api/v1/data/readings?sensor_id=$MODBUS_SENSOR_ID&field=active_power_w" "" "$TOKEN")
assert_status "readings with field filter" "200" "$(code "$R")"
UNIQUE_KEYS=$(body "$R" | jq '[.readings[].field_key] | unique | length')
[ "$UNIQUE_KEYS" = "1" ] && ok "field filter returns single field" \
  || fail "field filter returned $UNIQUE_KEYS unique keys, expected 1"

# =============================================================================
section "23. Delete — cascades"
# =============================================================================

# Delete sensor
R=$(api DELETE "/api/v1/sensors/$MODBUS_SENSOR_B_ID" "" "$TOKEN")
assert_status "delete sensor" "200" "$(code "$R")"
assert_field "delete returns new_hash" "$(body "$R" | jq -r .new_hash)"

# Verify sensor gone
R=$(api GET "/api/v1/readers/$MODBUS_READER_ID/sensors" "" "$TOKEN")
SENSORS_AFTER=$(body "$R" | jq '. | length')
[ "$SENSORS_AFTER" = "1" ] && ok "1 sensor after delete" \
  || fail "expected 1, got $SENSORS_AFTER"

# Delete reader — should cascade container
R=$(api DELETE "/api/v1/readers/$MQTT_READER_ID" "" "$TOKEN")
assert_status "delete mqtt reader" "200" "$(code "$R")"
assert_field "reader delete returns new_hash" "$(body "$R" | jq -r .new_hash)"

# Verify reader and container gone
R=$(api GET "/api/v1/qubes/$QUBE_ID/readers" "" "$TOKEN")
READERS_AFTER=$(body "$R" | jq '. | length')
[ "$READERS_AFTER" = "2" ] && ok "2 readers after delete" \
  || fail "expected 2, got $READERS_AFTER"

R=$(api GET "/api/v1/qubes/$QUBE_ID/containers" "" "$TOKEN")
CONT_AFTER=$(body "$R" | jq '. | length')
[ "$CONT_AFTER" = "2" ] && ok "2 containers after reader delete" \
  || fail "expected 2, got $CONT_AFTER"

# Delete device template
R=$(api DELETE "/api/v1/device-templates/$DT_ID" "" "$TOKEN")
assert_status "delete device template" "200" "$(code "$R")"

# Superadmin deletes reader template
R=$(api DELETE "/api/v1/reader-templates/$NEW_RT_ID" "" "$SA_TOKEN")
assert_status "superadmin deletes reader template" "200" "$(code "$R")"

# =============================================================================
section "24. Multi-qube isolation"
# =============================================================================

# Register org B
R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Org B $RUN_ID\",\"email\":\"$TEST_EMAIL_B\",\"password\":\"passB123\"}")
assert_status "register org B" "201" "$(code "$R")"
TOKEN_B=$(body "$R" | jq -r .token)

# Org B claims Q-1003
R=$(api POST /api/v1/qubes/claim \
  '{"register_key":"TEST-Q1003-REG"}' "$TOKEN_B")
assert_status "org B claims Q-1003" "200" "$(code "$R")"
QUBE_B_ID=$(body "$R" | jq -r .qube_id)
[ "$QUBE_B_ID" = "Q-1003" ] && ok "org B got Q-1003" || fail "expected Q-1003, got $QUBE_B_ID"

# Org B cannot get org A's qube
R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN_B")
assert_status "org B cannot get org A qube" "404" "$(code "$R")"

# Org B cannot get org A's reader
R=$(api GET "/api/v1/readers/$MODBUS_READER_ID" "" "$TOKEN_B")
assert_status "org B cannot get org A reader" "404" "$(code "$R")"

# Org B cannot create reader on org A's qube
R=$(api POST "/api/v1/qubes/$QUBE_ID/readers" \
  '{"name":"Attack","protocol":"modbus_tcp","config_json":{}}' "$TOKEN_B")
assert_status "org B cannot add reader to org A qube" "404" "$(code "$R")"

# TP-API: Q-1002's token should not work for Q-1003 operations
R=$(tp_api GET /v1/sync/state "" "Q-1003" "$QUBE_TOKEN_B")
assert_status "Q-1002 token cannot auth Q-1003" "401" "$(code "$R")"

# Me endpoint
R=$(api GET /api/v1/users/me "" "$TOKEN")
assert_status "get my profile" "200" "$(code "$R")"
MY_EMAIL=$(body "$R" | jq -r .email)
[ "$MY_EMAIL" = "$TEST_EMAIL" ] && ok "profile email correct" \
  || fail "expected $TEST_EMAIL, got $MY_EMAIL"

# =============================================================================
section "SUMMARY"
# =============================================================================
echo ""
echo -e "  ${GREEN}PASS: $PASS${NC}   ${RED}FAIL: $FAIL${NC}   ${YELLOW}SKIP: $SKIP${NC}"
echo ""
if [ "$FAIL" -gt 0 ]; then
  echo -e "  ${RED}Test suite FAILED${NC} — $FAIL assertion(s) failed"
  exit 1
else
  echo -e "  ${GREEN}All tests passed${NC}"
  exit 0
fi
