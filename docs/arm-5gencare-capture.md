# ARM 5GenCare control-flow capture — runbook

Statusdatum: 2026-08-27.

**Doel.** De **5GenCare-controlflow** als plaintext observeren en documenteren:
`v3_login`, `v3_session`, `CMD_DLIST` en de uitgifte/vernieuwing van de
tokens en het stream-`accessToken`. Dit is het **enige** resterende blok voor
een Android-loze tunnel — de Magic WEB2-transportlaag is al gereconstrueerd,
getest en verpakt (`internal/magic`, `internal/bridge`, `cmd/vm65-bridge`).

**Waarom dit niet op x86 kan.** De app-libraries (`libapp.so`,
`libflutter.so`, `libdevconn.so`) zijn armeabi-v7a en lopen op de x86-emulator
via de houdini native-bridge — onzichtbaar voor x86-Frida
(`Process.findModuleByName` = `null`). De enige zichtbare TLS-stack daar is
conscrypt (`libssl.so`), die uitsluitend Google/Firebase/RevenueCat droeg. Zie
`docs/missing-protocol-pieces.md` en `docs/current-state.md`. Op een **native
arm64** omgeving draaien diezelfde libs *native*, waardoor Frida
`libflutter.so` wél ziet en de Dart-runtime-BoringSSL kan hooken.

---

## 0. Veiligheid, scope en redactie (lees dit eerst)

- Alleen op een **eigen** toestel/account en een eigen camera. Er wordt niets
  naar Motorola/5GenCare gestuurd dat de app niet zelf al stuurt.
- **Nooit** plaintext secrets committen. Alle capture-scripts redacteren
  wachtwoorden, tokens, e-mailadressen en opaque strings (≥28 tekens) al aan de
  bron; de mitmproxy-addon doet hetzelfde. Controleer output vóór het delen.
- Ruwe captures (pcap, mitm-flows, frida-logs) horen onder `runtime-logs/` of
  `scratchpad/`, die git-ignored zijn. Alleen **geanonimiseerde** frames en
  afgeleide bevindingen gaan de repo in (zie `internal/magic/testdata/` voor de
  vorm die we aanhouden).

---

## 1. Benodigdheden

### Hardware/omgeving — kies er één

| Optie | Voordeel | Nadeel |
| --- | --- | --- |
| **Fysiek arm64 Android-toestel (gerooted)** | Snelst, geen emulatie | Vereist root; toestel nodig |
| **arm64-v8a AVD, `google_apis`, API 30** | Reproduceerbaar, `adb root` werkt | Trage volledige emulatie op x86-host |

Gebruik **niet** de `playstore`-image (niet te rooten) en **niet** een x86/-64
image (bridge-probleem). `google_apis` bevat de Play **services**
(Firebase/RevenueCat) die de app nodig heeft; de app zelf wordt gesideload uit
de XAPK (`analysis/xapk/`, split `config.armeabi_v7a`).

> API 30 arm64 draait een 32-bit ARM-userspace, dus de armeabi-v7a-libs draaien
> native en een **arm64** `frida-server` instrumenteert het 32-bit app-proces.

### Software

- Android SDK: `sdkmanager`, `emulator`, `platform-tools` (`adb`).
- `frida` + `frida-tools` op de host; `frida-server` (arm64) op het toestel.
  Versies moeten matchen; dit project gebruikte **17.17.0**.
- Voor de fallbacks: `mitmproxy` (`mitmdump`) en `reFlutter`.
- Scripts in `scripts/arm/` (recon, BoringSSL-dump, PairIP-bypass, mitm-addon,
  provisioning).

---

## 2. Fase 0 — omgeving opzetten

`scripts/arm/provision-arm64.ps1` doet stap 2–5 op Windows (met
machine-specifieke paden bovenin — pas `$Sdk`, `$Repo`, `$Fs`, `$Serial` aan).
De generieke equivalenten:

