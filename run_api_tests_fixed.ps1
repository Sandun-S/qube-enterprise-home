# Qube Enterprise — Full API Test Suite
# PowerShell — uses curl.exe directly

$BASE   = "http://localhost:8080"
$TPBASE = "http://localhost:8081"
$script:results = [System.Collections.Generic.List[PSObject]]::new()
$V = @{}   # shared variable bag

function T {
    param(
        [string]$Id,
        [string]$Name,
        [string]$Method,
        [string]$Url,
        [string]$Body,
        [string[]]$H,
        [int]$WantStatus = 200
    )
    $args2 = [System.Collections.Generic.List[string]]::new()
    $args2.AddRange([string[]]@("-s", "-w", "`n__S__%{http_code}", "-X", $Method, $Url))
    if ($H) { foreach ($hdr in $H) { $args2.Add("-H"); $args2.Add($hdr) } }
    if ($Body) { $args2.Add("-H"); $args2.Add("Content-Type: application/json"); $args2.Add("-d"); $args2.Add($Body) }

    try {
        $raw   = (curl.exe @args2 2>&1) -join "`n"
        $split = $raw -split "`n__S__"
        $body  = $split[0].Trim()
        $code  = if ($split.Count -gt 1) { $split[1].Trim() } else { "???" }
    } catch {
        $body = "EXCEPTION: $($_.Exception.Message)"
        $code = "ERR"
    }

    $pass = ($code -eq $WantStatus.ToString())

    $o = [PSCustomObject]@{
        Id       = $Id
        Name     = $Name
        Method   = $Method
        Url      = $Url
        Body     = if ($Body) { $Body } else { "" }
        GotCode  = $code
        WantCode = $WantStatus
        Response = $body
        Pass     = $pass
    }
    $script:results.Add($o)

    $icon = if ($pass) { "PASS" } else { "FAIL" }
    $col  = if ($pass) { "Green" } else { "Red" }
    Write-Host "  [$icon] $Id $Name  (HTTP $code)" -ForegroundColor $col
    if (-not $pass) { Write-Host "         $body" -ForegroundColor Yellow }

    return $body
}

# ======================================================
Write-Host ""
Write-Host "======================================================" -ForegroundColor Cyan
Write-Host "  QUBE ENTERPRISE FULL API TEST SUITE" -ForegroundColor Cyan
Write-Host "  $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan

# ======================================================
Write-Host "`n[SECTION 0] Health Checks" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/health"
$r = T "0.1" "Cloud API health" GET $url -WantStatus 200
$url = $TPBASE + "/health"
$r = T "0.2" "TP-API health" GET $url -WantStatus 200

# ======================================================
Write-Host "`n[SECTION 1] Authentication" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/auth/register"

$r = T "1.1" "Register Acme Corp" POST $url `
    -Body '{"org_name":"Acme Corp","email":"admin@acme.com","password":"secret123"}' `
    -WantStatus 200
try { $V["ACME_TOKEN"] = ($r | ConvertFrom-Json).token } catch {}

$r = T "1.1b" "Register Test Org" POST $url `
    -Body '{"org_name":"Test Org","email":"test@test.com","password":"pass1234"}' `
    -WantStatus 200
try { $V["TOKEN"] = ($r | ConvertFrom-Json).token; Write-Host "    TOKEN=$($V['TOKEN'].Substring(0,20))..." } catch {}

$r = T "1.2" "Register duplicate email (expect 409)" POST $url `
    -Body '{"org_name":"Another","email":"test@test.com","password":"pass1234"}' `
    -WantStatus 409

$r = T "1.3" "Register missing fields (expect 400)" POST $url `
    -Body '{"org_name":"No Email"}' `
    -WantStatus 400

$url = $BASE + "/api/v1/auth/login"
$r = T "1.4" "Login valid credentials" POST $url `
    -Body '{"email":"test@test.com","password":"pass1234"}' `
    -WantStatus 200
try { $t = ($r | ConvertFrom-Json).token; if ($t) { $V["TOKEN"] = $t } } catch {}

$r = T "1.5" "Login wrong password (expect 401)" POST $url `
    -Body '{"email":"test@test.com","password":"wrongpass"}' `
    -WantStatus 401

$r = T "1.6" "Login superadmin" POST $url `
    -Body '{"email":"iotteam@internal.local","password":"iotteam2024"}' `
    -WantStatus 200
try { $V["SA_TOKEN"] = ($r | ConvertFrom-Json).token; $saRole = ($r | ConvertFrom-Json).role; Write-Host "    SA role=$saRole" } catch {}

$url = $BASE + "/api/v1/qubes"
$r = T "1.7" "No Token -> 401" GET $url -WantStatus 401

$r = T "1.8" "Invalid Token -> 401" GET $url `
    -H @("Authorization: Bearer invalidtoken123") `
    -WantStatus 401

