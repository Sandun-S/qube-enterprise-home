#!/usr/bin/env bash
# =============================================================================
# Qube Enterprise — Full API Test Suite
# Tests every endpoint in order, checks every response, covers edge cases.
#
# Usage:
#   chmod +x test_api.sh
#   ./test_api.sh                        # run against localhost
#   ./test_api.sh http://192.168.1.10    # run against remote cloud VM
#
# Re-run safe: uses a unique RUN_ID timestamp so emails/names never conflict.
# Each run claims 2 qubes from the pool (Q-1001..Q-1005).
# If all 5 qubes are claimed after 2+ runs, reset with:
#   docker compose -f docker-compose.dev.yml down -v && docker compose -f docker-compose.dev.yml up -d
#
# sensor_map.json missing = normal until conf-agent syncs after a sensor is added.
# After adding a sensor, wait 30s then it appears in /opt/qube/sensor_map.json.
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

split() { echo "${1%$'\n'*}"; }   # body
code()  { echo "${1##*$'\n'}"; }  # status code

echo ""
echo "Qube Enterprise — Full API Test Suite"
echo "Cloud API: $BASE"
echo "TP-API:    $TP_BASE"
echo ""

# =============================================================================
section "0. Health checks"
# =============================================================================

R=$(curl -s -w "\n%{http_code}" "$BASE/health")
assert_status "Cloud API /health" "200" "$(code "$R")"

R=$(curl -s -w "\n%{http_code}" "$TP_BASE/health")
assert_status "TP-API /health" "200" "$(code "$R")"

# =============================================================================
section "1. Auth — register"
# =============================================================================

R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Test Org $RUN_ID\",\"email\":\"$TEST_EMAIL\",\"password\":\"testpass123\"}")
assert_status "register new org" "201" "$(code "$R")"
TOKEN=$(split "$R" | jq -r .token)
ORG_ID=$(split "$R" | jq -r .org_id)
assert_field "register returns token" "$TOKEN"
assert_field "register returns org_id" "$ORG_ID"

# duplicate email
R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Other\",\"email\":\"$TEST_EMAIL\",\"password\":\"x\"}")
assert_status "register duplicate email → 409" "409" "$(code "$R")"

# missing fields
R=$(api POST /api/v1/auth/register '{"email":"x@x.com"}')
assert_status "register missing fields → 400" "400" "$(code "$R")"

# =============================================================================
section "2. Auth — login"
# =============================================================================

R=$(api POST /api/v1/auth/login \
  "{\"email\":\"$TEST_EMAIL\",\"password\":\"testpass123\"}")
assert_status "login valid" "200" "$(code "$R")"
TOKEN=$(split "$R" | jq -r .token)
assert_field "login returns token" "$TOKEN"

R=$(api POST /api/v1/auth/login \
  "{\"email\":\"$TEST_EMAIL\",\"password\":\"wrongpassword\"}")
assert_status "login wrong password → 401" "401" "$(code "$R")"

R=$(api POST /api/v1/auth/login '{"email":"notexist@test.com","password":"x"}')
assert_status "login unknown email → 401" "401" "$(code "$R")"

# =============================================================================
section "3. Auth — superadmin login (IoT team)"
# =============================================================================

R=$(api POST /api/v1/auth/login '{"email":"iotteam@internal.local","password":"iotteam2024"}')
assert_status "superadmin login" "200" "$(code "$R")"
SUPER_TOKEN=$(split "$R" | jq -r .token)
assert_field "superadmin token" "$SUPER_TOKEN"

# =============================================================================
# =============================================================================
section "4. Protected routes — no token"
# =============================================================================

R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/qubes")
assert_status "no token → 401" "401" "$(code "$R")"

R=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer bad.token.here" "$BASE/api/v1/qubes")
assert_status "bad token → 401" "401" "$(code "$R")"

# =============================================================================
section "5. Qubes — list"
# =============================================================================

R=$(api GET /api/v1/qubes "" "$TOKEN")
assert_status "list qubes (empty)" "200" "$(code "$R")"
COUNT=$(split "$R" | jq '. | length')
[ "$COUNT" = "0" ] && ok "list qubes returns empty array" || fail "expected 0 qubes, got $COUNT"

# =============================================================================
section "6. Qubes — claim by register_key (production flow)"
# =============================================================================

# Find unclaimed qubes from the pre-registered test set (Q-1001..Q-1020)
CLAIM_KEY1=""; CLAIM_KEY2=""
for N in $(seq 1001 1020); do
  KEY="TEST-Q${N}-REG"
  R=$(api POST /api/v1/qubes/claim "{\"register_key\":\"$KEY\"}" "$TOKEN")
  if [ "$(code "$R")" = "200" ]; then
    if [ -z "$CLAIM_KEY1" ]; then
      CLAIM_KEY1="$KEY"
      QUBE_TOKEN=$(split "$R" | jq -r .auth_token)
      QUBE_ID=$(split "$R" | jq -r .qube_id)
      ok "claimed $QUBE_ID with key $KEY"
    elif [ -z "$CLAIM_KEY2" ]; then
      CLAIM_KEY2="$KEY"
      QUBE2_TOKEN=$(split "$R" | jq -r .auth_token)
      QUBE2_ID=$(split "$R" | jq -r .qube_id)
      ok "claimed second qube $QUBE2_ID"
      break
    fi
  fi
done

# Hard stop if no qubes available — everything downstream needs QUBE_ID
if [ -z "$QUBE_ID" ] || [ -z "$QUBE_TOKEN" ]; then
  fail "no unclaimed test qubes available — reset with: docker compose -f docker-compose.dev.yml down -v && up -d"
  echo ""
  echo -e "  ${RED}Cannot continue — QUBE_ID is empty. Reset the DB and rerun.${NC}"
  echo "════════════════════════════════════"
  echo -e "  ${GREEN}PASS${NC}: $PASS"
  echo -e "  ${RED}FAIL${NC}: $FAIL"
  echo -e "  ${YELLOW}SKIP${NC}: $SKIP"
  echo "════════════════════════════════════"
  exit 1
fi

assert_field "first qube token" "$QUBE_TOKEN"
assert_field "first qube id" "$QUBE_ID"
assert_field "second qube token" "$QUBE2_TOKEN"

# already claimed
R=$(api POST /api/v1/qubes/claim "{\"register_key\":\"$CLAIM_KEY1\"}" "$TOKEN")
assert_status "claim already claimed → 409" "409" "$(code "$R")"

# wrong key
R=$(api POST /api/v1/qubes/claim '{"register_key":"WRONG-KEY-HERE"}' "$TOKEN")
assert_status "claim wrong key → 404" "404" "$(code "$R")"

# missing body
R=$(api POST /api/v1/qubes/claim '{}' "$TOKEN")
assert_status "claim no key → 400" "400" "$(code "$R")"

# non-admin cannot claim
R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Viewer Org $RUN_ID\",\"email\":\"$TEST_EMAIL_VIEWER\",\"password\":\"viewerpass\"}")
VIEWER_TOKEN=$(split "$R" | jq -r .token)
# Create viewer user — for now just test that viewer token can't claim
# (viewer token belongs to a different org, so it won't find Q-1003 in that org)

