# Provisions the VM65-ARM64 emulator for native 5GenCare TLS capture:
#   waits for boot, roots the device, pushes+starts the arm64 frida-server,
#   and sideloads the Motorola Nursery app (armeabi-v7a split).
#
# Run AFTER the emulator is booting (scripts/arm start it on port 5556).
# The app login itself is manual — this only gets the app installed and Frida ready.

[CmdletBinding()]
param(
  [string]$Serial = '',
  [string]$Sdk = (Join-Path $env:LOCALAPPDATA 'Android\Sdk'),
  [string]$FridaServer = '',
  [string]$XapkDirectory = '',
  [string]$Package = 'com.fivegencare.com.motorola.nursery'
)

$ErrorActionPreference = 'Stop'
$Repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$Adb = Join-Path $Sdk 'platform-tools\adb.exe'
if (-not $XapkDirectory) { $XapkDirectory = Join-Path $Repo 'analysis\xapk' }
if (-not $FridaServer) {
  $FridaServer = Join-Path $Repo 'tools\frida-server\frida-server-17.17.0-android-arm64'
}
if (-not (Test-Path -LiteralPath $Adb -PathType Leaf)) {
  throw "adb not found at '$Adb'; pass -Sdk with the Android SDK directory"
}
if (-not $Serial) {
  $devices = @(& $Adb devices | Select-Object -Skip 1 | ForEach-Object {
    if ($_ -match '^(\S+)\s+device$') { $Matches[1] }
  })
  if ($devices.Count -ne 1) {
    throw "Expected exactly one connected Android device, found $($devices.Count). Pass -Serial explicitly."
  }
  $Serial = $devices[0]
}

Write-Host "[*] Waiting for $Serial to come online..."
& $Adb -s $Serial wait-for-device
do {
  Start-Sleep -Seconds 5
  $booted = (& $Adb -s $Serial shell getprop sys.boot_completed 2>$null).Trim()
  Write-Host "    boot_completed=$booted"
} while ($booted -ne '1')

$abi = (& $Adb -s $Serial shell getprop ro.product.cpu.abi).Trim()
Write-Host "[*] Device ABI: $abi"
if ($abi -notmatch '^arm') {
  throw "Device '$Serial' uses ABI '$abi'. Use a native ARM device; the app's ARM libraries are invisible to Frida on x86."
}
Write-Host "[*] Rooting adbd..."
& $Adb -s $Serial root | Out-Host
Start-Sleep -Seconds 3
& $Adb -s $Serial wait-for-device

Write-Host "[*] Pushing arm64 frida-server..."
if (-not (Test-Path -LiteralPath $FridaServer -PathType Leaf)) {
  throw "frida-server not found at '$FridaServer'; pass -FridaServer with the matching Android ARM binary"
}
& $Adb -s $Serial push $FridaServer /data/local/tmp/frida-server | Out-Host
& $Adb -s $Serial shell chmod 755 /data/local/tmp/frida-server

Write-Host "[*] Installing app (base + armeabi-v7a + hdpi + en)..."
$splits = @(
  (Join-Path $XapkDirectory 'com.fivegencare.com.motorola.nursery.apk'),
  (Join-Path $XapkDirectory 'config.armeabi_v7a.apk'),
  (Join-Path $XapkDirectory 'config.hdpi.apk'),
  (Join-Path $XapkDirectory 'config.en.apk')
)
foreach ($split in $splits) {
  if (-not (Test-Path -LiteralPath $split -PathType Leaf)) {
    throw "Required APK split not found: '$split'; pass -XapkDirectory"
  }
}
& $Adb -s $Serial install-multiple -r @splits | Out-Host

Write-Host "[*] Starting frida-server in the background..."
Start-Process -FilePath $Adb -ArgumentList @('-s', $Serial, 'shell',
  '/data/local/tmp/frida-server', '-l', '0.0.0.0:27042') -WindowStyle Hidden

Start-Sleep -Seconds 3
Write-Host "[*] frida-server processes:"
& $Adb -s $Serial shell ps -A 2>$null | Select-String 'frida-server' | Out-Host

Write-Host ""
Write-Host "[OK] Provisioned. Next:"
Write-Host "     1. Launch the app on the emulator and log in with your 5GenCare account."
Write-Host "     2. Recon:   frida -U -f $Package -l scripts\arm\frida_flutter_recon.js"
Write-Host "     3. Capture: frida -U -f $Package -l scripts\arm\frida_flutter_boringssl.js"
Write-Host "        (spawn with -f so the TLS hooks are in place before login handshakes)"
