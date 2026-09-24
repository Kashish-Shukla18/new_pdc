# Starts the PDC and nothing else. Docker data services and the React dashboard
# are started separately.
#
# Runs in the foreground on purpose: the PDC holds an exclusive instance lock and
# long-lived PMU sockets, so Ctrl+C must reach it. We use `go run` (not a
# checked-in .\pdc.exe) because Windows Application Control blocks unsigned
# binaries in this folder / %TEMP%\pdc-run on this machine.
#
# Any arguments are passed through, e.g.  .\start.ps1 -metrics-addr=:2112

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

# The lock port from internal/instance.Acquire. A previous PDC still holding it
# would make this run fail with "another PDC instance is already running".
$lockPort = 21119

$existing = Get-NetTCPConnection -LocalPort $lockPort -State Listen -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique

if ($existing) {
    foreach ($procId in $existing) {
        $name = (Get-Process -Id $procId -ErrorAction SilentlyContinue).ProcessName
        Write-Host "Stopping existing PDC (pid $procId $name)..."
        Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
    }
    # Give the OS a moment to release the lock and the PMU sockets.
    Start-Sleep -Milliseconds 500
}

Write-Host 'Starting PDC via go run (Ctrl+C to stop)'
Write-Host '  API/metrics: http://127.0.0.1:2112'
Write-Host '  PMU config:  http://127.0.0.1:8081'
Write-Host ''
# go run builds under the Go cache and waits on the child — same foreground
# behavior as a direct exe for Ctrl+C. stop.ps1 still clears lock port 21119.
go run . @args
exit $LASTEXITCODE