# ======================================================
Write-Host "`n[SECTION 2] Qubes" -ForegroundColor Magenta
# ======================================================
$AH = @("Authorization: Bearer $($V['TOKEN'])")   # auth header array

$url = $BASE + "/api/v1/qubes"
$r = T "2.1" "List qubes empty" GET $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/qubes/claim"
$r = T "2.2" "Claim Q-1001 by register_key" POST $url `
    -H $AH -Body '{"register_key":"TEST-Q1001-REG"}' -WantStatus 200
try {
    $obj = $r | ConvertFrom-Json
    $V["QUBE_TOKEN"] = $obj.auth_token
    Write-Host "    qube_id=$($obj.qube_id)  QUBE_TOKEN captured: $($V['QUBE_TOKEN'] -ne $null)"
} catch {}

$r = T "2.3" "Claim already claimed (expect 409)" POST $url `
    -H $AH -Body '{"register_key":"TEST-Q1001-REG"}' -WantStatus 409

$r = T "2.4" "Claim wrong key (expect 404)" POST $url `
    -H $AH -Body '{"register_key":"XXXX-YYYY-ZZZZ"}' -WantStatus 404

$r = T "2.5" "Claim Q-1002 by qube_id" POST $url `
    -H $AH -Body '{"qube_id":"Q-1002"}' -WantStatus 200

$url = $BASE + "/api/v1/qubes"
$r = T "2.7" "List qubes after claiming" GET $url -H $AH -WantStatus 200
Write-Host "    Qubes listed: $(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/qubes/Q-1001"
$r = T "2.8" "Get qube detail Q-1001" GET $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/qubes/Q-1003"
$r = T "2.9" "Get qube not yours (expect 404)" GET $url -H $AH -WantStatus 404

$url = $BASE + "/api/v1/qubes/Q-1001"
$r = T "2.10a" "Update qube location label" PUT $url `
    -H $AH -Body '{"location_label":"Building A - Floor 2"}' -WantStatus 200

$r = T "2.10b" "Verify location label" GET $url -H $AH -WantStatus 200
Write-Host "    location_label=$(try{($r|ConvertFrom-Json).location_label}catch{'?'})"

# ======================================================
Write-Host "`n[SECTION 3] Commands" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/qubes/Q-1001/commands"
$r = T "3.1" "Send ping command" POST $url `
    -H $AH -Body '{"command":"ping","payload":{"target":"8.8.8.8"}}' -WantStatus 200
try { $V["CMD_ID"] = ($r | ConvertFrom-Json).command_id; Write-Host "    CMD_ID=$($V['CMD_ID'])" } catch {}

$url = $BASE + "/api/v1/commands/" + $V["CMD_ID"]
$r = T "3.2" "Poll command result" GET $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/qubes/Q-1001/commands"
$r = T "3.3" "Send reload_config" POST $url `
    -H $AH -Body '{"command":"reload_config","payload":{}}' -WantStatus 200

$r = T "3.4" "Send list_containers" POST $url `
    -H $AH -Body '{"command":"list_containers","payload":{}}' -WantStatus 200

$r = T "3.5" "Send restart_service" POST $url `
    -H $AH -Body '{"command":"restart_service","payload":{"service":"panel-a"}}' -WantStatus 200

$r = T "3.6" "Send get_logs" POST $url `
    -H $AH -Body '{"command":"get_logs","payload":{"service":"panel-a","lines":50}}' -WantStatus 200

$r = T "3.7" "Unknown command format_disk (expect 400)" POST $url `
    -H $AH -Body '{"command":"format_disk","payload":{}}' -WantStatus 400

$url = $BASE + "/api/v1/qubes/Q-1003/commands"
$r = T "3.8" "Command to other org qube (expect 404)" POST $url `
    -H $AH -Body '{"command":"ping","payload":{}}' -WantStatus 404

# ======================================================
Write-Host "`n[SECTION 4] Templates" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/templates"
$r = T "4.1" "List all global templates" GET $url -H $AH -WantStatus 200
Write-Host "    Template count: $(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/templates?protocol=modbus_tcp"
$r = T "4.2a" "Filter protocol=modbus_tcp" GET $url -H $AH -WantStatus 200
try { $arr = $r | ConvertFrom-Json; $V["MODBUS_TMPL"] = $arr[0].id; $V["TMPL_ID"] = $arr[0].id; Write-Host "    MODBUS_TMPL=$($V['MODBUS_TMPL'])" } catch {}

$url = $BASE + "/api/v1/templates?protocol=snmp"
$r = T "4.2b" "Filter protocol=snmp" GET $url -H $AH -WantStatus 200
try { $arr = $r | ConvertFrom-Json; $V["SNMP_TMPL"] = $arr[0].id } catch {}

$url = $BASE + "/api/v1/templates?protocol=opcua"
$r = T "4.2c" "Filter protocol=opcua" GET $url -H $AH -WantStatus 200
try { $arr = $r | ConvertFrom-Json; $V["OPCUA_TMPL"] = $arr[0].id } catch {}

