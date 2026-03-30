# Qube Enterprise v2 — Multipass VM Launcher
#
# Modes:
#   .\scripts\launch-vms.ps1                          # both cloud-vm + qube-vm (default)
#   .\scripts\launch-vms.ps1 -Mode qube-only -CloudIP 20.x.x.x   # qube-vm only (cloud is Azure or external)
#
# qube-only: creates only the local Multipass qube-vm and points it at an
# existing cloud (Azure VM, another Multipass VM, or any remote IP).

param(
    [string]$Mode     = "both",   # "both" | "qube-only"
    [string]$CloudIP  = ""        # required when Mode = "qube-only"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot

# ── Validate args ─────────────────────────────────────────────────────────────
if ($Mode -eq "qube-only" -and $CloudIP -eq "") {
    Write-Error "Mode 'qube-only' requires -CloudIP <ip>  (your Azure / external cloud IP)"
    exit 1
}
if ($Mode -ne "both" -and $Mode -ne "qube-only") {
    Write-Error "Mode must be 'both' or 'qube-only'"
    exit 1
}

# ── Cloud VM setup (skipped in qube-only mode) ────────────────────────────────
if ($Mode -eq "both") {
    Write-Host "==> Launching cloud-vm..." -ForegroundColor Cyan
    multipass launch 22.04 --name cloud-vm --cpus 2 --memory 4G --disk 20G 2>$null
    if ($LASTEXITCODE -ne 0) { Write-Host "  cloud-vm already exists, continuing" -ForegroundColor Yellow }

    $CLOUD_IP = (multipass info cloud-vm --format json | ConvertFrom-Json).info.'cloud-vm'.ipv4[0]
    Write-Host "  cloud-vm IP: $CLOUD_IP" -ForegroundColor Green

    Write-Host "==> Transferring project to cloud-vm..." -ForegroundColor Cyan
    multipass exec cloud-vm -- mkdir -p /opt/qube-enterprise/migrations /opt/qube-enterprise/migrations-telemetry
    multipass transfer --recursive "$ProjectRoot\cloud\migrations"           cloud-vm:/opt/qube-enterprise/migrations
    multipass transfer --recursive "$ProjectRoot\cloud\migrations-telemetry" cloud-vm:/opt/qube-enterprise/migrations-telemetry
    multipass transfer --recursive "$ProjectRoot\cloud"                      cloud-vm:/home/ubuntu/qube-enterprise/cloud
    multipass transfer --recursive "$ProjectRoot\test-ui"                    cloud-vm:/home/ubuntu/qube-enterprise/test-ui
    multipass transfer "$ProjectRoot\scripts\setup-cloud.sh"                 cloud-vm:/home/ubuntu/setup-cloud.sh
    multipass exec cloud-vm -- chmod +x /home/ubuntu/setup-cloud.sh

    Write-Host "==> Provisioning cloud-vm (~3 min)..." -ForegroundColor Cyan
    multipass exec cloud-vm -- sudo bash /home/ubuntu/setup-cloud.sh
    Write-Host "  cloud-vm ready" -ForegroundColor Green
} else {
    # qube-only: use provided external IP
    $CLOUD_IP = $CloudIP
    Write-Host "  Using external cloud at $CLOUD_IP" -ForegroundColor Yellow
}

# ── Qube VM setup (always runs) ───────────────────────────────────────────────
Write-Host "==> Launching qube-vm..." -ForegroundColor Cyan
multipass launch 22.04 --name qube-vm --cpus 1 --memory 2G --disk 15G 2>$null
if ($LASTEXITCODE -ne 0) { Write-Host "  qube-vm already exists, continuing" -ForegroundColor Yellow }

$QUBE_IP = (multipass info qube-vm --format json | ConvertFrom-Json).info.'qube-vm'.ipv4[0]
Write-Host "  qube-vm IP: $QUBE_IP" -ForegroundColor Green

Write-Host "==> Transferring conf-agent to qube-vm..." -ForegroundColor Cyan
multipass transfer --recursive "$ProjectRoot\conf-agent"   qube-vm:/home/ubuntu/qube-enterprise/conf-agent
multipass transfer "$ProjectRoot\test\mit.txt"              qube-vm:/tmp/mit.txt
multipass exec qube-vm -- sudo cp /tmp/mit.txt /boot/mit.txt
multipass transfer "$ProjectRoot\scripts\setup-qube.sh"    qube-vm:/home/ubuntu/setup-qube.sh
multipass exec qube-vm -- chmod +x /home/ubuntu/setup-qube.sh

Write-Host "==> Provisioning qube-vm (~3 min)..." -ForegroundColor Cyan
multipass exec qube-vm -- sudo bash /home/ubuntu/setup-qube.sh --cloud-ip $CLOUD_IP
Write-Host "  qube-vm ready" -ForegroundColor Green

# ── Save IPs ──────────────────────────────────────────────────────────────────
"CLOUD_IP=$CLOUD_IP`nQUBE_IP=$QUBE_IP" | Out-File -FilePath "$ProjectRoot\.env.vms" -Encoding ascii
Write-Host "IPs saved to .env.vms" -ForegroundColor Yellow

# ── Summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "=====================================================" -ForegroundColor Green
Write-Host " DONE. Your environment is ready." -ForegroundColor Green
Write-Host "=====================================================" -ForegroundColor Green
Write-Host ""
Write-Host " Cloud API :  http://$CLOUD_IP`:8080" -ForegroundColor White
Write-Host " TP-API    :  http://$CLOUD_IP`:8081" -ForegroundColor White
Write-Host " InfluxDB  :  http://$CLOUD_IP`:8086" -ForegroundColor White
Write-Host " Test UI   :  http://$CLOUD_IP`:8888" -ForegroundColor White
Write-Host ""
if ($Mode -eq "both") {
    Write-Host " Shell into cloud-vm:  multipass shell cloud-vm" -ForegroundColor Yellow
}
Write-Host " Shell into qube-vm:   multipass shell qube-vm"  -ForegroundColor Yellow
Write-Host ""
Write-Host " Run tests:  .\test\test_api.sh http://$CLOUD_IP`:8080" -ForegroundColor Cyan
Write-Host " Redeploy:   .\scripts\redeploy.ps1" -ForegroundColor Cyan
Write-Host ""
Write-Host " Next step: register your org + claim a Qube via the Test UI or curl." -ForegroundColor Cyan