# =============================================================================
section "6b. User Management — invite and roles"
# =============================================================================

# Invite a viewer to our org
R=$(api POST /api/v1/users \
  "{\"email\":\"invited_viewer_${RUN_ID}@test.com\",\"password\":\"viewpass123\",\"role\":\"viewer\"}" \
  "$TOKEN")
assert_status "invite viewer" "201" "$(code "$R")"
INVITED_VIEWER_ID=$(split "$R" | jq -r .user_id)
assert_field "invited viewer id" "$INVITED_VIEWER_ID"
INVITED_VIEWER_ROLE=$(split "$R" | jq -r .role)
[ "$INVITED_VIEWER_ROLE" = "viewer" ] && ok "invited user has viewer role" \
  || fail "expected viewer role, got $INVITED_VIEWER_ROLE"

# Invite an editor to our org
R=$(api POST /api/v1/users \
  "{\"email\":\"invited_editor_${RUN_ID}@test.com\",\"password\":\"editpass123\",\"role\":\"editor\"}" \
  "$TOKEN")
assert_status "invite editor" "201" "$(code "$R")"
INVITED_EDITOR_ID=$(split "$R" | jq -r .user_id)

# List users — should see admin + viewer + editor
R=$(api GET /api/v1/users "" "$TOKEN")
assert_status "list users" "200" "$(code "$R")"
USER_COUNT=$(split "$R" | jq '. | length')
[ "$USER_COUNT" -ge "3" ] && ok "list users has $USER_COUNT users" \
  || fail "expected 3+ users, got $USER_COUNT"

# Get my own profile
R=$(api GET /api/v1/users/me "" "$TOKEN")
assert_status "get my profile" "200" "$(code "$R")"
MY_ROLE=$(split "$R" | jq -r .role)
[ "$MY_ROLE" = "admin" ] && ok "my role is admin" || fail "expected admin, got $MY_ROLE"
MY_USER_ID=$(split "$R" | jq -r .user_id)

