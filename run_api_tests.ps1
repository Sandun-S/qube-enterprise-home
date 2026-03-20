$BASE   = "http://localhost:8080"
$TPBASE = "http://localhost:8081"
$R = [System.Collections.Generic.List[PSObject]]::new()
$V = @{}

function Run-Curl {
    param([string[]]$Args2)
    $raw = (curl.exe @Args2 2>&1) -join "`n"
    $sp  = $raw -split "`n__S__"
    return @{ Body=$sp[0].Trim(); Code=if($sp.Count-gt 1){$sp[1].Trim()}else{"???"} }
}

function T {
    param([string]$Id,[string]$Name,[string]$Method,[string]$Url,
          [string]$Body="",[string[]]$H=@(),[int]$Want=200)
    $a = [System.Collections.Generic.List[string]]::new()
    $a.AddRange([string[]]@("-s","-w","`n__S__%{http_code}","-X",$Method,$Url))
    foreach($h in $H){$a.Add("-H");$a.Add($h)}
    if($Body){$a.Add("-H");$a.Add("Content-Type: application/json");$a.Add("-d");$a.Add($Body)}
    $res = Run-Curl $a
    $pass = ($res.Code -eq $Want.ToString())
    $R.Add([PSCustomObject]@{Id=$Id;Name=$Name;Method=$Method;Url=$Url;Body=$Body;Got=$res.Code;Want=$Want;Response=$res.Body;Pass=$pass})
    $icon = if($pass){"PASS"}else{"FAIL"}
    $color = if($pass){"Green"}else{"Red"}
    Write-Host "  [$icon] $Id - $Name  (HTTP $($res.Code))" -ForegroundColor $color
    if(-not $pass){Write-Host "         $($res.Body)" -ForegroundColor Yellow}
    return $res.Body
}

function Get-J { param($json,$key) try{($json|ConvertFrom-Json).$key}catch{$null} }

Write-Host "`n======================================================" -ForegroundColor Cyan
Write-Host "  QUBE ENTERPRISE FULL API TEST SUITE" -ForegroundColor Cyan
Write-Host "  $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan

# S0 Health
Write-Host "`n[S0] Health Checks" -ForegroundColor Magenta
T "0.1" "Cloud API health"  GET ($BASE+"/health")  -Want 200 | Out-Null
T "0.2" "TP-API health"     GET ($TPBASE+"/health") -Want 200 | Out-Null

# S1 Auth
Write-Host "`n[S1] Authentication" -ForegroundColor Magenta
$r = T "1.1" "Register Acme Corp" POST ($BASE+"/api/v1/auth/register") '{"org_name":"Acme Corp","email":"admin@acme.com","password":"secret123"}' -Want 200
$r = T "1.1b" "Register Test Org" POST ($BASE+"/api/v1/auth/register") '{"org_name":"Test Org","email":"test@test.com","password":"pass1234"}' -Want 200
$V["TOKEN"] = Get-J $r "token"
Write-Host "   TOKEN=$($V['TOKEN'].Substring(0,[Math]::Min(20,$V['TOKEN'].Length)))..."

$r = T "1.2" "Register dup email (409)" POST ($BASE+"/api/v1/auth/register") '{"org_name":"X","email":"test@test.com","password":"pass1234"}' -Want 409
$r = T "1.3" "Register missing fields (400)" POST ($BASE+"/api/v1/auth/register") '{"org_name":"X"}' -Want 400
$r = T "1.4" "Login valid" POST ($BASE+"/api/v1/auth/login") '{"email":"test@test.com","password":"pass1234"}' -Want 200
$t = Get-J $r "token"; if($t){$V["TOKEN"]=$t}
$r = T "1.5" "Login wrong pass (401)" POST ($BASE+"/api/v1/auth/login") '{"email":"test@test.com","password":"wrongpass"}' -Want 401
$r = T "1.6" "Login superadmin" POST ($BASE+"/api/v1/auth/login") '{"email":"iotteam@internal.local","password":"iotteam2024"}' -Want 200
$V["SA_TOKEN"] = Get-J $r "token"
Write-Host "   SA role=$(Get-J $r 'role')"
T "1.7" "No token (401)"      GET ($BASE+"/api/v1/qubes") -Want 401 | Out-Null
T "1.8" "Invalid token (401)" GET ($BASE+"/api/v1/qubes") @("Authorization: Bearer badtoken") -Want 401 | Out-Null

