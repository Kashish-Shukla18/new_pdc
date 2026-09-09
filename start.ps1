$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Set-Location $root

Write-Host 'Starting Redis and TimescaleDB...'
docker compose up -d redis timescaledb

$lock = Get-NetTCPConnection -LocalPort 21119 -State Listen -ErrorAction SilentlyContinue
if ($lock) {
    Write-Host 'Stopping the existing PDC...'
    $lock | Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 500
}

Write-Host 'Building PDC...'
go build -o pdc.exe .

Write-Host 'Starting PDC...'
Start-Process powershell.exe -WorkingDirectory $root -ArgumentList '-NoExit', '-Command', '.\pdc.exe'

if (-not (Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction SilentlyContinue)) {
    Write-Host 'Starting dashboard...'
    $dashboard = Join-Path $root 'dashboard'
    if (-not (Test-Path (Join-Path $dashboard 'node_modules'))) {
        npm --prefix $dashboard install
    }
    Start-Process powershell.exe -WorkingDirectory $dashboard -ArgumentList '-NoExit', '-Command', 'npm run dev'
} else {
    Write-Host 'Dashboard is already running.'
}

Write-Host ''
Write-Host 'PDC API:   http://127.0.0.1:2112'
Write-Host 'Dashboard: http://localhost:5173'
