param(
    [ValidateSet('Attach', 'Spawn')]
    [string]$Mode = 'Attach'
)

$ErrorActionPreference = 'Stop'
$package = 'com.fivegencare.com.motorola.nursery'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$logDir = Join-Path $root 'runtime-logs'
$rawLog = Join-Path $logDir 'protocol-capture.log'
$script = Join-Path $PSScriptRoot 'capture.js'
$frida = Join-Path $env:APPDATA 'Python\Python314\Scripts\frida.exe'

New-Item -ItemType Directory -Path $logDir -Force | Out-Null
if (-not (Test-Path -LiteralPath $frida)) { $frida = 'frida' }

if ($Mode -eq 'Spawn') {
    & $frida -U -f $package -l $script -o $rawLog
} else {
    & $frida -U -N $package -l $script -o $rawLog
}