# S2 Qubes
Write-Host "`n[S2] Qubes" -ForegroundColor Magenta
$AH = @("Authorization: Bearer $($V['TOKEN'])")
T "2.1" "List qubes empty" GET ($BASE+"/api/v1/qubes") -H $AH -Want 200 | Out-Null
$r = T "2.2" "Claim Q-1001" POST ($BASE+"/api/v1/qubes/claim") '{"register_key":"TEST-Q1001-REG"}' $AH -Want 200
$V["QUBE_TOKEN"] = Get-J $r "auth_token"
Write-Host "   qube_id=$(Get-J $r 'qube_id')  QUBE_TOKEN=$(-not [string]::IsNullOrEmpty($V['QUBE_TOKEN']))"
T "2.3" "Claim dup (409)"        POST ($BASE+"/api/v1/qubes/claim") '{"register_key":"TEST-Q1001-REG"}' $AH -Want 409 | Out-Null
T "2.4" "Claim bad key (404)"     POST ($BASE+"/api/v1/qubes/claim") '{"register_key":"XXXX-YYYY-ZZZZ"}' $AH -Want 404 | Out-Null
T "2.5" "Claim Q-1002 by id"     POST ($BASE+"/api/v1/qubes/claim") '{"qube_id":"Q-1002"}' $AH -Want 200 | Out-Null
$r = T "2.7" "List qubes after" GET ($BASE+"/api/v1/qubes") -H $AH -Want 200
Write-Host "   count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"
T "2.8" "Get Q-1001 detail"    GET ($BASE+"/api/v1/qubes/Q-1001") -H $AH -Want 200 | Out-Null
T "2.9" "Get Q-1003 (404)"     GET ($BASE+"/api/v1/qubes/Q-1003") -H $AH -Want 404 | Out-Null
T "2.10a" "Update location"    PUT ($BASE+"/api/v1/qubes/Q-1001") '{"location_label":"Building A - Floor 2"}' $AH -Want 200 | Out-Null
$r = T "2.10b" "Verify location" GET ($BASE+"/api/v1/qubes/Q-1001") -H $AH -Want 200
Write-Host "   location_label=$(Get-J $r 'location_label')"

# S3 Commands
Write-Host "`n[S3] Commands" -ForegroundColor Magenta
$r = T "3.1" "Ping command" POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"ping","payload":{"target":"8.8.8.8"}}' $AH -Want 200
$V["CMD_ID"] = Get-J $r "command_id"; Write-Host "   CMD_ID=$($V['CMD_ID'])"
T "3.2" "Poll cmd result"       GET ($BASE+"/api/v1/commands/"+$V["CMD_ID"]) -H $AH -Want 200 | Out-Null
T "3.3" "reload_config cmd"     POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"reload_config","payload":{}}' $AH -Want 200 | Out-Null
T "3.4" "list_containers cmd"   POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"list_containers","payload":{}}' $AH -Want 200 | Out-Null
T "3.5" "restart_service cmd"   POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"restart_service","payload":{"service":"panel-a"}}' $AH -Want 200 | Out-Null
T "3.6" "get_logs cmd"          POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"get_logs","payload":{"service":"panel-a","lines":50}}' $AH -Want 200 | Out-Null
T "3.7" "Bad cmd (400)"         POST ($BASE+"/api/v1/qubes/Q-1001/commands") '{"command":"format_disk","payload":{}}' $AH -Want 400 | Out-Null
T "3.8" "Cmd wrong qube (404)"  POST ($BASE+"/api/v1/qubes/Q-1003/commands") '{"command":"ping","payload":{}}' $AH -Want 404 | Out-Null

