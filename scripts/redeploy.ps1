# Qube Enterprise v2 — Hot Redeploy
# Pushes code changes to VMs and rebuilds without full reprovision.
# Run from your project root: .\scripts\redeploy.ps1
#
# Cloud API runs in Docker — redeploy rebuilds the image and restarts the container.
# conf-agent runs as a systemd binary — redeploy rebuilds and restarts the service.

param(
    [string]$Target = "both"   # "cloud", "qube", or "both"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot

# Load VM IPs from .env.vms (created by launch-vms.ps1)
if (!(Test-Path "$ProjectRoot\.env.vms")) {
    Write-Error "Run launch-vms.ps1 first to create .env.vms"
    exit 1
}
Get-Content "$ProjectRoot\.env.vms" | ForEach-Object {
    if ($_ -match "^(\w+)=(.+)$") {
        Set-Variable -Name $matches[1] -Value $matches[2]
    }
}

if ($Target -eq "cloud" -or $Target -eq "both") {
    Write-Host "==> Redeploying cloud-api..." -ForegroundColor Cyan

    # Transfer updated source
    multipass transfer --recursive "$ProjectRoot\cloud"   cloud-vm:/home/ubuntu/qube-enterprise/cloud
    multipass transfer --recursive "$ProjectRoot\test-ui" cloud-vm:/home/ubuntu/qube-enterprise/test-ui

    # Rebuild Docker image and restart container
    multipass exec cloud-vm -- bash -c '
        set -e
        cd /home/ubuntu/qube-enterprise/cloud
        sudo docker build -t qube-cloud-api:local .
        # Tag as the image name used in compose
        sudo docker tag qube-cloud-api:local qube-cloud-api:local
        cd /opt/qube-enterprise
        sudo docker compose up -d --no-deps cloud-api
        echo "Waiting for Cloud API..."
        for i in $(seq 1 20); do
          curl -sf http://localhost:8080/health | grep -q "\"ok\"" && echo "cloud-api redeployed OK" && exit 0
          sleep 3
        done
        echo "WARNING: health check timed out"
    '
    Write-Host "  Cloud API: http://$CLOUD_IP`:8080" -ForegroundColor Green
    Write-Host "  TP-API:    http://$CLOUD_IP`:8081" -ForegroundColor Green
}

if ($Target -eq "qube" -or $Target -eq "both") {
    Write-Host "==> Redeploying enterprise-conf-agent..." -ForegroundColor Cyan

    multipass transfer --recursive "$ProjectRoot\conf-agent" qube-vm:/home/ubuntu/qube-enterprise/conf-agent

    multipass exec qube-vm -- bash -c '
        set -e
        export PATH=$PATH:/usr/local/go/bin
        cd /home/ubuntu/qube-enterprise/conf-agent
        go mod tidy
        CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/enterprise-conf-agent .
        sudo systemctl restart enterprise-conf-agent.service
        sleep 2
        sudo systemctl is-active enterprise-conf-agent.service && echo "enterprise-conf-agent redeployed OK" || echo "WARNING: service not active"
    '
    Write-Host "  conf-agent restarted on qube-vm" -ForegroundColor Green
    Write-Host "  Logs: multipass exec qube-vm -- journalctl -u enterprise-conf-agent -f" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "==> Redeploy complete." -ForegroundColor Green
