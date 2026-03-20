# Qube Enterprise — Multipass VM Launcher
# Run from your qube-enterprise project root:
#   PowerShell: .\scripts\launch-vms.ps1

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot

Write-Host "==> Launching cloud-vm..." -ForegroundColor Cyan
multipass launch 22.04 --name cloud-vm --cpus 2 --memory 4G --disk 20G
Write-Host "==> Launching qube-vm..." -ForegroundColor Cyan
multipass launch 22.04 --name qube-vm  --cpus 2 --memory 2G --disk 15G

# Get IPs
$CLOUD_IP = (multipass info cloud-vm --format json | ConvertFrom-Json).info.'cloud-vm'.ipv4[0]
$QUBE_IP  = (multipass info qube-vm  --format json | ConvertFrom-Json).info.'qube-vm'.ipv4[0]
Write-Host ""
Write-Host "cloud-vm IP: $CLOUD_IP" -ForegroundColor Green
Write-Host "qube-vm  IP: $QUBE_IP"  -ForegroundColor Green
Write-Host ""

# Save IPs to a local file for reference
"CLOUD_IP=$CLOUD_IP`nQUBE_IP=$QUBE_IP" | Out-File -FilePath "$ProjectRoot\.env.vms" -Encoding ascii
Write-Host "IPs saved to .env.vms" -ForegroundColor Yellow

# Transfer project to cloud-vm
Write-Host "==> Transferring project to cloud-vm..." -ForegroundColor Cyan
multipass transfer --recursive "$ProjectRoot\cloud"      cloud-vm:/home/ubuntu/qube-enterprise/cloud
multipass transfer --recursive "$ProjectRoot\test-ui"   cloud-vm:/home/ubuntu/qube-enterprise/test-ui
multipass transfer "$ProjectRoot\scripts\setup-cloud.sh" cloud-vm:/home/ubuntu/setup-cloud.sh
multipass exec cloud-vm -- chmod +x /home/ubuntu/setup-cloud.sh

# Transfer conf-agent to qube-vm
Write-Host "==> Transferring conf-agent to qube-vm..." -ForegroundColor Cyan
multipass transfer --recursive "$ProjectRoot\conf-agent" qube-vm:/home/ubuntu/qube-enterprise/conf-agent
multipass transfer "$ProjectRoot\scripts\setup-qube.sh"  qube-vm:/home/ubuntu/setup-qube.sh
multipass exec qube-vm -- chmod +x /home/ubuntu/setup-qube.sh

# Provision cloud-vm
Write-Host "==> Provisioning cloud-vm (this takes ~3 min)..." -ForegroundColor Cyan
multipass exec cloud-vm -- bash /home/ubuntu/setup-cloud.sh

# Provision qube-vm — pass cloud IP so conf-agent knows where TP-API is
Write-Host "==> Provisioning qube-vm (this takes ~3 min)..." -ForegroundColor Cyan
multipass exec qube-vm -- bash /home/ubuntu/setup-qube.sh $CLOUD_IP

Write-Host ""
Write-Host "=====================================================" -ForegroundColor Green
Write-Host " DONE. Your environment is ready." -ForegroundColor Green
Write-Host "=====================================================" -ForegroundColor Green
Write-Host ""
Write-Host " Cloud API :  http://$CLOUD_IP`:8080" -ForegroundColor White
Write-Host " TP-API    :  http://$CLOUD_IP`:8081" -ForegroundColor White
Write-Host " Test UI   :  http://$CLOUD_IP`:9000" -ForegroundColor White
Write-Host ""
Write-Host " Shell into cloud-vm:  multipass shell cloud-vm" -ForegroundColor Yellow
Write-Host " Shell into qube-vm:   multipass shell qube-vm"  -ForegroundColor Yellow
Write-Host ""
Write-Host " Next step: register your org + claim a Qube via the Test UI or curl." -ForegroundColor Cyan