# S4 Templates
Write-Host "`n[S4] Templates" -ForegroundColor Magenta
$r = T "4.1" "List all templates" GET ($BASE+"/api/v1/templates") -H $AH -Want 200
Write-Host "   count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"
$r = T "4.2a" "Filter modbus_tcp" GET ($BASE+"/api/v1/templates?protocol=modbus_tcp") -H $AH -Want 200
try{$arr=$r|ConvertFrom-Json;$V["MODBUS_TMPL"]=$arr[0].id;$V["TMPL_ID"]=$arr[0].id}catch{}
Write-Host "   MODBUS_TMPL=$($V['MODBUS_TMPL'])"
$r = T "4.2b" "Filter snmp" GET ($BASE+"/api/v1/templates?protocol=snmp") -H $AH -Want 200
try{$arr=$r|ConvertFrom-Json;$V["SNMP_TMPL"]=$arr[0].id}catch{}
$r = T "4.2c" "Filter opcua" GET ($BASE+"/api/v1/templates?protocol=opcua") -H $AH -Want 200
try{$arr=$r|ConvertFrom-Json;$V["OPCUA_TMPL"]=$arr[0].id}catch{}
$r = T "4.2d" "Filter mqtt" GET ($BASE+"/api/v1/templates?protocol=mqtt") -H $AH -Want 200
try{$arr=$r|ConvertFrom-Json;$V["MQTT_TMPL"]=$arr[0].id}catch{}
Write-Host "   SNM=$($V['SNMP_TMPL']) OPC=$($V['OPCUA_TMPL']) MQTT=$($V['MQTT_TMPL'])"
T "4.3" "Get template detail" GET ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]) -H $AH  -Want 200 | Out-Null
T "4.4" "Preview template CSV" GET ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/preview?address_params=%7B%22unit_id%22%3A1%7D") -H $AH -Want 200 | Out-Null

$b45='{"name":"ABB B23 Energy Meter","protocol":"modbus_tcp","description":"ABB","config_json":{"registers":[{"address":0,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"},{"address":40,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"active_power_w","table":"Measurements"}]},"influx_fields_json":{"voltage_l1":{"display_label":"Voltage L1","unit":"V"},"active_power_w":{"display_label":"Active Power","unit":"W"}}}'
$r = T "4.5" "Create org template Modbus" POST ($BASE+"/api/v1/templates") $b45 $AH -Want 200
try{$V["ORG_TMPL_ID"]=($r|ConvertFrom-Json).id;Write-Host "   ORG_TMPL_ID=$($V['ORG_TMPL_ID'])"}catch{}

$b46='{"name":"Custom UPS Monitor","protocol":"snmp","config_json":{"oids":[{"oid":"1.3.6.1.4.1.318","field_key":"battery_pct","type":"gauge"}]},"influx_fields_json":{"battery_pct":{"display_label":"Battery","unit":"%"}}}'
T "4.6" "Create org template SNMP" POST ($BASE+"/api/v1/templates") $b46 $AH -Want 200 | Out-Null
$b47='{"name":"Custom OPC-UA","protocol":"opcua","config_json":{"nodes":[{"node_id":"ns=2;temp","field_key":"temperature","data_type":"float","table":"Measurements"}]}}'
T "4.7" "Create org template OPC-UA" POST ($BASE+"/api/v1/templates") $b47 $AH -Want 200 | Out-Null
$b48='{"name":"Custom MQTT","protocol":"mqtt","config_json":{"topic_pattern":"{base_topic}/{topic_suffix}","readings":[{"json_path":"$.temp","field_key":"temperature","unit":"C"}]}}'
T "4.8" "Create org template MQTT" POST ($BASE+"/api/v1/templates") $b48 $AH -Want 200 | Out-Null
T "4.9" "Bad protocol (400)" POST ($BASE+"/api/v1/templates") '{"name":"Bad","protocol":"bacnet","config_json":{}}' $AH -Want 400 | Out-Null

$b410='{"name":"ABB v2","description":"Updated","config_json":{"registers":[{"address":0,"register_type":"Holding","data_type":"float32","count":2,"scale":1.0,"field_key":"voltage_l1","table":"Measurements"}]},"influx_fields_json":{"voltage_l1":{"display_label":"Voltage L1","unit":"V"}}}'
T "4.10" "Update org template" PUT ($BASE+"/api/v1/templates/"+$V["ORG_TMPL_ID"]) $b410 $AH -Want 200 | Out-Null

