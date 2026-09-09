$lock = Get-NetTCPConnection -LocalPort 21119 -State Listen -ErrorAction SilentlyContinue
if ($lock) {
    $lock | Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
}

$dashboard = Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction SilentlyContinue
if ($dashboard) {
    $dashboard | Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
}

Write-Host 'PDC and dashboard stopped. Docker data services remain running.'