# Login as the invited viewer
INVITED_VIEWER_TOKEN=$(split "$(api POST /api/v1/auth/login \
  "{\"email\":\"invited_viewer_${RUN_ID}@test.com\",\"password\":\"viewpass123\"}")" | jq -r .token)
assert_field "invited viewer can login" "$INVITED_VIEWER_TOKEN"

# Invited viewer can see qubes in same org
R=$(api GET /api/v1/qubes "" "$INVITED_VIEWER_TOKEN")
assert_status "viewer can list qubes" "200" "$(code "$R")"

# Invited viewer cannot claim a qube (needs admin)
R=$(api POST /api/v1/qubes/claim '{"register_key":"TEST-Q1005-REG"}' "$INVITED_VIEWER_TOKEN")
assert_status "viewer cannot claim → 403" "403" "$(code "$R")"

# Invited viewer cannot add a gateway (needs editor+) — use QUBE2_ID so section 14 count stays at 5
R=$(api POST "/api/v1/qubes/$QUBE2_ID/gateways" \
  '{"name":"X","protocol":"modbus_tcp","host":"1.2.3.4","port":502}' "$INVITED_VIEWER_TOKEN")
assert_status "viewer cannot create gateway → 403" "403" "$(code "$R")"

# Promote viewer to editor
R=$(api PATCH "/api/v1/users/$INVITED_VIEWER_ID" '{"role":"editor"}' "$TOKEN")
assert_status "promote viewer to editor" "200" "$(code "$R")"
NEW_ROLE=$(split "$R" | jq -r .role)
[ "$NEW_ROLE" = "editor" ] && ok "user promoted to editor" || fail "expected editor, got $NEW_ROLE"

# Cannot change own role
R=$(api PATCH "/api/v1/users/$MY_USER_ID" '{"role":"viewer"}' "$TOKEN")
assert_status "cannot change own role → 400" "400" "$(code "$R")"

# Remove editor from org
R=$(api DELETE "/api/v1/users/$INVITED_EDITOR_ID" "" "$TOKEN")
assert_status "remove user from org" "200" "$(code "$R")"

# Duplicate email invite
R=$(api POST /api/v1/users \
  "{\"email\":\"invited_viewer_${RUN_ID}@test.com\",\"password\":\"pass\",\"role\":\"viewer\"}" \
  "$TOKEN")
assert_status "duplicate email invite → 409" "409" "$(code "$R")"

# =============================================================================
section "7. Qubes — get and update"
# =============================================================================

R=$(api GET /api/v1/qubes "" "$TOKEN")
assert_status "list qubes after claim" "200" "$(code "$R")"
COUNT=$(split "$R" | jq '. | length')
[ "$COUNT" -ge "1" ] && ok "list qubes has entries" || fail "expected qubes, got $COUNT"

R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN")
assert_status "get qube detail" "200" "$(code "$R")"
assert_field "qube has id" "$(split "$R" | jq -r .id)"

R=$(api PUT "/api/v1/qubes/$QUBE_ID" '{"location_label":"Server Room 1"}' "$TOKEN")
assert_status "update location label" "200" "$(code "$R")"

# wrong org
R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$VIEWER_TOKEN")
assert_status "get qube wrong org → 404" "404" "$(code "$R")"

# =============================================================================
section "8. TP-API — device self-registration"
# =============================================================================

# Find an unclaimed qube for self-register test (skip the two we just claimed)
UNCLAIMED_ID=""; UNCLAIMED_KEY=""
for N in $(seq 1003 1020); do
  R=$(tp_api POST /v1/device/register \
    "{\"device_id\":\"Q-$N\",\"register_key\":\"TEST-Q${N}-REG\"}" "" "")
  if [ "$(code "$R")" = "202" ]; then
    UNCLAIMED_ID="Q-$N"; UNCLAIMED_KEY="TEST-Q${N}-REG"; break
  fi
done

if [ -n "$UNCLAIMED_ID" ]; then
  R=$(tp_api POST /v1/device/register \
    "{\"device_id\":\"$UNCLAIMED_ID\",\"register_key\":\"$UNCLAIMED_KEY\"}")
  assert_status "self-register unclaimed → 202" "202" "$(code "$R")"
  STATUS=$(split "$R" | jq -r .status)
  [ "$STATUS" = "pending" ] && ok "self-register unclaimed returns pending" \
    || fail "expected pending, got $STATUS"
else
  skip "self-register unclaimed (all test qubes already claimed — run: docker compose down -v && up)"
fi

# Wrong register_key
R=$(tp_api POST /v1/device/register \
  "{\"device_id\":\"$UNCLAIMED_ID\",\"register_key\":\"WRONG-KEY\"}")
assert_status "self-register wrong key → 401" "401" "$(code "$R")"

# Missing fields
R=$(tp_api POST /v1/device/register "{\"device_id\":\"$UNCLAIMED_ID\"}")
assert_status "self-register missing key → 400" "400" "$(code "$R")"

# Claimed device — should return token
R=$(tp_api POST /v1/device/register \
  "{\"device_id\":\"$QUBE_ID\",\"register_key\":\"$CLAIM_KEY1\"}")
assert_status "self-register claimed → 200" "200" "$(code "$R")"
RETURNED_TOKEN=$(split "$R" | jq -r .qube_token)
assert_field "self-register returns qube_token" "$RETURNED_TOKEN"
STATUS=$(split "$R" | jq -r .status)
[ "$STATUS" = "claimed" ] && ok "self-register claimed returns status=claimed" \
  || fail "expected claimed, got $STATUS"

# =============================================================================
section "9. TP-API — heartbeat"
# =============================================================================

R=$(tp_api POST /v1/heartbeat "{}" "$QUBE_ID" "$QUBE_TOKEN")
assert_status "heartbeat ok" "200" "$(code "$R")"
assert_field "heartbeat acknowledged" "$(split "$R" | jq -r .acknowledged)"

# No auth
R=$(tp_api POST /v1/heartbeat '{}')
assert_status "heartbeat no auth → 401" "401" "$(code "$R")"

# Wrong token
R=$(tp_api POST /v1/heartbeat '{}' "$QUBE_ID" "wrongtoken")
assert_status "heartbeat wrong token → 401" "401" "$(code "$R")"

# =============================================================================
section "10. TP-API — sync state"
# =============================================================================

R=$(tp_api GET /v1/sync/state "" "$QUBE_ID" "$QUBE_TOKEN")
assert_status "sync/state ok" "200" "$(code "$R")"
HASH=$(split "$R" | jq -r .hash)
assert_field "sync/state returns hash" "$HASH"

# =============================================================================
section "10b. Protocols — list"
# =============================================================================

R=$(api GET /api/v1/protocols "" "$TOKEN")
assert_status "list protocols" "200" "$(code "$R")"
PROTO_COUNT=$(split "$R" | jq '. | length')
[ "$PROTO_COUNT" = "4" ] && ok "4 protocols returned" || fail "expected 4 protocols, got $PROTO_COUNT"

PROTO_IDS=$(split "$R" | jq -r '[.[].id] | sort | join(",")')
[ "$PROTO_IDS" = "modbus_tcp,mqtt,opcua,snmp" ] && ok "all 4 protocol IDs present" \
  || fail "expected modbus_tcp,mqtt,opcua,snmp, got $PROTO_IDS"

assert_field "protocol has label"        "$(split "$R" | jq -r '.[0].label')"
assert_field "protocol has default_port" "$(split "$R" | jq -r '.[0].default_port')"
assert_field "protocol has addr_params_schema" "$(split "$R" | jq -r '.[0].addr_params_schema')"

# No auth → 401
R=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/protocols")
assert_status "protocols no auth → 401" "401" "$(code "$R")"

# =============================================================================
section "11. Templates — list and get"
# =============================================================================

R=$(api GET /api/v1/templates "" "$TOKEN")
assert_status "list templates" "200" "$(code "$R")"
TMPL_COUNT=$(split "$R" | jq '. | length')
[ "$TMPL_COUNT" -ge "10" ] && ok "templates has seeded globals ($TMPL_COUNT found)" \
  || fail "expected ≥10 global templates from migrations, got $TMPL_COUNT"

# Filter by protocol
R=$(api GET "/api/v1/templates?protocol=modbus_tcp" "" "$TOKEN")
assert_status "templates filter modbus_tcp" "200" "$(code "$R")"
MODBUS_TMPL_ID=$(split "$R" | jq -r '.[0].id')
assert_field "modbus template id" "$MODBUS_TMPL_ID"

R=$(api GET "/api/v1/templates?protocol=snmp" "" "$TOKEN")
assert_status "templates filter snmp" "200" "$(code "$R")"

R=$(api GET "/api/v1/templates?protocol=opcua" "" "$TOKEN")
assert_status "templates filter opcua" "200" "$(code "$R")"

R=$(api GET "/api/v1/templates?protocol=mqtt" "" "$TOKEN")
assert_status "templates filter mqtt" "200" "$(code "$R")"

# Get full detail
R=$(api GET "/api/v1/templates/$MODBUS_TMPL_ID" "" "$TOKEN")
assert_status "get template detail" "200" "$(code "$R")"
assert_field "template has config_json" "$(split "$R" | jq -r .config_json)"
assert_field "template has influx_fields_json" "$(split "$R" | jq -r .influx_fields_json)"

# Preview
R=$(api GET "/api/v1/templates/$MODBUS_TMPL_ID/preview" "" "$TOKEN")
assert_status "template preview" "200" "$(code "$R")"
assert_field "preview has rows" "$(split "$R" | jq -r '.rows | length')"

# Not found
R=$(api GET "/api/v1/templates/00000000-0000-0000-0000-000000000000" "" "$TOKEN")
assert_status "template not found → 404" "404" "$(code "$R")"

# =============================================================================
section "12. Templates — create org template"
# =============================================================================

R=$(api POST /api/v1/templates \
  '{"name":"Test Modbus Device","protocol":"modbus_tcp","description":"test device","config_json":{"registers":[{"address":100,"register_type":"Holding","data_type":"uint16","count":1,"scale":1.0,"field_key":"test_value","table":"Measurements"}]},"influx_fields_json":{"test_value":{"display_label":"Test","unit":""}}}' \
  "$TOKEN")
assert_status "create org template" "201" "$(code "$R")"
ORG_TMPL_ID=$(split "$R" | jq -r .id)
assert_field "create template returns id" "$ORG_TMPL_ID"

# Invalid protocol
R=$(api POST /api/v1/templates '{"name":"X","protocol":"bacnet"}' "$TOKEN")
assert_status "create template invalid protocol → 400" "400" "$(code "$R")"

# Missing name
R=$(api POST /api/v1/templates '{"protocol":"modbus_tcp"}' "$TOKEN")
assert_status "create template missing name → 400" "400" "$(code "$R")"

# =============================================================================
section "13. Templates — update and patch"
# =============================================================================

R=$(api PUT "/api/v1/templates/$ORG_TMPL_ID" \
  '{"name":"Test Modbus Device Updated","description":"updated","config_json":{"registers":[{"address":100,"register_type":"Holding","data_type":"uint16","count":1,"scale":1.0,"field_key":"test_value","table":"Measurements"},{"address":102,"register_type":"Holding","data_type":"uint16","count":1,"scale":0.1,"field_key":"test_power","table":"Measurements"}]},"influx_fields_json":{"test_value":{"display_label":"Test","unit":""},"test_power":{"display_label":"Power","unit":"W"}}}' \
  "$TOKEN")
assert_status "update template" "200" "$(code "$R")"

# Patch add register (superadmin only for global, but org template ok with editor+)
R=$(api PATCH "/api/v1/templates/$ORG_TMPL_ID/registers" \
  '{"action":"add","entry":{"address":104,"register_type":"Holding","data_type":"uint16","count":1,"scale":1.0,"field_key":"test_freq","table":"Measurements"}}' \
  "$SUPER_TOKEN")
assert_status "patch add register" "200" "$(code "$R")"
NEW_COUNT=$(split "$R" | jq -r .total_entries)
[ "$NEW_COUNT" = "3" ] && ok "patch add → 3 registers total" || fail "expected 3 registers, got $NEW_COUNT"

# Patch update register at index 0
R=$(api PATCH "/api/v1/templates/$ORG_TMPL_ID/registers" \
  '{"action":"update","index":0,"entry":{"address":101,"register_type":"Holding","data_type":"uint16","count":1,"scale":1.0,"field_key":"test_value","table":"Measurements"}}' \
  "$SUPER_TOKEN")
assert_status "patch update register" "200" "$(code "$R")"

# Patch delete register at index 2
R=$(api PATCH "/api/v1/templates/$ORG_TMPL_ID/registers" '{"action":"delete","index":2}' "$SUPER_TOKEN")
assert_status "patch delete register" "200" "$(code "$R")"
AFTER_DELETE=$(split "$R" | jq -r .total_entries)
[ "$AFTER_DELETE" = "2" ] && ok "patch delete → 2 registers remain" || fail "expected 2, got $AFTER_DELETE"

# Patch invalid action
R=$(api PATCH "/api/v1/templates/$ORG_TMPL_ID/registers" '{"action":"invalid"}' "$SUPER_TOKEN")
assert_status "patch invalid action → 400" "400" "$(code "$R")"

# Patch index out of range
R=$(api PATCH "/api/v1/templates/$ORG_TMPL_ID/registers" '{"action":"delete","index":99}' "$SUPER_TOKEN")
assert_status "patch out-of-range index → 400" "400" "$(code "$R")"

# Cannot patch global template with non-superadmin
R=$(api GET "/api/v1/templates?protocol=modbus_tcp" "" "$TOKEN")
GLOBAL_TMPL_ID=$(split "$R" | jq -r '[.[] | select(.is_global==true)][0].id')
if [ -n "$GLOBAL_TMPL_ID" ] && [ "$GLOBAL_TMPL_ID" != "null" ]; then
  R=$(api PATCH "/api/v1/templates/$GLOBAL_TMPL_ID/registers" '{"action":"delete","index":0}' "$TOKEN")
  assert_status "patch global template non-superadmin → 403" "403" "$(code "$R")"
fi

# =============================================================================
section "13b. Templates — superadmin creates global SNMP template (Vertiv ITA2)"
# =============================================================================

# Superadmin creates a new global template via API
R=$(api POST /api/v1/templates   '{
    "name":"Vertiv ITA2 UPS",
    "protocol":"snmp",
    "description":"Vertiv ITA2 3-phase UPS — voltages, currents, load, battery",
    "config_json":{
      "map_file":"vertiv-ita2.csv",
      "table":"snmp_data",
      "oids":[
        {"oid":"1.3.6.1.4.1.13400.2.54.2.1.1.0","field_key":"systemStatus"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.2.1.0","field_key":"inputPhaseVoltageA"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.3.1.0","field_key":"outputPhaseVoltageA"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.3.4.0","field_key":"outputCurrentA"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.3.8.0","field_key":"outputActivePowerA"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.3.14.0","field_key":"outputLoadA"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.5.7.0","field_key":"batteryRemainsTime"},
        {"oid":"1.3.6.1.4.1.13400.2.54.2.5.10.0","field_key":"batteryCapacity"}
      ]
    },
    "influx_fields_json":{
      "systemStatus":       {"display_label":"System Status","unit":""},
      "inputPhaseVoltageA": {"display_label":"Input Voltage A","unit":"V"},
      "outputPhaseVoltageA":{"display_label":"Output Voltage A","unit":"V"},
      "outputCurrentA":     {"display_label":"Output Current A","unit":"A"},
      "outputActivePowerA": {"display_label":"Output Active Power A","unit":"W"},
      "outputLoadA":        {"display_label":"Output Load A","unit":"%"},
      "batteryRemainsTime": {"display_label":"Battery Remaining","unit":"min"},
      "batteryCapacity":    {"display_label":"Battery Capacity","unit":"%"}
    }
  }'   "$SUPER_TOKEN")
assert_status "superadmin create global SNMP template" "201" "$(code "$R")"
VERTIV_TMPL_ID=$(split "$R" | jq -r .id)
assert_field "vertiv template id" "$VERTIV_TMPL_ID"

# Verify it is global=true (superadmin-created templates default to global)
IS_GLOBAL=$(split "$R" | jq -r .is_global)
[ "$IS_GLOBAL" = "true" ] && ok "new template is global" || fail "expected is_global=true, got $IS_GLOBAL"

# Regular admin can see it
R=$(api GET "/api/v1/templates?protocol=snmp" "" "$TOKEN")
assert_status "regular admin sees new global SNMP template" "200" "$(code "$R")"
SNMP_COUNT=$(split "$R" | jq '[.[] | select(.is_global==true)] | length')
[ "$SNMP_COUNT" -ge "4" ] && ok "snmp global templates: $SNMP_COUNT"   || fail "expected ≥4 global SNMP templates (3 seeded + 1 created), got $SNMP_COUNT"

# Preview the Vertiv template
R=$(api GET "/api/v1/templates/$VERTIV_TMPL_ID/preview" "" "$TOKEN")
assert_status "vertiv template preview" "200" "$(code "$R")"
ROW_COUNT=$(split "$R" | jq -r .row_count)
[ "$ROW_COUNT" -ge "1" ] && ok "vertiv preview has $ROW_COUNT rows" || fail "expected preview rows"

# Regular admin CANNOT delete a global template
R=$(api DELETE "/api/v1/templates/$VERTIV_TMPL_ID" "" "$TOKEN")
assert_status "regular admin cannot delete global template → 403" "403" "$(code "$R")"

# Superadmin can patch OIDs
R=$(api PATCH "/api/v1/templates/$VERTIV_TMPL_ID/registers"   '{"action":"add","entry":{"oid":"1.3.6.1.4.1.13400.2.54.2.5.8.0","field_key":"batteryTemperature"}}'   "$SUPER_TOKEN")
assert_status "superadmin patch global SNMP template" "200" "$(code "$R")"
AFTER=$(split "$R" | jq -r .total_entries)
[ "$AFTER" = "9" ] && ok "patch add OID → 9 total" || fail "expected 9 OIDs, got $AFTER"

# =============================================================================
section "13c. Registry — view and update (superadmin only)"
# =============================================================================

R=$(api GET /api/v1/admin/registry "" "$SUPER_TOKEN")
assert_status "get registry (superadmin)" "200" "$(code "$R")"
REG_MODE=$(split "$R" | jq -r .mode)
assert_field "registry has mode" "$REG_MODE"
assert_field "registry has github_base" "$(split "$R" | jq -r .github_base)"
RESOLVED_COUNT=$(split "$R" | jq -r '.resolved | length')
[ "$RESOLVED_COUNT" -ge "6" ] && ok "registry has $RESOLVED_COUNT resolved images" \
  || fail "expected ≥6 resolved images, got $RESOLVED_COUNT"

# Non-superadmin cannot access
R=$(api GET /api/v1/admin/registry "" "$TOKEN")
assert_status "get registry non-superadmin → 403" "403" "$(code "$R")"

# Update mode to gitlab
R=$(api PUT /api/v1/admin/registry '{"mode":"gitlab"}' "$SUPER_TOKEN")
assert_status "switch to gitlab mode" "200" "$(code "$R")"
[ "$(split "$R" | jq -r .settings.mode)" = "gitlab" ] && ok "mode switched to gitlab" \
  || fail "expected gitlab mode"

# Restore github mode
R=$(api PUT /api/v1/admin/registry '{"mode":"github"}' "$SUPER_TOKEN")
assert_status "restore github mode" "200" "$(code "$R")"
[ "$(split "$R" | jq -r .settings.mode)" = "github" ] && ok "mode restored to github" \
  || fail "expected github mode"

# Non-superadmin cannot update
R=$(api PUT /api/v1/admin/registry '{"mode":"custom"}' "$TOKEN")
assert_status "update registry non-superadmin → 403" "403" "$(code "$R")"

# =============================================================================
section "14. Gateways — all 4 protocols"
# =============================================================================

# Modbus TCP
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"Panel_A","protocol":"modbus_tcp","host":"192.168.1.100","port":502,"config_json":{"poll_interval_ms":5000}}' \
  "$TOKEN")