$SAH = @("Authorization: Bearer $($V['SA_TOKEN'])")
$b411='{"action":"add","entry":{"address":100,"register_type":"Holding","data_type":"uint16","count":1,"scale":0.01,"field_key":"power_factor","table":"Measurements"}}'
T "4.11" "Patch add reg (SA)" PATCH ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/registers") $b411 $SAH -Want 200 | Out-Null
$b412='{"action":"update","index":0,"entry":{"address":3000,"register_type":"Holding","data_type":"float32","count":2,"scale":0.1,"field_key":"active_power_w","table":"Measurements"}}'
T "4.12" "Patch upd reg (SA)" PATCH ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/registers") $b412 $SAH -Want 200 | Out-Null
T "4.13" "Patch del reg (SA)" PATCH ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/registers") '{"action":"delete","index":0}' $SAH -Want 200 | Out-Null
T "4.14" "Patch global (regular user 403)" PATCH ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/registers") '{"action":"add","entry":{"address":999}}' $AH -Want 403 | Out-Null
T "4.15" "Patch bad index (400)" PATCH ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]+"/registers") '{"action":"delete","index":9999}' $SAH -Want 400 | Out-Null
T "4.16" "Delete org template" DELETE ($BASE+"/api/v1/templates/"+$V["ORG_TMPL_ID"]) -H $AH -Want 200 | Out-Null
T "4.17" "Delete global (403)" DELETE ($BASE+"/api/v1/templates/"+$V["TMPL_ID"]) -H $AH -Want 403 | Out-Null

# S5 Gateways
Write-Host "`n[S5] Gateways" -ForegroundColor Magenta
# refresh template IDs
$tmpls = @{modbus_tcp="MODBUS_TMPL";snmp="SNMP_TMPL";opcua="OPCUA_TMPL";mqtt="MQTT_TMPL"}
foreach($proto in $tmpls.Keys){
    $raw = curl.exe -s ($BASE+"/api/v1/templates?protocol="+$proto) -H "Authorization: Bearer $($V['TOKEN'])"
    try{$V[$tmpls[$proto]]=($raw|ConvertFrom-Json)[0].id}catch{}
}
Write-Host "   MOD=$($V['MODBUS_TMPL']) SNM=$($V['SNMP_TMPL']) OPC=$($V['OPCUA_TMPL']) MQT=$($V['MQTT_TMPL'])"

T "5.1" "List gateways empty" GET ($BASE+"/api/v1/qubes/Q-1001/gateways") -H $AH -Want 200 | Out-Null
$r = T "5.2" "Add Modbus GW Panel_A" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"Panel_A","protocol":"modbus_tcp","host":"192.168.1.100","port":502,"config_json":{"unit_id":1,"poll_interval_ms":5000}}' $AH -Want 200
try{$V["GW_MODBUS"]=($r|ConvertFrom-Json).gateway_id;Write-Host "   GW_MODBUS=$($V['GW_MODBUS'])"}catch{}
$r = T "5.3" "Add Modbus GW Panel_B" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"Panel_B","protocol":"modbus_tcp","host":"192.168.1.101","port":502,"config_json":{"unit_id":1}}' $AH -Want 200
try{$V["GW_MODBUS2"]=($r|ConvertFrom-Json).gateway_id}catch{}
$r = T "5.4" "Add OPC-UA GW PlantOPC" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"PlantOPC","protocol":"opcua","host":"opc.tcp://192.168.1.18:52520","port":52520}' $AH -Want 200
try{$V["GW_OPCUA"]=($r|ConvertFrom-Json).gateway_id}catch{}
$r = T "5.5" "Add SNMP GW UPS_Room1" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"UPS_Room1","protocol":"snmp","host":"192.168.1.200","config_json":{"community":"public","version":"2c"}}' $AH -Want 200
try{$V["GW_SNMP"]=($r|ConvertFrom-Json).gateway_id}catch{}
$r = T "5.6" "Add MQTT GW MQTTFloor2" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"MQTTFloor2","protocol":"mqtt","host":"192.168.1.10","port":1883,"config_json":{"broker_url":"tcp://192.168.1.10:1883","base_topic":"factory/floor2"}}' $AH -Want 200
try{$V["GW_MQTT"]=($r|ConvertFrom-Json).gateway_id}catch{}
Write-Host "   GW_OPCUA=$($V['GW_OPCUA']) GW_SNMP=$($V['GW_SNMP']) GW_MQTT=$($V['GW_MQTT'])"
T "5.7" "Add GW bad protocol (400)" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"Bad","protocol":"bacnet","host":"1.2.3.4"}' $AH -Want 400 | Out-Null
T "5.8" "Add GW missing name (400)" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"protocol":"modbus_tcp","host":"192.168.1.100"}' $AH -Want 400 | Out-Null
T "5.9" "Add GW wrong qube (404)"   POST ($BASE+"/api/v1/qubes/Q-1003/gateways") '{"name":"T","protocol":"modbus_tcp","host":"1.2.3.4"}' $AH -Want 404 | Out-Null
$r = T "5.10" "List all 5 gateways" GET ($BASE+"/api/v1/qubes/Q-1001/gateways") -H $AH -Want 200
Write-Host "   count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"
T "5.11a" "Delete GW Panel_B"  DELETE ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS2"]) -H $AH -Want 200 | Out-Null
$r = T "5.11b" "List gateways (4 remain)" GET ($BASE+"/api/v1/qubes/Q-1001/gateways") -H $AH -Want 200
Write-Host "   count=$(try{($r|ConvertFrom-Json).Count}catch{'?'}) (expect 4)"
T "5.12" "Delete fake GW (404)" DELETE ($BASE+"/api/v1/gateways/00000000-0000-0000-0000-000000000000") -H $AH -Want 404 | Out-Null