```sh
# 1. Image + AVD (eenmalig)
sdkmanager "system-images;android-30;google_apis;arm64-v8a"
avdmanager create avd -n VM65-ARM64 -k "system-images;android-30;google_apis;arm64-v8a" \
  -d pixel_4

# 2. Boot (arm64 op x86-host = volledige emulatie; boot duurt lang)
emulator -avd VM65-ARM64 -no-snapshot-load -gpu swiftshader_indirect -port 5556 &
adb -s emulator-5556 wait-for-device
# wacht tot sys.boot_completed == 1:
until [ "$(adb -s emulator-5556 shell getprop sys.boot_completed | tr -d '\r')" = 1 ]; do sleep 5; done

# 3. Root + frida-server
adb -s emulator-5556 root
adb -s emulator-5556 push tools/frida-server/frida-server-17.17.0-android-arm64 /data/local/tmp/frida-server
adb -s emulator-5556 shell chmod 755 /data/local/tmp/frida-server
adb -s emulator-5556 shell /data/local/tmp/frida-server -l 0.0.0.0:27042 &

# 4. Sideload de app (base + armeabi-v7a + density + taal)
adb -s emulator-5556 install-multiple -r \
  analysis/xapk/com.fivegencare.com.motorola.nursery.apk \
  analysis/xapk/config.armeabi_v7a.apk \
  analysis/xapk/config.hdpi.apk \
  analysis/xapk/config.en.apk
```

**Verificatiepoort 0.** Voordat je verdergaat:

```sh
adb -s emulator-5556 shell getprop ro.product.cpu.abi      # -> arm64-v8a
adb -s emulator-5556 shell ps -A | grep frida-server        # -> draait
adb -s emulator-5556 shell pm path com.fivegencare.com.motorola.nursery  # -> paden
frida-ps -U | grep -i nursery                               # host ziet het toestel
```

---

## 3. Fase 1 — recon (bewijs de hypothese, kies de methode)

```sh
frida -U -f com.fivegencare.com.motorola.nursery -l scripts/arm/frida_flutter_recon.js
```

Altijd **spawnen** met `-f`, zodat hooks staan vóór de eerste TLS-handshake.
`frida_flutter_recon.js` rapporteert:

- `Process.arch` / `pointerSize` — verwacht **32-bit ARM** voor het app-proces.
- Of `libflutter.so`, `libapp.so`, `libdevconn.so` nu present zijn
  (`Process.findModuleByName` ≠ null).
- Of `SSL_read` / `SSL_write` / `SSL_get_servername` op naam resolven in
  `libflutter.so`.

**Verificatiepoort 1 → beslisboom:**

- arch = 32-bit ARM **en** modules present **en** `SSL_*` resolvable → ga naar
  **Fase 2** (directe BoringSSL-dump). Beste pad: schone plaintext zonder MITM.
- modules present maar `SSL_*` **niet** resolvable (gestript) → **Fase 3a**
  (signature-scan) of **Fase 3b** (reFlutter + mitm).
- modules **niet** present → verkeerde image/bridge; controleer dat je op een
  echte arm64-omgeving zit (herhaal Verificatiepoort 0).

---

## 4. Fase 2 — primaire capture: BoringSSL plaintext-dump

```sh
# Draai tcpdump mee voor ciphertext-correlatie op host/poort-niveau:
adb -s emulator-5556 shell /system/bin/tcpdump -i any -s0 -w /data/local/tmp/5gc.pcap &

frida -U -f com.fivegencare.com.motorola.nursery -l scripts/arm/frida_flutter_boringssl.js
```

Log daarna **handmatig** in de app in met je eigen 5GenCare-account en open één
camera live-view (dat triggert `v3_login` → `v3_session` → `CMD_DLIST` →
Magic-connect).

`frida_flutter_boringssl.js` hookt `SSL_read`/`SSL_write` overal waar ze op naam
resolven en logt per record: richting (`in`/`out`), owning module, socket-`fd`
+ **peer-adres** (om de 5GenCare-socket van Google-verkeer te scheiden), **SNI**
en een geredacteerde printable preview. Elke regel begint met `[BSSL]`.

