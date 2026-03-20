# Qube Enterprise — Log Watcher
# Opens two PowerShell windows: one for cloud logs, one for qube agent logs.
# Run: .\scripts\watch-logs.ps1

$ProjectRoot = Split-Path -Parent $PSScriptRoot

if (Test-Path "$ProjectRoot\.env.vms") {
    Get-Content "$ProjectRoot\.env.vms" | ForEach-Object {
        $k, $v = $_ -split '='
        Set-Variable -Name $k -Value $v
    }
}

Write-Host "Opening log windows..." -ForegroundColor Cyan

Start-Process powershell -ArgumentList @(
    "-NoExit",
    "-Command",
    "Write-Host '=== CLOUD API LOGS ===' -ForegroundColor Cyan; multipass exec cloud-vm -- sudo journalctl -fu qube-cloud"
)

Start-Process powershell -ArgumentList @(
    "-NoExit",
    "-Command",
    "Write-Host '=== CONF-AGENT LOGS ===' -ForegroundColor Green; multipass exec qube-vm -- sudo journalctl -fu qube-agent"
)

Write-Host "Log windows opened. Close them when done." -ForegroundColor Yellow