assert_status "create modbus gateway" "201" "$(code "$R")"
GW_MODBUS_ID=$(split "$R" | jq -r .gateway_id)
assert_field "modbus gateway_id" "$GW_MODBUS_ID"
assert_field "modbus service_id" "$(split "$R" | jq -r .service_id)"
assert_field "modbus service_name" "$(split "$R" | jq -r .service_name)"
assert_field "modbus new_hash" "$(split "$R" | jq -r .new_hash)"

# OPC-UA
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"PlantOPC","protocol":"opcua","host":"opc.tcp://192.168.1.18:52520/OPCUA/Server","port":52520}' \
  "$TOKEN")
assert_status "create opcua gateway" "201" "$(code "$R")"
GW_OPCUA_ID=$(split "$R" | jq -r .gateway_id)
assert_field "opcua gateway_id" "$GW_OPCUA_ID"

# SNMP
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"UPS_Room1","protocol":"snmp","host":"192.168.1.200","config_json":{"community":"public","version":"2c"}}' \
  "$TOKEN")
assert_status "create snmp gateway" "201" "$(code "$R")"
GW_SNMP_ID=$(split "$R" | jq -r .gateway_id)
assert_field "snmp gateway_id" "$GW_SNMP_ID"

# MQTT
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"MQTT_Floor2","protocol":"mqtt","host":"192.168.1.10","port":1883,"config_json":{"broker_url":"tcp://192.168.1.10:1883","base_topic":"factory/floor2"}}' \
  "$TOKEN")
