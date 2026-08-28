# ARM 5GenCare control-flow capture — runbook

Statusdatum: 2026-08-27. **Update 2026-08-28: de arm64-AVD-route werkt niet op
deze x86_64-Windows-host.** De geïnstalleerde emulator (36.4.9/36.4.10) weigert
een arm64-guest botweg:

> `FATAL | Avd's CPU Architecture 'arm64' is not supported by the QEMU2 emulator on x86_64 host.`

Google heeft ARM-op-x86-emulatie rond emulator v29 geschrapt; arm64-images
draaien alleen op arm64-hosts (bijv. Apple Silicon). De hieronder beschreven
`VM65-ARM64`-opzet blijft geldig als runbook voor een arm64-host, maar is op
deze machine niet uitvoerbaar.

**Werkende host-onafhankelijke route op deze machine:** reFlutter (0.8.6) +
mitmproxy (12.2.3), beide al geïnstalleerd. reFlutter patcht `libflutter.so`
statisch (certificaatvalidatie uit + verkeer naar een proxy), dus het werkt op
de snelle x86_64-`VM65-Frida`-AVD ondanks de houdini-native-bridge. Zie
`docs/reflutter-mitm-capture.md`.

Doel: de **5GenCare-controlflow** (login v3, session v3, `CMD_DLIST`, token-/
accessToken-uitgifte) als plaintext observeren — het enige resterende blok voor
een Android-loze tunnel. Op de x86-emulator kan dit niet: de app-libs zijn
ARMv7 en lopen via de houdini native-bridge, onzichtbaar voor x86-Frida
(zie `5gencare-tls-arm-bridge` en `docs/missing-protocol-pieces.md`). Op een
**native arm64-v8a AVD** draaien diezelfde armeabi-v7a-libs *native*, waardoor
Frida `libflutter.so`/`libapp.so`/`libdevconn.so` wél ziet en de Dart-runtime-
BoringSSL kan hooken.

## Waarom arm64-v8a met google_apis (niet playstore)

- **google_apis** is te rooten met `adb root` — vereist om `frida-server` te
  draaien. De **playstore**-image is niet te rooten.
- Google Play **services** (Firebase/RevenueCat) zitten in de google_apis-image;
  de Play **Store**-app niet, dus de app wordt gesideload uit de XAPK
  (`analysis/xapk/`, split `config.armeabi_v7a`).
- API 30 arm64 draait 32-bit ARM-userspace, dus de armeabi-v7a-libs draaien
  native; arm64-`frida-server` instrumenteert het 32-bit proces.

## Setup (gedaan / scripts aanwezig)

1. Image: `sdkmanager "system-images;android-30;google_apis;arm64-v8a"` — geïnstalleerd.
2. AVD: `VM65-ARM64` (pixel_4, 2G RAM, 10G data) — aangemaakt.
3. arm64 frida-server 17.17.0 uitgepakt in `tools/frida-server/`.
4. Boot: `emulator -avd VM65-ARM64 -no-snapshot-load -gpu swiftshader_indirect -port 5556`
   (arm64 op x86-host = volledige emulatie, boot duurt lang).
5. Provision: `scripts/arm/provision-arm64.ps1` — wacht op boot, `adb root`,
   pusht+start frida-server, sideloadt de app.

## Capture

Handmatig: start de app en log in met het eigen 5GenCare-account, dan:

```
frida -U -f com.fivegencare.com.motorola.nursery -l scripts/arm/frida_flutter_recon.js
frida -U -f com.fivegencare.com.motorola.nursery -l scripts/arm/frida_flutter_boringssl.js
```

Spawn met `-f` zodat de TLS-hooks staan vóór de login-handshake. Draai
gelijktijdig `tcpdump` (`/system/bin/tcpdump`, adb root) voor ciphertext-
correlatie op poort/host-niveau.

### Stap 1 — recon (`frida_flutter_recon.js`)

Bewijst de hypothese en kiest de capture-methode. Rapporteert:
- `Process.arch`/`pointerSize` (verwacht 32-bit ARM voor het app-proces),
- of `libflutter.so`/`libapp.so`/`libdevconn.so` nu present zijn,
- of `SSL_read`/`SSL_write`/`SSL_get_servername` resolvable zijn in `libflutter.so`.

### Stap 2 — dump (`frida_flutter_boringssl.js`)

Hookt `SSL_read`/`SSL_write` overal waar ze op naam resolven, met richting,
module, socket-fd + peer-adres (om de 5GenCare-socket van Google-verkeer te
scheiden), SNI en een geredacteerde preview. Secrets worden nooit plain gelogd.

## Fallback als BoringSSL-symbolen gestript zijn

Flutter-release-`libflutter.so` heeft `SSL_*` vaak intern/gestript. Als
`frida_flutter_boringssl.js` `flutterHooks=0` meldt:

1. **Signature-scan**: zoek de BoringSSL `SSL_read`/`SSL_write`-prologen (ARM/
   Thumb) in het `libflutter.so`-tekstsegment en hook op adres. Anker: het
   `bssl` versiestring-referentiepatroon of bekende `ssl_lib.cc`-callsites.
2. **Proxy-MITM met Flutter-pinning-bypass**: forceer de app door een
   mitmproxy (transparant via iptables-redirect, want Flutter negeert de
   systeemproxy) en schakel Flutters certificaatvalidatie uit met een
   Frida-pinning-bypass (of `reFlutter`-repack). mitmproxy toont dan de
   5GenCare-request/response met framing. Voordeel: clean framing zonder de
   exacte `SSL_*`-adressen te hoeven vinden.
3. **Dart-laag**: hook `SocketImplement`/`SocketDataHandler`/`loginV3Handler`
   in `libapp.so` (AOT) via de uit Ghidra bekende offsets — pre-TLS, dus
   plaintext, maar vergt symbol-/offset-reconstructie.

## Verwacht resultaat

Geanonimiseerde request/response-frames die aantonen:
- wireframing (delimiter/lengteprefix/escaping) van de controlsocket,
- de `v3_login`-serialisatie en password-/challenge-transformatie,
- de exacte `CMD_DLIST`-velden en de bron/expiry van het stream-accessToken,
- heartbeat en sessievernieuwing.

Daarmee kan `internal/magic` worden uitgebreid met een 5GenCare-controlclient
die een verse, geautoriseerde sessie opzet — het laatste stuk richting een
Android-loze tunnel.