$url = $BASE + "/api/v1/templates?protocol=mqtt"
$r = T "4.2d" "Filter protocol=mqtt" GET $url -H $AH -WantStatus 200
try { $arr = $r | ConvertFrom-Json; $V["MQTT_TMPL"] = $arr[0].id } catch {}

$url = $BASE + "/api/v1/templates/" + $V["TMPL_ID"]
$r = T "4.3" "Get template detail" GET $url -H $AH -WantStatus 200

$previewUrl = $BASE + "/api/v1/templates/" + $V["TMPL_ID"] + "/preview?address_params=%7B%22unit_id%22%3A1%7D"
$r = T "4.4" "Preview template CSV" GET $previewUrl -H $AH -WantStatus 200

$modbusTmplBody = '{"name":"ABB B23 Energy Meter","protocol":"modbus_tcp","description":"ABB B23","config_json":{"registers":[{"address":0,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"},{"address":40,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"active_power_w","table":"Measurements"},{"address":76,"register_type":"Holding","data_type":"float32","count":2,"scale":0.001,"field_key":"energy_kwh","table":"Measurements"}]},"influx_fields_json":{"voltage_l1":{"display_label":"Voltage L1","unit":"V"},"active_power_w":{"display_label":"Active Power","unit":"W"},"energy_kwh":{"display_label":"Energy","unit":"kWh"}}}'
$url = $BASE + "/api/v1/templates"
$r = T "4.5" "Create org template Modbus" POST $url -H $AH -Body $modbusTmplBody -WantStatus 200
try { $V["ORG_TMPL_ID"] = ($r | ConvertFrom-Json).id; Write-Host "    ORG_TMPL_ID=$($V['ORG_TMPL_ID'])" } catch {}

$snmpBody = '{"name":"Custom UPS Monitor","protocol":"snmp","description":"Custom UPS","config_json":{"oids":[{"oid":"1.3.6.1.4.1.318.1.1.1.2.2.1.0","field_key":"battery_pct","type":"gauge"}]},"influx_fields_json":{"battery_pct":{"display_label":"Battery","unit":"%"}}}'
$r = T "4.6" "Create org template SNMP" POST $url -H $AH -Body $snmpBody -WantStatus 200

$opcuaBody = '{"name":"Custom OPC-UA Sensor","protocol":"opcua","config_json":{"nodes":[{"node_id":"ns=2;points/Temperature","field_key":"temperature","data_type":"float","table":"Measurements"}]}}'
$r = T "4.7" "Create org template OPC-UA" POST $url -H $AH -Body $opcuaBody -WantStatus 200

$mqttBody = '{"name":"Custom MQTT Sensor","protocol":"mqtt","config_json":{"topic_pattern":"{base_topic}/{topic_suffix}","readings":[{"json_path":"$.temp","field_key":"temperature","unit":"C"}]}}'
$r = T "4.8" "Create org template MQTT" POST $url -H $AH -Body $mqttBody -WantStatus 200

$r = T "4.9" "Create template invalid protocol (expect 400)" POST $url `
    -H $AH -Body '{"name":"Bad","protocol":"bacnet","config_json":{}}' -WantStatus 400

$updBody = '{"name":"ABB B23 Energy Meter v2","description":"Updated","config_json":{"registers":[{"address":0,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"}]},"influx_fields_json":{"voltage_l1":{"display_label":"Voltage L1","unit":"V"}}}'
$url = $BASE + "/api/v1/templates/" + $V["ORG_TMPL_ID"]
$r = T "4.10" "Update full org template" PUT $url -H $AH -Body $updBody -WantStatus 200

$SAH = @("Authorization: Bearer $($V['SA_TOKEN'])")
$patchAdd = '{"action":"add","entry":{"address":100,"register_type":"Holding","data_type":"uint16","count":1,"scale":0.01,"field_key":"power_factor","table":"Measurements"}}'
$url = $BASE + "/api/v1/templates/" + $V["TMPL_ID"] + "/registers"
$r = T "4.11" "Patch register add (superadmin)" PATCH $url -H $SAH -Body $patchAdd -WantStatus 200

$patchUpd = '{"action":"update","index":0,"entry":{"address":3000,"register_type":"Holding","data_type":"float32","count":2,"scale":0.1,"field_key":"active_power_w","table":"Measurements"}}'
$r = T "4.12" "Patch register update (superadmin)" PATCH $url -H $SAH -Body $patchUpd -WantStatus 200

$r = T "4.13" "Patch register delete (superadmin)" PATCH $url `
    -H $SAH -Body '{"action":"delete","index":0}' -WantStatus 200

$r = T "4.14" "Patch global template regular user (expect 403)" PATCH $url `
    -H $AH -Body '{"action":"add","entry":{"address":999}}' -WantStatus 403

$r = T "4.15" "Patch invalid index (expect 400)" PATCH $url `
    -H $SAH -Body '{"action":"delete","index":9999}' -WantStatus 400