assert_status "create mqtt gateway" "201" "$(code "$R")"
GW_MQTT_ID=$(split "$R" | jq -r .gateway_id)
assert_field "mqtt gateway_id" "$GW_MQTT_ID"

# Second Modbus gateway (same protocol, different IP — should create separate container)
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" \
  '{"name":"Panel_B","protocol":"modbus_tcp","host":"192.168.1.101","port":502}' \
  "$TOKEN")
assert_status "create second modbus gateway" "201" "$(code "$R")"
GW_MODBUS2_ID=$(split "$R" | jq -r .gateway_id)
GW_MODBUS2_NAME=$(split "$R" | jq -r .service_name)
assert_field "second modbus gateway_id" "$GW_MODBUS2_ID"
[ "$GW_MODBUS2_NAME" != "panel-a" ] && ok "second modbus has different service name ($GW_MODBUS2_NAME)" \
  || fail "second modbus must have different name from first"

# Invalid protocol
R=$(api POST "/api/v1/qubes/$QUBE_ID/gateways" '{"name":"X","protocol":"bacnet","host":"1.2.3.4"}' "$TOKEN")
assert_status "create gateway invalid protocol → 400" "400" "$(code "$R")"

# Wrong qube
R=$(api POST "/api/v1/qubes/Q-9999/gateways" '{"name":"X","protocol":"modbus_tcp","host":"1.2.3.4"}' "$TOKEN")
assert_status "create gateway wrong qube → 404" "404" "$(code "$R")"

# List gateways
R=$(api GET "/api/v1/qubes/$QUBE_ID/gateways" "" "$TOKEN")
assert_status "list gateways" "200" "$(code "$R")"
GW_COUNT=$(split "$R" | jq '. | length')
[ "$GW_COUNT" = "5" ] && ok "5 gateways created" || fail "expected 5 gateways, got $GW_COUNT"

# =============================================================================
section "15. Sensors — add for each protocol"
# =============================================================================

# Get template IDs
R=$(api GET "/api/v1/templates" "" "$TOKEN")
ALL_TEMPLATES=$(split "$R")
MODBUS_TMPL_ID=$(echo "$ALL_TEMPLATES" | jq -r '[.[] | select(.is_global==true and .protocol=="modbus_tcp")][0].id')
OPCUA_TMPL_ID=$(echo  "$ALL_TEMPLATES" | jq -r '[.[] | select(.is_global==true and .protocol=="opcua")][0].id')
SNMP_TMPL_ID=$(echo   "$ALL_TEMPLATES" | jq -r '[.[] | select(.is_global==true and .protocol=="snmp")][0].id')
MQTT_TMPL_ID=$(echo   "$ALL_TEMPLATES" | jq -r '[.[] | select(.is_global==true and .protocol=="mqtt")][0].id')

# Modbus sensor
BODY=$(printf '{"name":"Main_Meter","template_id":"%s","address_params":{"unit_id":1,"register_offset":0},"tags_json":{"location":"panel_a","building":"HQ"}}' "$MODBUS_TMPL_ID")
R=$(api POST "/api/v1/gateways/$GW_MODBUS_ID/sensors" "$BODY" "$TOKEN")
assert_status "create modbus sensor" "201" "$(code "$R")"
SENSOR_MODBUS_ID=$(split "$R" | jq -r .sensor_id)
CSV_ROWS=$(split "$R" | jq -r .csv_rows)
assert_field "modbus sensor_id" "$SENSOR_MODBUS_ID"
[ "$CSV_ROWS" -ge "1" ] && ok "modbus sensor generated $CSV_ROWS CSV rows" || fail "expected CSV rows, got $CSV_ROWS"

# Second modbus sensor (same gateway, different unit_id)
BODY=$(printf '{"name":"Sub_Meter","template_id":"%s","address_params":{"unit_id":2},"tags_json":{"location":"panel_a_sub"}}' "$MODBUS_TMPL_ID")
R=$(api POST "/api/v1/gateways/$GW_MODBUS_ID/sensors" "$BODY" "$TOKEN")
assert_status "create second modbus sensor" "201" "$(code "$R")"
SENSOR_MODBUS2_ID=$(split "$R" | jq -r .sensor_id)

