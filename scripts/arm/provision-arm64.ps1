# Provisions the VM65-ARM64 emulator for native 5GenCare TLS capture:
#   waits for boot, roots the device, pushes+starts the arm64 frida-server,
#   and sideloads the Motorola Nursery app (armeabi-v7a split).
#
# Run AFTER the emulator is booting (scripts/arm start it on port 5556).
# The app login itself is manual — this only gets the app installed and Frida ready.

$ErrorActionPreference = 'Stop'
$Sdk    = Join-Path $env:LOCALAPPDATA 'Android\Sdk'
$Adb    = Join-Path $Sdk 'platform-tools\adb.exe'
$Serial = 'emulator-5556'
$Repo   = 'C:\Users\vvessen\Downloads\Research'
$Xapk   = Join-Path $Repo 'analysis\xapk'
$Fs     = Join-Path $Repo 'tools\frida-server\frida-server-17.17.0-android-arm64'
$Pkg    = 'com.fivegencare.com.motorola.nursery'

Write-Host "[*] Waiting for $Serial to come online..."
& $Adb -s $Serial wait-for-device
do {
  Start-Sleep -Seconds 5
  $booted = (& $Adb -s $Serial shell getprop sys.boot_completed 2>$null).Trim()
  Write-Host "    boot_completed=$booted"
} while ($booted -ne '1')

Write-Host "[*] Device ABI: $((& $Adb -s $Serial shell getprop ro.product.cpu.abi).Trim())"
Write-Host "[*] Rooting adbd..."
& $Adb -s $Serial root | Out-Host
Start-Sleep -Seconds 3
& $Adb -s $Serial wait-for-device

Write-Host "[*] Pushing arm64 frida-server..."
& $Adb -s $Serial push $Fs /data/local/tmp/frida-server | Out-Host
& $Adb -s $Serial shell chmod 755 /data/local/tmp/frida-server

Write-Host "[*] Installing app (base + armeabi-v7a + hdpi + en)..."
$splits = @(
  (Join-Path $Xapk 'com.fivegencare.com.motorola.nursery.apk'),
  (Join-Path $Xapk 'config.armeabi_v7a.apk'),
  (Join-Path $Xapk 'config.hdpi.apk'),
  (Join-Path $Xapk 'config.en.apk')
)
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
Write-Host "     2. Recon:   frida -U -f $Pkg -l scripts\arm\frida_flutter_recon.js"
Write-Host "     3. Capture: frida -U -f $Pkg -l scripts\arm\frida_flutter_boringssl.js"
Write-Host "        (spawn with -f so the TLS hooks are in place before login handshakes)"