$url = $BASE + "/api/v1/templates/" + $V["ORG_TMPL_ID"]
$r = T "4.16" "Delete org template" DELETE $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/templates/" + $V["TMPL_ID"]
$r = T "4.17" "Delete global template regular user (expect 403)" DELETE $url -H $AH -WantStatus 403

# ======================================================
Write-Host "`n[SECTION 5] Gateways" -ForegroundColor Magenta
# ======================================================
# Refresh template IDs
$url = $BASE + "/api/v1/templates?protocol=modbus_tcp"
try { $V["MODBUS_TMPL"] = ((curl.exe -s $url -H "Authorization: Bearer $($V['TOKEN'])") | ConvertFrom-Json)[0].id } catch {}
$url = $BASE + "/api/v1/templates?protocol=snmp"
try { $V["SNMP_TMPL"] = ((curl.exe -s $url -H "Authorization: Bearer $($V['TOKEN'])") | ConvertFrom-Json)[0].id } catch {}
$url = $BASE + "/api/v1/templates?protocol=opcua"
try { $V["OPCUA_TMPL"] = ((curl.exe -s $url -H "Authorization: Bearer $($V['TOKEN'])") | ConvertFrom-Json)[0].id } catch {}
$url = $BASE + "/api/v1/templates?protocol=mqtt"
try { $V["MQTT_TMPL"] = ((curl.exe -s $url -H "Authorization: Bearer $($V['TOKEN'])") | ConvertFrom-Json)[0].id } catch {}
Write-Host "    Template IDs — MOD=$($V['MODBUS_TMPL']) SNMP=$($V['SNMP_TMPL']) OPC=$($V['OPCUA_TMPL']) MQTT=$($V['MQTT_TMPL'])"

$url = $BASE + "/api/v1/qubes/Q-1001/gateways"
$r = T "5.1" "List gateways empty" GET $url -H $AH -WantStatus 200

$r = T "5.2" "Add Modbus gateway Panel_A" POST $url `
    -H $AH -Body '{"name":"Panel_A","protocol":"modbus_tcp","host":"192.168.1.100","port":502,"config_json":{"unit_id":1,"poll_interval_ms":5000}}' `
    -WantStatus 200
try { $V["GW_MODBUS"] = ($r | ConvertFrom-Json).gateway_id; Write-Host "    GW_MODBUS=$($V['GW_MODBUS'])" } catch {}

$r = T "5.3" "Add Modbus gateway Panel_B" POST $url `
    -H $AH -Body '{"name":"Panel_B","protocol":"modbus_tcp","host":"192.168.1.101","port":502,"config_json":{"unit_id":1,"poll_interval_ms":5000}}' `
    -WantStatus 200
try { $V["GW_MODBUS2"] = ($r | ConvertFrom-Json).gateway_id; Write-Host "    GW_MODBUS2=$($V['GW_MODBUS2'])" } catch {}

$r = T "5.4" "Add OPC-UA gateway PlantOPC" POST $url `
    -H $AH -Body '{"name":"PlantOPC","protocol":"opcua","host":"opc.tcp://192.168.1.18:52520/OPCUA/Server","port":52520}' `
    -WantStatus 200
try { $V["GW_OPCUA"] = ($r | ConvertFrom-Json).gateway_id } catch {}

$r = T "5.5" "Add SNMP gateway UPS_Room1" POST $url `
    -H $AH -Body '{"name":"UPS_Room1","protocol":"snmp","host":"192.168.1.200","config_json":{"community":"public","version":"2c"}}' `
    -WantStatus 200
try { $V["GW_SNMP"] = ($r | ConvertFrom-Json).gateway_id } catch {}

$r = T "5.6" "Add MQTT gateway MQTTFloor2" POST $url `
    -H $AH -Body '{"name":"MQTTFloor2","protocol":"mqtt","host":"192.168.1.10","port":1883,"config_json":{"broker_url":"tcp://192.168.1.10:1883","base_topic":"factory/floor2"}}' `
    -WantStatus 200
try { $V["GW_MQTT"] = ($r | ConvertFrom-Json).gateway_id } catch {}

$r = T "5.7" "Add gateway invalid protocol bacnet (expect 400)" POST $url `
    -H $AH -Body '{"name":"Bad","protocol":"bacnet","host":"1.2.3.4"}' -WantStatus 400

$r = T "5.8" "Add gateway missing name (expect 400)" POST $url `
    -H $AH -Body '{"protocol":"modbus_tcp","host":"192.168.1.100"}' -WantStatus 400