# S6 Sensors
Write-Host "`n[S6] Sensors" -ForegroundColor Magenta
T "6.1" "List sensors empty" GET ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") -H $AH -Want 200 | Out-Null

$b62='{"name":"Main_Meter","template_id":"__MOD__","address_params":{"unit_id":1,"register_offset":0},"tags_json":{"location":"panel_a","building":"HQ"}}'.Replace("__MOD__",$V["MODBUS_TMPL"])
$r = T "6.2" "Add Modbus sensor Main_Meter" POST ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") $b62 $AH -Want 200
try{$V["SENSOR_MODBUS"]=($r|ConvertFrom-Json).sensor_id;Write-Host "   SENSOR_MODBUS=$($V['SENSOR_MODBUS'])"}catch{}

$b63='{"name":"Sub_Meter_1","template_id":"__MOD__","address_params":{"unit_id":2},"tags_json":{"location":"panel_a"}}'.Replace("__MOD__",$V["MODBUS_TMPL"])
$r = T "6.3" "Add Modbus sensor Sub_Meter_1" POST ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") $b63 $AH -Want 200
try{$V["SENSOR_MODBUS2"]=($r|ConvertFrom-Json).sensor_id}catch{}

$b64='{"name":"Pasteuriser_1","template_id":"__OPC__","address_params":{"freq_sec":15},"tags_json":{"line":"line1"}}'.Replace("__OPC__",$V["OPCUA_TMPL"])
$r = T "6.4" "Add OPC-UA sensor" POST ($BASE+"/api/v1/gateways/"+$V["GW_OPCUA"]+"/sensors") $b64 $AH -Want 200
try{$V["SENSOR_OPCUA"]=($r|ConvertFrom-Json).sensor_id}catch{}

$b65='{"name":"UPS_Main","template_id":"__SNM__","address_params":{"community":"public"},"tags_json":{"location":"server_room"}}'.Replace("__SNM__",$V["SNMP_TMPL"])
$r = T "6.5" "Add SNMP sensor" POST ($BASE+"/api/v1/gateways/"+$V["GW_SNMP"]+"/sensors") $b65 $AH -Want 200
try{$V["SENSOR_SNMP"]=($r|ConvertFrom-Json).sensor_id}catch{}

$b66='{"name":"Env_Sensor_01","template_id":"__MQT__","address_params":{"topic_suffix":"env_01"},"tags_json":{"floor":"2"}}'.Replace("__MQT__",$V["MQTT_TMPL"])
$r = T "6.6" "Add MQTT sensor" POST ($BASE+"/api/v1/gateways/"+$V["GW_MQTT"]+"/sensors") $b66 $AH -Want 200
try{$V["SENSOR_MQTT"]=($r|ConvertFrom-Json).sensor_id}catch{}

$b67='{"name":"Bad","template_id":"__SNM__","address_params":{}}'.Replace("__SNM__",$V["SNMP_TMPL"])
T "6.7" "Sensor proto mismatch (400)" POST ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") $b67 $AH -Want 400 | Out-Null
T "6.8" "Sensor tmpl not found (404)" POST ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") '{"name":"Bad","template_id":"00000000-0000-0000-0000-000000000000","address_params":{}}' $AH -Want 404 | Out-Null
$r = T "6.9" "List sensors modbus GW" GET ($BASE+"/api/v1/gateways/"+$V["GW_MODBUS"]+"/sensors") -H $AH -Want 200
Write-Host "   count=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"
$r = T "6.10" "List all sensors Q-1001" GET ($BASE+"/api/v1/qubes/Q-1001/sensors") -H $AH -Want 200
Write-Host "   total=$(try{($r|ConvertFrom-Json).Count}catch{'?'})"
T "6.11" "Delete Sub_Meter_1" DELETE ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS2"]) -H $AH -Want 200 | Out-Null

