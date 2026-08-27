[CmdletBinding()]
param(
    [ValidateSet('Spawn', 'Attach')]
    [string]$Mode = 'Spawn'
)

$ErrorActionPreference = 'Stop'
$workspaceRoot = Split-Path -Parent $PSScriptRoot
$packageName = 'com.fivegencare.com.motorola.nursery'
$adb = Join-Path $env:LOCALAPPDATA 'Android\Sdk\platform-tools\adb.exe'
$fridaUserScripts = Join-Path $env:APPDATA 'Python\Python314\Scripts'
$frida = Join-Path $fridaUserScripts 'frida.exe'
$fridaPs = Join-Path $fridaUserScripts 'frida-ps.exe'
$hookScript = Join-Path $PSScriptRoot 'frida_vm65.js'
$logDirectory = Join-Path $workspaceRoot 'runtime-logs'
$logFile = Join-Path $logDirectory 'vm65-session.log'

foreach ($requiredFile in @($adb, $frida, $fridaPs, $hookScript)) {
    if (-not (Test-Path -LiteralPath $requiredFile)) {
        throw "Benodigd bestand ontbreekt: $requiredFile"
    }
}

$deviceLines = & $adb devices | Select-Object -Skip 1 | Where-Object { $_ -match "\tdevice$" }
if (@($deviceLines).Count -ne 1) {
    throw "Verwacht exact één online adb-device, gevonden: $(@($deviceLines).Count)"
}

& $fridaPs -Uai *> $null
if ($LASTEXITCODE -ne 0) {
    throw 'frida-server is niet bereikbaar via USB/adb. Start frida-server eerst.'
}

New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null
if (Test-Path -LiteralPath $logFile) {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    Copy-Item -LiteralPath $logFile -Destination "$logFile.$stamp.bak"
}

Write-Host "Mode: $Mode"
Write-Host "Package: $packageName"
Write-Host "Log: $logFile"
Write-Host 'Stop de capture met Ctrl+C.'

if ($Mode -eq 'Attach') {
    $pidText = (& $adb shell pidof $packageName).Trim()
    if (-not $pidText) {
        & $adb shell monkey -p $packageName -c android.intent.category.LAUNCHER 1 *> $null
        Start-Sleep -Seconds 3
    }
    & $frida -U -N $packageName -l $hookScript -o $logFile
} else {
    & $frida -U -f $packageName -l $hookScript -o $logFile
}