**Wat je zoekt.** Records met `module: libflutter.so` en een `peer`/`sni` die
naar 5GenCare-infra wijst (`*.moto.5gencare.com`, `*.5gen.care`, `vrelay-*`) —
niet naar `googleapis.com`/`firebase`/`crashlytics`. De `dir:out` records zijn
requests, `dir:in` de responses.

**Verificatiepoort 2.** `ready`-event meldt `flutterHooks > 0` én je ziet
5GenCare-records langskomen → capture geslaagd, ga naar **Fase 5**. Meldt het
`flutterHooks: 0` → symbolen gestript, ga naar **Fase 3**.

Haal de pcap op voor offline correlatie:
`adb -s emulator-5556 pull /data/local/tmp/5gc.pcap runtime-logs/`.

---

## 5. Fase 3 — fallbacks (alleen als Fase 2 `flutterHooks: 0` gaf)

Flutter-release-`libflutter.so` heeft `SSL_*` vaak intern/gestript. Drie routes,
in oplopende complexiteit:

### 3a. BoringSSL signature-scan

Zoek de BoringSSL `SSL_read`/`SSL_write`-prologen (ARM én Thumb) in het
`.text`-segment van `libflutter.so` en hook op adres i.p.v. op naam. Ankers: de
`bssl (...)` versiestring-referenties of bekende `ssl_lib.cc`-callsites. Hergebruik
daarna dezelfde emit/redact-logica als `frida_flutter_boringssl.js`. Voordeel:
blijft binnen Frida, geen certificaat-/pinning-gedoe.

### 3b. reFlutter + mitmproxy + PairIP-bypass (meest betrouwbare framing)

Levert schone request/response-**framing** zonder de exacte `SSL_*`-adressen te
hoeven vinden.

1. **Repack** met reFlutter: patcht Flutters certificaatvalidatie eruit en laat
   de app naar de proxy praten. De hersignde APK faalt echter PairIP's Google
   Play-licentiecheck.
2. **Neutraliseer PairIP** met `scripts/arm/frida_pairip_bypass.js`. PairIP is
   hier pure Java op de ART-runtime (dus óók vanaf x86 hookbaar), die
   `checkLicense()`, de error/paywall-activity en `System.exit(0)` uit de
   `pairip`-package platlegt.
3. **Start** de app onder de bypass en houd hem draaiend:
   ```sh
   python scripts/arm/spawn_capture.py com.fivegencare.com.motorola.nursery <agent.js>
   ```
   (`<agent.js>` = de gecompileerde Frida-agent met `frida-java-bridge`; Frida 17
   verwijderde het ingebouwde `Java`-globaal.)
4. **Capture** met de mitmproxy-addon, die alleen 5GenCare/Magic-verkeer
   geredacteerd logt en de rest ongemoeid laat; volledige flows gaan naar een
   git-ignored bestand:
   ```sh
   mitmdump --mode transparent -p 8083 \
     -s scripts/arm/mitm_5gencare.py \
     -w scratchpad/reflutter/flows.mitm
   ```
   Raw-TLS controlsockets (bv. 5GenCare-control op 3388) verschijnen als
   `tcp_message`; HTTPS als `request`/`response`. Zet de device-HTTP-proxy naar
   de mitm-listener (reFlutter praat naar `10.0.2.2:8083` = host-loopback).

### 3c. Dart-laag (laatste redmiddel)

Hook `SocketImplement` / `SocketDataHandler` / `loginV3Handler` in `libapp.so`
(AOT) via de uit Ghidra bekende offsets — dit is **pre-TLS**, dus plaintext,
maar vergt symbol-/offset-reconstructie in het AOT-blob.

---

## 6. Fase 4 — vast te leggen (de openstaande protocolvragen)

Leg per punt een **geanonimiseerd** request/response-paar vast:

- [ ] **Wireframing** van de controlsocket: delimiter, lengteprefix en/of
      escaping. Waar begint/eindigt een frame?
- [ ] **`v3_login`-serialisatie**: veldvolgorde en de **password-/challenge-
      transformatie** (hash? challenge-response?). Ook het `guest`-loginpad.