# S7 CSV Rows
Write-Host "`n[S7] Sensor CSV Rows" -ForegroundColor Magenta
$r = T "7.1" "View rows for sensor" GET ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows") -H $AH -Want 200
try{$rowsO=$r|ConvertFrom-Json;$V["ROW_ID"]=$rowsO.rows[0].id;Write-Host "   ROW_ID=$($V['ROW_ID']) rows=$($rowsO.rows.Count)"}catch{}
$b72='{"row_data":{"Equipment":"Main_Meter","Reading":"active_power_w","RegType":"Holding","Address":3002,"Type":"float32","Output":"influxdb","Table":"Measurements","Tags":"location=panel_a"}}'
T "7.2" "Update row" PUT ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows/"+$V["ROW_ID"]) $b72 $AH -Want 200 | Out-Null
$b73='{"row_data":{"Equipment":"Main_Meter","Reading":"reactive_power_var","RegType":"Holding","Address":3060,"Type":"float32","Output":"influxdb","Table":"Measurements","Tags":"location=panel_a"}}'
T "7.3" "Add extra row" POST ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows") $b73 $AH -Want 200 | Out-Null

$rawRows = curl.exe -s ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows") -H "Authorization: Bearer $($V['TOKEN'])"
try{$V["LAST_ROW"]=($rawRows|ConvertFrom-Json).rows[-1].id;Write-Host "   LAST_ROW=$($V['LAST_ROW'])"}catch{}
T "7.4" "Delete last row" DELETE ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows/"+$V["LAST_ROW"]) -H $AH -Want 200 | Out-Null
T "7.5" "Update bad row (404)" PUT ($BASE+"/api/v1/sensors/"+$V["SENSOR_MODBUS"]+"/rows/00000000-0000-0000-0000-000000000000") '{"row_data":{"Address":999}}' $AH -Want 404 | Out-Null

# S8 TP-API
Write-Host "`n[S8] TP-API" -ForegroundColor Magenta
$QH = @("X-Qube-ID: Q-1001","Authorization: Bearer $($V['QUBE_TOKEN'])")
T "8.1" "Device reg Q-1003 pending (202)" POST ($TPBASE+"/v1/device/register") '{"device_id":"Q-1003","register_key":"TEST-Q1003-REG"}' -Want 202 | Out-Null
T "8.2" "Device reg Q-1001 claimed (200)" POST ($TPBASE+"/v1/device/register") '{"device_id":"Q-1001","register_key":"TEST-Q1001-REG"}' -Want 200 | Out-Null
T "8.3" "Device reg wrong key (401)"      POST ($TPBASE+"/v1/device/register") '{"device_id":"Q-1001","register_key":"WRONG"}' -Want 401 | Out-Null
$r = T "8.4a" "Heartbeat valid" POST ($TPBASE+"/v1/heartbeat") '{}' $QH -Want 200
Write-Host "   acknowledged=$(Get-J $r 'acknowledged')"
$r = T "8.4b" "Qube online after HB" GET ($BASE+"/api/v1/qubes/Q-1001") -H $AH -Want 200
Write-Host "   status=$(Get-J $r 'status')"
T "8.5" "HB bad token (401)"    POST ($TPBASE+"/v1/heartbeat") '{}' @("X-Qube-ID: Q-1001","Authorization: Bearer bad") -Want 401 | Out-Null
T "8.6" "HB missing hdr (401)"  POST ($TPBASE+"/v1/heartbeat") '{}' -Want 401 | Out-Null
$r = T "8.7" "Sync state hash" GET ($TPBASE+"/v1/sync/state") -H $QH -Want 200
try{$V["TP_HASH"]=($r|ConvertFrom-Json).hash;Write-Host "   hash=$($V['TP_HASH'])"}catch{}
$r = T "8.8" "Sync config full" GET ($TPBASE+"/v1/sync/config") -H $QH -Want 200
try{
    $cfg=$r|ConvertFrom-Json
    $csvCnt=($cfg.csv_files|Get-Member -MemberType NoteProperty).Count
    $smCnt=($cfg.sensor_map|Get-Member -MemberType NoteProperty).Count
    Write-Host "   csv_files=$csvCnt  sensor_map_keys=$smCnt"
}catch{}
T "8.9" "Sync config CSV format" GET ($TPBASE+"/v1/sync/config") -H $QH -Want 200 | Out-Null
T "8.10" "Sync config sensor_map" GET ($TPBASE+"/v1/sync/config") -H $QH -Want 200 | Out-Null