$url = $BASE + "/api/v1/qubes/Q-1003/gateways"
$r = T "5.9" "Add gateway qube not yours (expect 404)" POST $url `
    -H $AH -Body '{"name":"Test","protocol":"modbus_tcp","host":"1.2.3.4"}' -WantStatus 404

$url = $BASE + "/api/v1/qubes/Q-1001/gateways"
$r = T "5.10" "List gateways all 5" GET $url -H $AH -WantStatus 200
Write-Host "    Gateway count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/gateways/" + $V["GW_MODBUS2"]
$r = T "5.11a" "Delete gateway Panel_B" DELETE $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/qubes/Q-1001/gateways"
$r = T "5.11b" "List gateways after delete (expect 4)" GET $url -H $AH -WantStatus 200
Write-Host "    Gateway count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/gateways/00000000-0000-0000-0000-000000000000"
$r = T "5.12" "Delete gateway not yours (expect 404)" DELETE $url -H $AH -WantStatus 404

# ======================================================
Write-Host "`n[SECTION 6] Sensors" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/gateways/" + $V["GW_MODBUS"] + "/sensors"
$r = T "6.1" "List sensors empty" GET $url -H $AH -WantStatus 200

$s6_2 = '{"name":"Main_Meter","template_id":"MODBUS","address_params":{"unit_id":1,"register_offset":0},"tags_json":{"location":"panel_a","building":"HQ"}}'.Replace("MODBUS", $V["MODBUS_TMPL"])
$r = T "6.2" "Add Modbus sensor Main_Meter" POST $url -H $AH -Body $s6_2 -WantStatus 200
try { $V["SENSOR_MODBUS"] = ($r | ConvertFrom-Json).sensor_id; Write-Host "    SENSOR_MODBUS=$($V['SENSOR_MODBUS'])" } catch {}

$s6_3 = '{"name":"Sub_Meter_1","template_id":"MODBUS","address_params":{"unit_id":2},"tags_json":{"location":"panel_a","circuit":"sub1"}}'.Replace("MODBUS", $V["MODBUS_TMPL"])
$r = T "6.3" "Add Modbus sensor Sub_Meter_1" POST $url -H $AH -Body $s6_3 -WantStatus 200
try { $V["SENSOR_MODBUS2"] = ($r | ConvertFrom-Json).sensor_id } catch {}

$url = $BASE + "/api/v1/gateways/" + $V["GW_OPCUA"] + "/sensors"
$s6_4 = '{"name":"Pasteuriser_1","template_id":"OPCUA","address_params":{"freq_sec":15},"tags_json":{"line":"line1"}}'.Replace("OPCUA", $V["OPCUA_TMPL"])
$r = T "6.4" "Add OPC-UA sensor Pasteuriser_1" POST $url -H $AH -Body $s6_4 -WantStatus 200
try { $V["SENSOR_OPCUA"] = ($r | ConvertFrom-Json).sensor_id } catch {}

$url = $BASE + "/api/v1/gateways/" + $V["GW_SNMP"] + "/sensors"
$s6_5 = '{"name":"UPS_Main","template_id":"SNMPT","address_params":{"community":"public"},"tags_json":{"location":"server_room"}}'.Replace("SNMPT", $V["SNMP_TMPL"])
$r = T "6.5" "Add SNMP sensor UPS_Main" POST $url -H $AH -Body $s6_5 -WantStatus 200
try { $V["SENSOR_SNMP"] = ($r | ConvertFrom-Json).sensor_id } catch {}

$url = $BASE + "/api/v1/gateways/" + $V["GW_MQTT"] + "/sensors"
$s6_6 = '{"name":"Env_Sensor_01","template_id":"MQTTI","address_params":{"topic_suffix":"env_01"},"tags_json":{"floor":"2"}}'.Replace("MQTTI", $V["MQTT_TMPL"])
$r = T "6.6" "Add MQTT sensor Env_Sensor_01" POST $url -H $AH -Body $s6_6 -WantStatus 200
try { $V["SENSOR_MQTT"] = ($r | ConvertFrom-Json).sensor_id } catch {}

$url = $BASE + "/api/v1/gateways/" + $V["GW_MODBUS"] + "/sensors"
$s6_7 = '{"name":"Bad","template_id":"SNMPT","address_params":{}}'.Replace("SNMPT", $V["SNMP_TMPL"])
$r = T "6.7" "Add sensor protocol mismatch (expect 400)" POST $url -H $AH -Body $s6_7 -WantStatus 400

$r = T "6.8" "Add sensor template not found (expect 404)" POST $url `
    -H $AH -Body '{"name":"Bad","template_id":"00000000-0000-0000-0000-000000000000","address_params":{}}' -WantStatus 404

$r = T "6.9" "List sensors for modbus gateway" GET $url -H $AH -WantStatus 200
Write-Host "    Sensor count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/qubes/Q-1001/sensors"
$r = T "6.10" "List all sensors for qube" GET $url -H $AH -WantStatus 200
Write-Host "    Total sensor count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"

$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS2"]
$r = T "6.11" "Delete sensor Sub_Meter_1" DELETE $url -H $AH -WantStatus 200

# ======================================================
Write-Host "`n[SECTION 7] Sensor CSV Rows" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows"
$r = T "7.1" "View all rows for sensor" GET $url -H $AH -WantStatus 200
try {
    $rowsObj = $r | ConvertFrom-Json
    $V["ROW_ID"] = $rowsObj.rows[0].id
    Write-Host "    ROW_ID=$($V['ROW_ID'])  rows count=$($rowsObj.rows.Count)"
} catch {}

