# Qube Enterprise — Hot Redeploy
# Pushes code changes to VMs and rebuilds without full reprovision.
# Run from your project root: .\scripts\redeploy.ps1

param(
    [string]$Target = "both"   # "cloud", "qube", or "both"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot

# Load VM IPs
if (!(Test-Path "$ProjectRoot\.env.vms")) {
    Write-Error "Run launch-vms.ps1 first to create .env.vms"
    exit 1
}
Get-Content "$ProjectRoot\.env.vms" | ForEach-Object {
    $k, $v = $_ -split '='
    Set-Variable -Name $k -Value $v
}

if ($Target -eq "cloud" -or $Target -eq "both") {
    Write-Host "==> Redeploying cloud..." -ForegroundColor Cyan

    # Transfer updated source
    multipass transfer --recursive "$ProjectRoot\cloud"   cloud-vm:/home/ubuntu/qube-enterprise/cloud
    multipass transfer --recursive "$ProjectRoot\test-ui" cloud-vm:/home/ubuntu/qube-enterprise/test-ui

    # Rebuild and restart
    multipass exec cloud-vm -- bash -c '
        export PATH=$PATH:/usr/local/go/bin
        cd /home/ubuntu/qube-enterprise/cloud
        go build -o /usr/local/bin/qube-cloud ./cmd/server
        sudo systemctl restart qube-cloud
        sudo systemctl restart qube-ui
        echo "cloud redeployed OK"
    '
    Write-Host "  Cloud API: http://$CLOUD_IP`:8080" -ForegroundColor Green
    Write-Host "  Test UI:   http://$CLOUD_IP`:9000" -ForegroundColor Green
}

if ($Target -eq "qube" -or $Target -eq "both") {
    Write-Host "==> Redeploying conf-agent..." -ForegroundColor Cyan

    multipass transfer --recursive "$ProjectRoot\conf-agent" qube-vm:/home/ubuntu/qube-enterprise/conf-agent

    multipass exec qube-vm -- bash -c '
        export PATH=$PATH:/usr/local/go/bin
        cd /home/ubuntu/qube-enterprise/conf-agent
        go mod tidy
        go build -o /usr/local/bin/qube-conf-agent .
        sudo systemctl restart qube-agent
        echo "conf-agent redeployed OK"
    '
    Write-Host "  conf-agent restarted" -ForegroundColor Green
}

Write-Host ""
Write-Host "==> Redeploy complete." -ForegroundColor Green
