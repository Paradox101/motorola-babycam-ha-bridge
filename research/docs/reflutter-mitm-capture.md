# reFlutter + mitmproxy capture route (x86_64 emulator)

Statusdatum: 2026-08-28. Host-onafhankelijk alternatief voor de ARM-Frida-route
(die op deze x86_64-Windows-host niet kan; zie `docs/arm-5gencare-capture.md`).

Doel blijft: de **5GenCare-controlflow** (v3_login, session, `CMD_DLIST`,
accessToken-uitgifte) als plaintext observeren.

## Idee

reFlutter patcht `libflutter.so` statisch: certificaatvalidatie uit + verkeer
naar een proxy. Omdat dit de app-binary wijzigt (geen runtime-hook), werkt het
óók op de snelle x86_64-`VM65-Frida`-AVD, waar de armeabi-v7a-libs via houdini
lopen. mitmproxy decodeert dan het TLS-verkeer.

Tooling staat klaar: `reflutter` 0.8.6 + `mitmproxy` 12.2.3 (pip), `APKEditor.jar`
(merge splits → universal APK), Android SDK + `VM65-Frida` (x86_64, API 30).

## Wat is bewezen te werken (2026-08-28)

- De gemergede + reFlutter-gepatchte + hergesignde APK (`release.RE.apk`,
  uit een eerdere sessie in scratchpad) **installeert en draait** op `VM65-Frida`;
  de Flutter-runtime bereikt `app_opened` en registreert `magicP2PRegisterCallback`.
- De app is **PairIP**-beschermd (`com.pairip.application.Application`).

## Blokkade: PairIP-restartlus

De resigned APK faalt de Play-licentiecheck:

```
E LicenseClient: com.pairip.licensecheck.LicenseCheckException: Licensing service could not process request.
    at com.pairip.licensecheck.LicenseClient.checkLicenseInternal(LicenseClient.java:372)
    at com.pairip.licensecheck.LicenseClient.lambda$onServiceConnected$0
```

Gevolg: PairIP toont `com.pairip.licensecheck.LicenseActivity` ("Something went
wrong / Check that Google Play is enabled") **en** roept `scheduleAppShutdown`
aan → het proces wordt gekilld en herstart → dialoog → lus. Alleen "Close"
tikken volstaat niet (herstart komt terug).

## Frida-bevindingen

- **Frida 17 werkt niet voor Java-hooks hier:** de ingebouwde `Java`-bridge is
  in v17 verwijderd; de meegebundelde `frida-java-bridge@7.0.13` initialiseert
  niet (`TypeError: not a function` in `__require`). Exact de muur waar de vorige
  sessie op strandde (`fridaproj/agent.js`, `spawn.log`).
- **Frida 16 werkt wél:** een venv met `frida==16.7.19` + `frida-server-16.7.19-
  android-x86_64` geeft een native `Java.perform`. Een minimale bypass die
  `LicenseClient.checkLicense`/`checkLicenseInternal`/`handleError`/
  `scheduleAppShutdown` no-opt, stopt de lus (proces blijft stabiel).

## Waarom het (nog) niet tot een capture leidde

1. **Bypass is niet persistent.** De Frida-hooks gelden alleen voor het
   Frida-**gespawnde** proces. Een normale icoon-launch start een vers proces
   zónder hooks → de lus komt terug. Nodig: persistente neutralisatie —
   een **statische smali-patch** van `com.pairip.licensecheck.*` (de license-
   klassen staan buiten de VMRunner-bytecode, dus vermoedelijk veilig te patchen),
   of een Frida-gadget in de APK.
2. **reFlutter-proxy-IP fout ingebakken.** De bestaande patch gebruikte
   `127.0.0.1` — binnen de emulator wijst dat naar de emulator zelf, niet naar
   mitmproxy op de host. Moet `10.0.2.2` (host-loopback vanuit de emulator) zijn,
   met mitmproxy in de bijpassende mode. Dit verklaart de lege `flows.mitm`
   (alleen 127.0.0.1-ruis, geen 5GenCare-flows).
3. **Emulator-connectiviteit onzeker.** DNS resolvet (`google.com` → echt IP),
   maar Android's captive-portal-validatie slaagt nooit (`everValidated{false}`,
   wifi-"×") — vermoedelijk bedrijfsfirewall/Defender rond de emulator-NAT.
   TCP/443 naar de 5GenCare-hosts is nog niet bevestigd.

## Aanbevolen vervolg

- **Betrouwbaarst: fysiek ARM-Android-toestel** op eigen wifi met `frida-server`.
  Native Frida hookt de Flutter-BoringSSL direct (`scripts/arm/frida_flutter_
  boringssl.js`) op de **originele, gesignde** APK → geen reFlutter-resign, dus
  geen PairIP-lus, geen proxy-routing-hack, en echt internet.
- **Emulator afmaken (indien gewenst):** (a) statische smali-patch van de
  PairIP-license-klassen voor een permanent werkende app; (b) reFlutter opnieuw
  met `10.0.2.2`; (c) mitmproxy in transparante/reverse mode + `adb reverse`;
  (d) eerst TCP/443-bereikbaarheid naar 5GenCare vanuit de emulator bevestigen.

De live login met een geautoriseerd 5GenCare-account + een online camera blijft
in alle gevallen nodig; die kan de research niet vervangen.