- [ ] **`v3_session`**: welke waarden (account/master-ID, twee tokenachtige
      waarden, session-ID) komen terug en hoe worden ze hergebruikt.
- [ ] **Shard-redirect**: de eerste `-6`-respons die doorverwijst naar een
      genummerde `*.moto.5gencare.com`-host — trigger en exacte vorm.
- [ ] **`CMD_DLIST`**: volledige request/response-velden en de **bron + expiry**
      van het stream-`accessToken`.
- [ ] **Heartbeat & sessievernieuwing**: interval, frame, en wanneer tokens
      verlopen/ververst worden.

---

## 7. Fase 5 — terugkoppeling naar de code

De capture sluit de lus met wat er al staat.

**a. Valideer meteen met de bestaande tunnel.** De velden die je nu leest
mappen 1-op-1 op het (git-ignored) `creds.json` dat `cmd/tunnelcheck` en
`cmd/vm65-bridge` al lezen:

| Capture-veld (5GenCare) | `creds.json` sleutel |
| --- | --- |
| numeriek device-ID uit device-discovery | `device_id` |
| camera-SID | `sid` |
| device-token | `device_token` |
| relay-controlhost (`vrelay-*.5gen.care`) | `control_host` |
| target-poort (6667) | `target_port` |

Draai daarmee `go run ./cmd/tunnelcheck -creds runtime-logs/creds/creds.json`:
de productie-relay hoort de afgeleide `magicUuid` te accepteren (dat is al
bewezen). Zodra je óók een **verse, geautoriseerde** sessie hebt, moet de camera
nu wél aanhaken i.p.v. EOF-bij-eerste-data.

**b. Bouw de 5GenCare-controlclient.** Codeer de vastgelegde flow als een nieuw
pakket (bv. `internal/fivegencare`) dat, zoals `internal/magic`, **alleen**
bewezen frames implementeert: `v3_login` (met de exacte password-transformatie),
shard-redirect, `v3_session`, `CMD_DLIST` en de heartbeat. Output: verse `sid`,
`device_token`, `control_host` en stream-`accessToken`.

**c. Draad het in de bridge.** Laat `cmd/vm65-bridge` de controlclient draaien
om de credentials te verkrijgen/vernieuwen i.p.v. ze uit een bestand te lezen,
en geef ze door aan `magic.Dial`. Dan is de tunnel Android-loos end-to-end.

**d. Wire-fixtures.** Voeg per bewezen 5GenCare-frame een geanonimiseerde golden
fixture toe onder `testdata/`, in dezelfde stijl als
`internal/magic/testdata/` + de `-update`-golden-test.

---

## 8. Troubleshooting

| Symptoom | Oorzaak / actie |
| --- | --- |
| `frida-ps -U` leeg | `frida-server` draait niet of versie mismatcht de host-`frida` |
| recon: modules `null`, arch = x86 | Verkeerde image (x86/bridge). Gebruik arm64-v8a `google_apis` |
| `flutterHooks: 0` | `SSL_*` gestript → Fase 3a/3b |
| Alleen Google/Firebase-records | Je kijkt naar conscrypt `libssl.so`; filter op `module: libflutter.so` + 5GenCare-`peer`/`sni` |
| App sluit direct af | PairIP-licentiecheck → `frida_pairip_bypass.js` (Fase 3b) |
| `adb root` geweigerd | `playstore`-image i.p.v. `google_apis`, of fysiek toestel zonder root |
| mitm ziet niets | Flutter negeert de systeemproxy → transparante mode + iptables-redirect, en pinning uit via reFlutter |

---

## Verwacht eindresultaat

Geanonimiseerde 5GenCare-frames die de wireframing, `v3_login`-transformatie,
`CMD_DLIST`-velden, accessToken-bron/expiry en heartbeat aantonen — genoeg om
`internal/fivegencare` te bouwen en de bridge Android-loos end-to-end te maken.
Dat is het laatste ontbrekende stuk; al het downstream-werk (tunnel, bridge,
go2rtc, Home Assistant add-on, containerbuilds) staat en is getest.