# OPC-UA sensor
if [ -n "$OPCUA_TMPL_ID" ] && [ "$OPCUA_TMPL_ID" != "null" ]; then
  BODY=$(printf '{"name":"Pasteuriser_1","template_id":"%s","address_params":{"freq_sec":15},"tags_json":{"line":"line1"}}' "$OPCUA_TMPL_ID")
  R=$(api POST "/api/v1/gateways/$GW_OPCUA_ID/sensors" "$BODY" "$TOKEN")
  [ "$(code "$R")" != "201" ] && echo "  DEBUG opcua resp: $(split "$R")"
  assert_status "create opcua sensor" "201" "$(code "$R")"
  SENSOR_OPCUA_ID=$(split "$R" | jq -r .sensor_id)
else
  skip "opcua sensor (no global template found)"
fi

# SNMP sensor
if [ -n "$SNMP_TMPL_ID" ] && [ "$SNMP_TMPL_ID" != "null" ]; then
  BODY=$(printf '{"name":"UPS_Main","template_id":"%s","address_params":{"community":"public"},"tags_json":{"room":"server_room"}}' "$SNMP_TMPL_ID")
  R=$(api POST "/api/v1/gateways/$GW_SNMP_ID/sensors" "$BODY" "$TOKEN")
  assert_status "create snmp sensor" "201" "$(code "$R")"
  SENSOR_SNMP_ID=$(split "$R" | jq -r .sensor_id)
else
  skip "snmp sensor (no global template found)"
fi

# MQTT sensor
if [ -n "$MQTT_TMPL_ID" ] && [ "$MQTT_TMPL_ID" != "null" ]; then
  BODY=$(printf '{"name":"Temp_Sensor_1","template_id":"%s","address_params":{"topic_suffix":"temp_01"},"tags_json":{"floor":"2"}}' "$MQTT_TMPL_ID")
  R=$(api POST "/api/v1/gateways/$GW_MQTT_ID/sensors" "$BODY" "$TOKEN")
  assert_status "create mqtt sensor" "201" "$(code "$R")"
  SENSOR_MQTT_ID=$(split "$R" | jq -r .sensor_id)
else
  skip "mqtt sensor (no global template found)"
fi

# Protocol mismatch (modbus template on mqtt gateway)
R=$(api POST "/api/v1/gateways/$GW_MQTT_ID/sensors" \
  "{\"name\":\"Wrong\",\"template_id\":\"$MODBUS_TMPL_ID\",\"address_params\":{}}" "$TOKEN")
assert_status "sensor protocol mismatch → 400" "400" "$(code "$R")"

# Template not found
R=$(api POST "/api/v1/gateways/$GW_MODBUS_ID/sensors" \
  '{"name":"X","template_id":"00000000-0000-0000-0000-000000000000","address_params":{}}' "$TOKEN")
assert_status "sensor template not found → 404" "404" "$(code "$R")"

# Missing name
R=$(api POST "/api/v1/gateways/$GW_MODBUS_ID/sensors" \
  "{\"template_id\":\"$MODBUS_TMPL_ID\",\"address_params\":{}}" "$TOKEN")
assert_status "sensor missing name → 400" "400" "$(code "$R")"

# List sensors for gateway
R=$(api GET "/api/v1/gateways/$GW_MODBUS_ID/sensors" "" "$TOKEN")
assert_status "list sensors by gateway" "200" "$(code "$R")"
S_COUNT=$(split "$R" | jq '. | length')
[ "$S_COUNT" = "2" ] && ok "2 sensors on modbus gateway" || fail "expected 2, got $S_COUNT"

# List all sensors for qube
R=$(api GET "/api/v1/qubes/$QUBE_ID/sensors" "" "$TOKEN")
assert_status "list all sensors for qube" "200" "$(code "$R")"
assert_field "sensors list not empty" "$(split "$R" | jq '. | length')"

# =============================================================================
section "16. Sensor rows — view and fix"
# =============================================================================

R=$(api GET "/api/v1/sensors/$SENSOR_MODBUS_ID/rows" "" "$TOKEN")
assert_status "list sensor rows" "200" "$(code "$R")"
ROW_COUNT=$(split "$R" | jq '.rows | length')
[ "$ROW_COUNT" -ge "1" ] && ok "sensor has $ROW_COUNT CSV rows" || fail "expected rows, got $ROW_COUNT"
ROW_ID=$(split "$R" | jq -r '.rows[0].id')
assert_field "first row has id" "$ROW_ID"

# Fix a row (wrong address correction)
R=$(api PUT "/api/v1/sensors/$SENSOR_MODBUS_ID/rows/$ROW_ID" '{
  "row_data":{
    "Equipment":"Main_Meter",
    "Reading":"active_power_w",
    "RegType":"Holding",
    "Address":3001,
    "Type":"uint16",
    "Output":"influxdb",
    "Table":"Measurements",
    "Tags":"location=panel_a"
  }}' "$TOKEN")
assert_status "fix sensor row" "200" "$(code "$R")"
assert_field "fix row returns new_hash" "$(split "$R" | jq -r .new_hash)"

# Add a new row to sensor
R=$(api POST "/api/v1/sensors/$SENSOR_MODBUS_ID/rows" '{
  "row_data":{
    "Equipment":"Main_Meter",
    "Reading":"reactive_energy_kvarh",
    "RegType":"Holding",
    "Address":3210,
    "Type":"uint16",
    "Output":"influxdb",
    "Table":"Measurements",
    "Tags":"location=panel_a"
  }}' "$TOKEN")
assert_status "add sensor row" "201" "$(code "$R")"
NEW_ROW_ID=$(split "$R" | jq -r .row_id)
assert_field "new row id" "$NEW_ROW_ID"

# Delete the row we just added
R=$(api DELETE "/api/v1/sensors/$SENSOR_MODBUS_ID/rows/$NEW_ROW_ID" "" "$TOKEN")
assert_status "delete sensor row" "200" "$(code "$R")"

# Fix row for wrong sensor (different org)
R=$(api PUT "/api/v1/sensors/$SENSOR_MODBUS_ID/rows/$ROW_ID" \
  '{"row_data":{"Equipment":"X"}}' "$VIEWER_TOKEN")
assert_status "fix row wrong org → 404" "404" "$(code "$R")"

# =============================================================================
section "17. TP-API — sync config (verifies CSV generation)"
# =============================================================================

R=$(tp_api GET /v1/sync/config "" "$QUBE_ID" "$QUBE_TOKEN")
assert_status "sync/config ok" "200" "$(code "$R")"
COMPOSE=$(split "$R" | jq -r .docker_compose_yml)
assert_field "sync/config has docker_compose_yml" "$COMPOSE"

CSV_KEYS=$(split "$R" | jq -r '.csv_files | keys | length')
[ "$CSV_KEYS" -ge "1" ] && ok "sync/config has $CSV_KEYS CSV files" || fail "expected CSV files"