$rawCmd = curl.exe -s -X POST ($BASE+"/api/v1/qubes/Q-1001/commands") -H "Authorization: Bearer $($V['TOKEN'])" -H "Content-Type: application/json" -d '{"command":"ping","payload":{"target":"8.8.8.8"}}'
try{$V["POLL_CMD"]=($rawCmd|ConvertFrom-Json).command_id;Write-Host "   POLL_CMD=$($V['POLL_CMD'])"}catch{}
$r = T "8.11" "Poll commands" POST ($TPBASE+"/v1/commands/poll") '{}' $QH -Want 200
Write-Host "   pending cmds=$(try{($r|ConvertFrom-Json).commands.Count}catch{'?'})"
$ack='{"status":"executed","result":{"output":"PING ok","latency_ms":12}}'
T "8.12a" "Acknowledge cmd"    POST ($TPBASE+"/v1/commands/"+$V["POLL_CMD"]+"/ack") $ack $QH -Want 200 | Out-Null
$r = T "8.12b" "Verify cmd executed" GET ($BASE+"/api/v1/commands/"+$V["POLL_CMD"]) -H $AH -Want 200
Write-Host "   cmd status=$(Get-J $r 'status')"

$now=(Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
$sid=$V["SENSOR_MODBUS"]
$tel='{"readings":[{"time":"__T__","sensor_id":"__S__","field_key":"active_power_w","value":1250.5,"unit":"W"},{"time":"__T__","sensor_id":"__S__","field_key":"voltage_ll_v","value":231.2,"unit":"V"},{"time":"__T__","sensor_id":"__S__","field_key":"current_a","value":5.4,"unit":"A"},{"time":"__T__","sensor_id":"__S__","field_key":"energy_kwh","value":12045.3,"unit":"kWh"}]}'.Replace("__T__",$now).Replace("__S__",$sid)
$r = T "8.13" "Telemetry ingest 4 readings" POST ($TPBASE+"/v1/telemetry/ingest") $tel $QH -Want 200
Write-Host "   inserted=$(Get-J $r 'inserted')"
T "8.15" "Telemetry empty batch" POST ($TPBASE+"/v1/telemetry/ingest") '{"readings":[]}' $QH -Want 200 | Out-Null

# S9 Telemetry Queries
Write-Host "`n[S9] Telemetry Data Queries" -ForegroundColor Magenta
$r = T "9.1" "Latest values after ingest" GET ($BASE+"/api/v1/data/sensors/"+$V["SENSOR_MODBUS"]+"/latest") -H $AH -Want 200
try{$lat=$r|ConvertFrom-Json;Write-Host "   sensor=$($lat.sensor_name) fields=$($lat.fields.Count)"}catch{}
$r = T "9.2" "Latest with no data (OPCUA)" GET ($BASE+"/api/v1/data/sensors/"+$V["SENSOR_OPCUA"]+"/latest") -H $AH -Want 200
Write-Host "   fields=$(try{($r|ConvertFrom-Json).fields.Count}catch{'?'}) (expect 0)"
T "9.3" "Historical last 24h"      GET ($BASE+"/api/v1/data/readings?sensor_id="+$V["SENSOR_MODBUS"]) -H $AH -Want 200 | Out-Null
T "9.4" "Historical filter field"   GET ($BASE+"/api/v1/data/readings?sensor_id="+$V["SENSOR_MODBUS"]+"&field=active_power_w") -H $AH -Want 200 | Out-Null
T "9.6" "Readings no sensor_id (400)" GET ($BASE+"/api/v1/data/readings") -H $AH -Want 400 | Out-Null
T "9.7" "Readings bad sensor (404)"   GET ($BASE+"/api/v1/data/sensors/00000000-0000-0000-0000-000000000000/latest") -H $AH -Want 404 | Out-Null

# S12 Multi-org isolation
Write-Host "`n[S12] Multi-org Isolation" -ForegroundColor Magenta
$r = T "12.1" "Register Org2 Other Corp" POST ($BASE+"/api/v1/auth/register") '{"org_name":"Other Corp","email":"admin@other.com","password":"pass1234"}' -Want 200
try{$V["TOKEN2"]=($r|ConvertFrom-Json).token}catch{}
$AH2=@("Authorization: Bearer $($V['TOKEN2'])")
T "12.2" "Org2 Claim Q-1002"            POST ($BASE+"/api/v1/qubes/claim") '{"register_key":"TEST-Q1002-REG"}' $AH2 -Want 200 | Out-Null
T "12.3" "Org2 cant see Org1 qube (404)"   GET ($BASE+"/api/v1/qubes/Q-1001") -H $AH2 -Want 404 | Out-Null
T "12.4" "Org2 cant see Org1 sensor (404)" GET ($BASE+"/api/v1/data/sensors/"+$V["SENSOR_MODBUS"]+"/latest") -H $AH2 -Want 404 | Out-Null
T "12.5" "Org1 cant see Org2 qube (404)"   GET ($BASE+"/api/v1/qubes/Q-1002") -H $AH -Want 404 | Out-Null

# S13 Edge cases
Write-Host "`n[S13] Edge Cases" -ForegroundColor Magenta
$r = T "13.2" "GW name sanitization" POST ($BASE+"/api/v1/qubes/Q-1001/gateways") '{"name":"Panel A #2 (Main)","protocol":"modbus_tcp","host":"192.168.1.200","port":502}' $AH -Want 200
Write-Host "   service_name=$(Get-J $r 'service_name')"

# Final state
Write-Host "`n[FINAL STATE]" -ForegroundColor Magenta
$fQ  = try{(curl.exe -s ($BASE+"/api/v1/qubes") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Measure-Object|Select-Object -Exp Count}catch{"?"}
$fG  = try{(curl.exe -s ($BASE+"/api/v1/qubes/Q-1001/gateways") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Measure-Object|Select-Object -Exp Count}catch{"?"}
$fS  = try{(curl.exe -s ($BASE+"/api/v1/qubes/Q-1001/sensors") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Measure-Object|Select-Object -Exp Count}catch{"?"}
$fT  = try{(curl.exe -s ($BASE+"/api/v1/templates") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Measure-Object|Select-Object -Exp Count}catch{"?"}
$fSt = try{(curl.exe -s ($BASE+"/api/v1/qubes/Q-1001") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Select-Object -Exp status}catch{"?"}
$cH  = try{(curl.exe -s ($BASE+"/api/v1/qubes/Q-1001") -H "Authorization: Bearer $($V['TOKEN'])")|ConvertFrom-Json|Select-Object -Exp config_hash}catch{"?"}
$tH  = try{(curl.exe -s ($TPBASE+"/v1/sync/state") -H "X-Qube-ID: Q-1001" -H "Authorization: Bearer $($V['QUBE_TOKEN'])")|ConvertFrom-Json|Select-Object -Exp hash}catch{"?"}
Write-Host "  Qubes=$fQ  Gateways=$fG  Sensors=$fS  Templates=$fT  Status=$fSt"
Write-Host "  CloudHash=$cH"
Write-Host "  TP-Hash  =$tH"
Write-Host "  HashMatch=$(if($cH-eq $tH -and $cH-ne '?'){'YES - MATCH'}else{'NO - MISMATCH'})"

# Summary
Write-Host "`n======================================================" -ForegroundColor Cyan
Write-Host "  RESULTS SUMMARY" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan
$passed=($R|Where-Object{$_.Pass}).Count; $failed=($R|Where-Object{-not $_.Pass}).Count
Write-Host "TOTAL: $($R.Count)   PASSED: $passed   FAILED: $failed" -ForegroundColor $(if($failed-gt 0){"Yellow"}else{"Green"})
Write-Host ""
Write-Host "FAILED TESTS:" -ForegroundColor Red
$R|Where-Object{-not $_.Pass}|ForEach-Object{
    Write-Host "  FAIL [$($_.Id)] $($_.Name)"
    Write-Host "       $($_.Method) $($_.Url)"
    Write-Host "       Got=$($_.Got) Want=$($_.Want)"
    Write-Host "       $($_.Response)"
}
$R|ConvertTo-Json -Depth 5|Out-File "$PSScriptRoot\test_results.json" -Encoding UTF8
$V|ConvertTo-Json|Out-File "$PSScriptRoot\test_vars.json" -Encoding UTF8
Write-Host "`n  Results saved to test_results.json" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan
