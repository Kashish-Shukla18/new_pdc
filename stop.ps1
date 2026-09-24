# Stops the PDC. Only needed to clear an orphan that outlived its terminal —
# a foreground .\start.ps1 exits on Ctrl+C.
#
# Docker data services and the dashboard are managed separately and are left alone.

$lockPort = 21119

$owners = Get-NetTCPConnection -LocalPort $lockPort -State Listen -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique

if (-not $owners) {
    Write-Host 'No PDC running.'
    return
}

foreach ($procId in $owners) {
    $name = (Get-Process -Id $procId -ErrorAction SilentlyContinue).ProcessName
    Write-Host "Stopped PDC (pid $procId $name)."
    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
}