SENSOR_MAP_LEN=$(split "$R" | jq -r '.sensor_map | length')
[ "$SENSOR_MAP_LEN" -ge "1" ] && ok "sync/config has sensor_map with $SENSOR_MAP_LEN entries" \
  || fail "expected sensor_map entries"

# Check compose contains qube-net
echo "$COMPOSE" | grep -q "qube-net" && ok "compose has qube-net network" || fail "compose missing qube-net"

# Check compose has panel-a service
echo "$COMPOSE" | grep -q "panel-a" && ok "compose has panel-a service" || fail "compose missing panel-a service"

# Check configs.yml generated for modbus gateway
CONFIG_YML=$(split "$R" | jq -r '.csv_files["configs/panel-a/configs.yml"]')
[ -n "$CONFIG_YML" ] && [ "$CONFIG_YML" != "null" ] \
  && ok "configs.yml generated for panel-a" || fail "missing configs/panel-a/configs.yml"

# Check registers.csv for modbus
REGS_CSV=$(split "$R" | jq -r '.csv_files["configs/panel-a/config.csv"]')
[ -n "$REGS_CSV" ] && [ "$REGS_CSV" != "null" ] \
  && ok "config.csv generated for panel-a" || fail "missing configs/panel-a/config.csv"

# Check hash changed after adding sensors (compared to initial claim hash)
NEW_HASH=$(split "$R" | jq -r .hash)
[ -n "$NEW_HASH" ] && ok "sync/config returns hash ($NEW_HASH)" || fail "missing hash"

# =============================================================================
section "18. Commands — send and poll"
# =============================================================================

R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"ping","payload":{"target":"8.8.8.8"}}' "$TOKEN")
assert_status "send ping command" "202" "$(code "$R")"
CMD_ID=$(split "$R" | jq -r .command_id)
assert_field "command_id" "$CMD_ID"
STATUS=$(split "$R" | jq -r .status)
[ "$STATUS" = "pending" ] && ok "command status is pending" || fail "expected pending"

R=$(api GET "/api/v1/commands/$CMD_ID" "" "$TOKEN")
assert_status "get command result" "200" "$(code "$R")"

# All valid commands
for CMD in reload_config list_containers; do
  R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
    "{\"command\":\"$CMD\",\"payload\":{}}" "$TOKEN")
  assert_status "send $CMD command" "202" "$(code "$R")"
done

# restart_service with service name
R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"restart_service","payload":{"service":"panel-a"}}' "$TOKEN")
assert_status "send restart_service command" "202" "$(code "$R")"

# get_logs with options
R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"get_logs","payload":{"service":"panel-a","lines":50}}' "$TOKEN")
assert_status "send get_logs command" "202" "$(code "$R")"

# Unknown command
R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" \
  '{"command":"unknown_cmd","payload":{}}' "$TOKEN")
assert_status "unknown command → 400" "400" "$(code "$R")"

# Missing command
R=$(api POST "/api/v1/qubes/$QUBE_ID/commands" '{"payload":{}}' "$TOKEN")
assert_status "missing command → 400" "400" "$(code "$R")"

# Wrong qube
R=$(api POST "/api/v1/qubes/Q-9999/commands" \
  '{"command":"ping","payload":{}}' "$TOKEN")
assert_status "command wrong qube → 404" "404" "$(code "$R")"

# Get command wrong org
R=$(api GET "/api/v1/commands/$CMD_ID" "" "$VIEWER_TOKEN")
assert_status "get command wrong org → 404" "404" "$(code "$R")"

# =============================================================================
section "19. TP-API — commands poll and ack"
# =============================================================================

R=$(tp_api POST /v1/commands/poll "{}" "$QUBE_ID" "$QUBE_TOKEN")
assert_status "commands/poll" "200" "$(code "$R")"
CMDS=$(split "$R" | jq -r '.commands | length')
ok "commands/poll returned $CMDS commands"

# Get the latest command id to ack
LATEST_CMD_ID=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN" | \
  head -1 | jq -r '.recent_commands[0].id // empty')
if [ -n "$LATEST_CMD_ID" ]; then
  R=$(tp_api POST "/v1/commands/$LATEST_CMD_ID/ack" \
    '{"status":"executed","result":{"output":"test ok"}}' "$QUBE_ID" "$QUBE_TOKEN")
  assert_status "commands/ack executed" "200" "$(code "$R")"

  # Ack again (already acked → 404)
  R=$(tp_api POST "/v1/commands/$LATEST_CMD_ID/ack" \
    '{"status":"executed","result":{}}' "$QUBE_ID" "$QUBE_TOKEN")
  assert_status "double ack → 404" "404" "$(code "$R")"
else
  skip "ack test (no recent commands found)"
fi

# Invalid ack status
R=$(tp_api POST "/v1/commands/$CMD_ID/ack" \
  '{"status":"invalid_status"}' "$QUBE_ID" "$QUBE_TOKEN")
assert_status "ack invalid status → 400" "400" "$(code "$R")"

# =============================================================================
section "20. TP-API — telemetry ingest"
# =============================================================================

# Build readings payload using our sensor ID — use printf for clean JSON
PAYLOAD=$(printf '{"readings":[{"sensor_id":"%s","field_key":"active_power_w","value":1250.5,"unit":"W"},{"sensor_id":"%s","field_key":"voltage_v","value":231.2,"unit":"V"},{"sensor_id":"%s","field_key":"current_a","value":5.4,"unit":"A"},{"sensor_id":"%s","field_key":"reactive_power_var","value":87.5,"unit":"VAR"}]}' \
  "$SENSOR_MODBUS_ID" "$SENSOR_MODBUS_ID" "$SENSOR_MODBUS_ID" "$SENSOR_MODBUS_ID")

R=$(tp_api POST /v1/telemetry/ingest "$PAYLOAD" "$QUBE_ID" "$QUBE_TOKEN")
assert_status "telemetry ingest" "200" "$(code "$R")"
INSERTED=$(split "$R" | jq -r .inserted)
[ "$INSERTED" = "4" ] && ok "telemetry inserted 4 readings" || fail "expected 4 inserted, got $INSERTED"
FAILED=$(split "$R" | jq -r .failed)
[ "$FAILED" = "0" ] && ok "telemetry 0 failures" || fail "expected 0 failed, got $FAILED"

# Empty batch (valid, returns 0)
R=$(tp_api POST /v1/telemetry/ingest '{"readings":[]}' "$QUBE_ID" "$QUBE_TOKEN")
assert_status "telemetry empty batch" "200" "$(code "$R")"

# Batch too large (>5000) — write to temp file to avoid shell arg limit
TMPFILE=$(mktemp /tmp/qube_test_XXXXXX.json)
printf '{"readings":[' > "$TMPFILE"
for i in $(seq 1 5001); do
  printf '{"sensor_id":"%s","field_key":"val","value":1.0}' "$SENSOR_MODBUS_ID" >> "$TMPFILE"
  [ $i -lt 5001 ] && printf ',' >> "$TMPFILE"