$updRow = '{"row_data":{"Equipment":"Main_Meter","Reading":"active_power_w","RegType":"Holding","Address":3002,"Type":"float32","Output":"influxdb","Table":"Measurements","Tags":"location=panel_a,building=HQ"}}'
$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows/" + $V["ROW_ID"]
$r = T "7.2" "Update row fix address" PUT $url -H $AH -Body $updRow -WantStatus 200

$addRow = '{"row_data":{"Equipment":"Main_Meter","Reading":"reactive_power_var","RegType":"Holding","Address":3060,"Type":"float32","Output":"influxdb","Table":"Measurements","Tags":"location=panel_a"}}'
$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows"
$r = T "7.3" "Add extra row new reading" POST $url -H $AH -Body $addRow -WantStatus 200

# Get last row ID
$rRows = & curl.exe -s ($BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows") -H "Authorization: Bearer $($V['TOKEN'])"
try { $V["LAST_ROW_ID"] = ($rRows | ConvertFrom-Json).rows[-1].id; Write-Host "    LAST_ROW_ID=$($V['LAST_ROW_ID'])" } catch {}

$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows/" + $V["LAST_ROW_ID"]
$r = T "7.4" "Delete last row" DELETE $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/sensors/" + $V["SENSOR_MODBUS"] + "/rows/00000000-0000-0000-0000-000000000000"
$r = T "7.5" "Update wrong row_id (expect 404)" PUT $url `
    -H $AH -Body '{"row_data":{"Address":999}}' -WantStatus 404

# ======================================================
Write-Host "`n[SECTION 8] TP-API (Qube-facing)" -ForegroundColor Magenta
# ======================================================
$QH = @("X-Qube-ID: Q-1001", "Authorization: Bearer $($V['QUBE_TOKEN'])")

$url = $TPBASE + "/v1/device/register"
$r = T "8.1" "Device register pending Q-1003 (expect 202)" POST $url `
    -Body '{"device_id":"Q-1003","register_key":"TEST-Q1003-REG"}' -WantStatus 202

$r = T "8.2" "Device register already claimed Q-1001" POST $url `
    -Body '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' -WantStatus 200

$r = T "8.3" "Device register wrong key (expect 401)" POST $url `
    -Body '{"device_id":"Q-1001","register_key":"WRONG-KEY-HERE"}' -WantStatus 401

$url = $TPBASE + "/v1/heartbeat"
$r = T "8.4a" "Heartbeat valid" POST $url -H $QH -Body '{}' -WantStatus 200
Write-Host "    acknowledged=$(try{($r|ConvertFrom-Json).acknowledged}catch{'?'})"

$url = $BASE + "/api/v1/qubes/Q-1001"
$r = T "8.4b" "Verify qube online after heartbeat" GET $url -H $AH -WantStatus 200
Write-Host "    status=$(try{($r|ConvertFrom-Json).status}catch{'?'})"

$url = $TPBASE + "/v1/heartbeat"
$r = T "8.5" "Heartbeat invalid token (expect 401)" POST $url `
    -H @("X-Qube-ID: Q-1001","Authorization: Bearer wrongtoken") -Body '{}' -WantStatus 401

$r = T "8.6" "Heartbeat missing headers (expect 401)" POST $url -Body '{}' -WantStatus 401

$url = $TPBASE + "/v1/sync/state"
$r = T "8.7" "Sync state check hash" GET $url -H $QH -WantStatus 200
try { $V["TP_HASH"] = ($r | ConvertFrom-Json).hash; Write-Host "    hash=$($V['TP_HASH'])" } catch {}

$url = $TPBASE + "/v1/sync/config"
$r = T "8.8" "Sync config download full" GET $url -H $QH -WantStatus 200
try {
    $cfg = $r | ConvertFrom-Json
    $csvCount = ($cfg.csv_files | Get-Member -MemberType NoteProperty).Count
    $smapCount = ($cfg.sensor_map | Get-Member -MemberType NoteProperty).Count
    Write-Host "    csv_files=$csvCount  sensor_map_keys=$smapCount"
} catch {}

$r = T "8.9" "Sync config verify CSV format" GET $url -H $QH -WantStatus 200
try {
    $cfg = $r | ConvertFrom-Json
    $csvKeys = ($cfg.csv_files | Get-Member -MemberType NoteProperty).Name
    Write-Host "    csv_files keys: $($csvKeys -join ', ')"
} catch {}

$r = T "8.10" "Sync config verify sensor_map" GET $url -H $QH -WantStatus 200
try {
    $cfg = $r | ConvertFrom-Json
    $smKeys = ($cfg.sensor_map | Get-Member -MemberType NoteProperty).Name
    Write-Host "    sensor_map keys: $($smKeys -join ', ')"
} catch {}

# Send command then poll
$cmdRaw = & curl.exe -s -X POST ($BASE + "/api/v1/qubes/Q-1001/commands") `
    -H "Authorization: Bearer $($V['TOKEN'])" -H "Content-Type: application/json" `
    -d '{"command":"ping","payload":{"target":"8.8.8.8"}}'
try { $V["POLL_CMD_ID"] = ($cmdRaw | ConvertFrom-Json).command_id; Write-Host "    POLL_CMD_ID=$($V['POLL_CMD_ID'])" } catch {}

$url = $TPBASE + "/v1/commands/poll"
$r = T "8.11" "Poll commands" POST $url -H $QH -Body '{}' -WantStatus 200
Write-Host "    commands=$(try{($r|ConvertFrom-Json).commands.Count}catch{'?'})"

$ack = '{"status":"executed","result":{"output":"PING 8.8.8.8: 64 bytes, time=12ms","latency_ms":12}}'
$url = $TPBASE + "/v1/commands/" + $V["POLL_CMD_ID"] + "/ack"
$r = T "8.12a" "Acknowledge command" POST $url -H $QH -Body $ack -WantStatus 200

$url = $BASE + "/api/v1/commands/" + $V["POLL_CMD_ID"]
$r = T "8.12b" "Verify command executed via Cloud API" GET $url -H $AH -WantStatus 200
Write-Host "    cmd status=$(try{($r|ConvertFrom-Json).status}catch{'?'})"

$now = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
$telBody = '[{"time":"NOW","sensor_id":"SID","field_key":"active_power_w","value":1250.5,"unit":"W"},{"time":"NOW","sensor_id":"SID","field_key":"voltage_ll_v","value":231.2,"unit":"V"},{"time":"NOW","sensor_id":"SID","field_key":"current_a","value":5.4,"unit":"A"},{"time":"NOW","sensor_id":"SID","field_key":"energy_kwh","value":12045.3,"unit":"kWh"}]'
$telBody = '{"readings":' + $telBody.Replace("NOW",$now).Replace("SID",$V["SENSOR_MODBUS"]) + '}'
$url = $TPBASE + "/v1/telemetry/ingest"
$r = T "8.13" "Telemetry ingest 4 readings" POST $url -H $QH -Body $telBody -WantStatus 200
Write-Host "    inserted=$(try{($r|ConvertFrom-Json).inserted}catch{'?'})"

$r = T "8.15" "Telemetry ingest empty batch" POST $url -H $QH -Body '{"readings":[]}' -WantStatus 200

# ======================================================
Write-Host "`n[SECTION 9] Telemetry Data Queries" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/data/sensors/" + $V["SENSOR_MODBUS"] + "/latest"
$r = T "9.1" "Latest values after ingest" GET $url -H $AH -WantStatus 200
try {
    $lat = $r | ConvertFrom-Json
    Write-Host "    sensor=$($lat.sensor_name)  fields=$($lat.fields.Count)"
} catch {}

$url = $BASE + "/api/v1/data/sensors/" + $V["SENSOR_OPCUA"] + "/latest"
$r = T "9.2" "Latest values sensor with no data" GET $url -H $AH -WantStatus 200
Write-Host "    fields=$(try{($r|ConvertFrom-Json).fields.Count}catch{'?'}) (expect 0)"

$url = $BASE + "/api/v1/data/readings?sensor_id=" + $V["SENSOR_MODBUS"]
$r = T "9.3" "Historical readings last 24h" GET $url -H $AH -WantStatus 200
Write-Host "    count=$(try{($r|ConvertFrom-Json).count}catch{'?'})"

$url = $BASE + "/api/v1/data/readings?sensor_id=" + $V["SENSOR_MODBUS"] + "&field=active_power_w"
$r = T "9.4" "Historical readings filter by field" GET $url -H $AH -WantStatus 200

$url = $BASE + "/api/v1/data/readings"
$r = T "9.6" "Readings missing sensor_id (expect 400)" GET $url -H $AH -WantStatus 400

$url = $BASE + "/api/v1/data/sensors/00000000-0000-0000-0000-000000000000/latest"
$r = T "9.7" "Readings other org sensor (expect 404)" GET $url -H $AH -WantStatus 404

# ======================================================
Write-Host "`n[SECTION 12] Multi-org Isolation" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/auth/register"
$r = T "12.1" "Register second org Other Corp" POST $url `
    -Body '{"org_name":"Other Corp","email":"admin@other.com","password":"pass1234"}' -WantStatus 200
try { $V["TOKEN2"] = ($r | ConvertFrom-Json).token } catch {}

$AH2 = @("Authorization: Bearer $($V['TOKEN2'])")

$url = $BASE + "/api/v1/qubes/claim"
$r = T "12.2" "Claim Q-1002 for second org" POST $url -H $AH2 `
    -Body '{"register_key":"TEST-Q1002-REG"}' -WantStatus 200

$url = $BASE + "/api/v1/qubes/Q-1001"
$r = T "12.3" "Org2 cannot see Org1 qube (expect 404)" GET $url -H $AH2 -WantStatus 404

$url = $BASE + "/api/v1/data/sensors/" + $V["SENSOR_MODBUS"] + "/latest"
$r = T "12.4" "Org2 cannot see Org1 sensor (expect 404)" GET $url -H $AH2 -WantStatus 404

$url = $BASE + "/api/v1/qubes/Q-1002"
$r = T "12.5" "Org1 cannot see Org2 qube (expect 404)" GET $url -H $AH -WantStatus 404

# ======================================================
Write-Host "`n[SECTION 13] Edge Cases" -ForegroundColor Magenta
# ======================================================
$url = $BASE + "/api/v1/qubes/Q-1001/gateways"
$r = T "13.2" "Gateway name sanitization" POST $url `
    -H $AH -Body '{"name":"Panel A #2 (Main)","protocol":"modbus_tcp","host":"192.168.1.200","port":502}' `
    -WantStatus 200
Write-Host "    service_name=$(try{($r|ConvertFrom-Json).service_name}catch{'?'})"

# ======================================================
Write-Host "`n[FINAL STATE CHECK]" -ForegroundColor Magenta
# ======================================================
$fQubes     = try { (curl.exe -s ($BASE+"/api/v1/qubes") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).Count } catch { "?" }
$fGateways  = try { (curl.exe -s ($BASE+"/api/v1/qubes/Q-1001/gateways") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).Count } catch { "?" }
$fSensors   = try { (curl.exe -s ($BASE+"/api/v1/qubes/Q-1001/sensors") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).Count } catch { "?" }
$fTemplates = try { (curl.exe -s ($BASE+"/api/v1/templates") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).Count } catch { "?" }
$fStatus    = try { (curl.exe -s ($BASE+"/api/v1/qubes/Q-1001") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).status } catch { "?" }
$cloudHash  = try { (curl.exe -s ($BASE+"/api/v1/qubes/Q-1001") -H "Authorization: Bearer $($V['TOKEN'])" | ConvertFrom-Json).config_hash } catch { "?" }
$tpHash     = try { (curl.exe -s ($TPBASE+"/v1/sync/state") -H "X-Qube-ID: Q-1001" -H "Authorization: Bearer $($V['QUBE_TOKEN'])" | ConvertFrom-Json).hash } catch { "?" }

Write-Host "  Qubes (Org1):    $fQubes"
Write-Host "  Gateways Q-1001: $fGateways"
Write-Host "  Sensors Q-1001:  $fSensors"
Write-Host "  Templates:       $fTemplates"
Write-Host "  Qube status:     $fStatus"
Write-Host "  Cloud hash:      $cloudHash"
Write-Host "  TP-API hash:     $tpHash"
$hashMatch = ($cloudHash -eq $tpHash) -and ($cloudHash -ne "?")
Write-Host "  Hash match:      $(if ($hashMatch) { 'YES' } else { 'NO / MISMATCH' })"

# ======================================================
Write-Host "`n======================================================" -ForegroundColor Cyan
Write-Host "  FINAL RESULTS" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan

$passed = ($script:results | Where-Object { $_.Pass }).Count
$failed  = ($script:results | Where-Object { -not $_.Pass }).Count
$total   = $script:results.Count
Write-Host "TOTAL: $total   PASSED: $passed   FAILED: $failed" -ForegroundColor $(if ($failed -gt 0) { "Yellow" } else { "Green" })

Write-Host "`n--- FAILED TESTS ---" -ForegroundColor Red
$script:results | Where-Object { -not $_.Pass } | ForEach-Object {
    Write-Host "  FAIL [$($_.Id)] $($_.Name)"
    Write-Host "       Method: $($_.Method)  URL: $($_.Url)"
    Write-Host "       Got HTTP $($_.GotCode)  Want HTTP $($_.WantCode)"
    Write-Host "       Response: $($_.Response)"
}

# Save JSON
$script:results | ConvertTo-Json -Depth 5 | Out-File "$PSScriptRoot\test_results.json" -Encoding UTF8
Write-Host "`n  Results JSON saved to: $PSScriptRoot\test_results.json" -ForegroundColor Cyan

# Save vars for reference
$V | ConvertTo-Json | Out-File "$PSScriptRoot\test_vars.json" -Encoding UTF8
Write-Host "======================================================`n" -ForegroundColor Cyan