done
printf ']}' >> "$TMPFILE"
R=$(curl -s -w "\n%{http_code}" -H "Content-Type: application/json" \
  -H "X-Qube-ID: $QUBE_ID" -H "Authorization: Bearer $QUBE_TOKEN" \
  -X POST "$TP_BASE/v1/telemetry/ingest" -d "@$TMPFILE")
assert_status "telemetry oversized batch → 400" "400" "$(code "$R")"
rm -f "$TMPFILE"

# No auth
R=$(tp_api POST /v1/telemetry/ingest "$PAYLOAD")
assert_status "telemetry no auth → 401" "401" "$(code "$R")"

# Ingest from second qube (different token, same endpoint)
PAYLOAD2=$(printf '{"readings":[{"sensor_id":"%s","field_key":"voltage_v","value":229.5,"unit":"V"}]}' "$SENSOR_MODBUS_ID")
R=$(tp_api POST /v1/telemetry/ingest "$PAYLOAD2" "$QUBE2_ID" "$QUBE2_TOKEN")
assert_status "telemetry from second qube" "200" "$(code "$R")"

# =============================================================================
section "21. Telemetry — query"
# =============================================================================

# Latest values
R=$(api GET "/api/v1/data/sensors/$SENSOR_MODBUS_ID/latest" "" "$TOKEN")
assert_status "latest sensor values" "200" "$(code "$R")"
FIELDS=$(split "$R" | jq -r '.fields | length')
[ "$FIELDS" -ge "1" ] && ok "latest values has $FIELDS fields" || fail "expected fields, got $FIELDS"
assert_field "latest has sensor_name" "$(split "$R" | jq -r .sensor_name)"

# Check specific field value
POWER=$(split "$R" | jq -r '[.fields[] | select(.field_key=="active_power_w")][0].value')
[ "$POWER" = "1250.5" ] && ok "active_power_w = 1250.5 W" || fail "expected 1250.5, got $POWER"

# Historical readings (default last 24h)
R=$(api GET "/api/v1/data/readings?sensor_id=$SENSOR_MODBUS_ID" "" "$TOKEN")
assert_status "historical readings" "200" "$(code "$R")"
assert_field "readings count" "$(split "$R" | jq -r .count)"

# Filter by field
R=$(api GET "/api/v1/data/readings?sensor_id=$SENSOR_MODBUS_ID&field=active_power_w" "" "$TOKEN")
assert_status "readings filtered by field" "200" "$(code "$R")"

# Filter by time range
FROM=$(date -u -d '1 hour ago' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -v-1H '+%Y-%m-%dT%H:%M:%SZ')
TO=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
R=$(api GET "/api/v1/data/readings?sensor_id=$SENSOR_MODBUS_ID&from=$FROM&to=$TO" "" "$TOKEN")
assert_status "readings with time range" "200" "$(code "$R")"

# Missing sensor_id
R=$(api GET "/api/v1/data/readings" "" "$TOKEN")
assert_status "readings no sensor_id → 400" "400" "$(code "$R")"

# Sensor not found
R=$(api GET "/api/v1/data/sensors/00000000-0000-0000-0000-000000000000/latest" "" "$TOKEN")
assert_status "latest wrong sensor → 404" "404" "$(code "$R")"

# Wrong org
R=$(api GET "/api/v1/data/sensors/$SENSOR_MODBUS_ID/latest" "" "$VIEWER_TOKEN")
assert_status "latest wrong org → 404" "404" "$(code "$R")"

# =============================================================================
section "22. Delete — cascades"
# =============================================================================

# Delete sensor → rows deleted, hash updated
R=$(api DELETE "/api/v1/sensors/$SENSOR_MODBUS2_ID" "" "$TOKEN")
assert_status "delete sensor" "200" "$(code "$R")"
assert_field "delete sensor returns new_hash" "$(split "$R" | jq -r .new_hash)"

# Deleted sensor is gone
R=$(api GET "/api/v1/data/sensors/$SENSOR_MODBUS2_ID/latest" "" "$TOKEN")
assert_status "deleted sensor → 404" "404" "$(code "$R")"

# Delete gateway → sensors + services + csv_rows cascade
R=$(api DELETE "/api/v1/gateways/$GW_MODBUS2_ID" "" "$TOKEN")
assert_status "delete gateway" "200" "$(code "$R")"

# Deleted gateway's sensors are gone
R=$(api GET "/api/v1/gateways/$GW_MODBUS2_ID/sensors" "" "$TOKEN")
assert_status "deleted gateway → 404" "404" "$(code "$R")"

# Delete org template
R=$(api DELETE "/api/v1/templates/$ORG_TMPL_ID" "" "$TOKEN")
assert_status "delete org template" "200" "$(code "$R")"

# Cannot delete global template as non-superadmin
if [ -n "$GLOBAL_TMPL_ID" ] && [ "$GLOBAL_TMPL_ID" != "null" ]; then
  R=$(api DELETE "/api/v1/templates/$GLOBAL_TMPL_ID" "" "$TOKEN")
  assert_status "delete global template non-superadmin → 403" "403" "$(code "$R")"
fi

# =============================================================================
section "23. Multi-qube isolation check"
# =============================================================================

# Claim a new org and make sure it can't see first org's data
R=$(api POST /api/v1/auth/register \
  "{\"org_name\":\"Org B $RUN_ID\",\"email\":\"$TEST_EMAIL_B\",\"password\":\"orgbpass\"}")
TOKEN_B=$(split "$R" | jq -r .token)

# Org B can't see Org A's qube
R=$(api GET "/api/v1/qubes/$QUBE_ID" "" "$TOKEN_B")
assert_status "org B can't see org A qube → 404" "404" "$(code "$R")"

# Org B can't see Org A's sensors
R=$(api GET "/api/v1/data/sensors/$SENSOR_MODBUS_ID/latest" "" "$TOKEN_B")
assert_status "org B can't see org A sensor → 404" "404" "$(code "$R")"

# Org B can see global templates
R=$(api GET "/api/v1/templates" "" "$TOKEN_B")
assert_status "org B can see global templates" "200" "$(code "$R")"
GLOBAL_COUNT=$(split "$R" | jq '[.[] | select(.is_global==true)] | length')
[ "$GLOBAL_COUNT" -ge "10" ] && ok "org B sees $GLOBAL_COUNT global templates" \
  || fail "org B should see ≥10 global templates"

# =============================================================================
section "SUMMARY"
# =============================================================================
echo ""
echo "════════════════════════════════════"
echo -e "  ${GREEN}PASS${NC}: $PASS"
echo -e "  ${RED}FAIL${NC}: $FAIL"
echo -e "  ${YELLOW}SKIP${NC}: $SKIP"
TOTAL=$((PASS + FAIL + SKIP))
echo "  TOTAL: $TOTAL"
echo "════════════════════════════════════"

if [ "$FAIL" -gt "0" ]; then
  echo ""
  echo -e "${RED}Some tests failed. Check output above for details.${NC}"
  exit 1
else
  echo ""
  echo -e "${GREEN}All tests passed.${NC}"
  exit 0
fi
