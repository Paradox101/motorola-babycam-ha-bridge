# Project status

Statusdatum: 2026-08-27

- [done] Statische APK/XAPK-inventarisatie
- [done] Runtime VM65 live-view-observatie
- [done] Camera-targetpoort 6667 bevestigd
- [done] Tijdelijke loopback-RTSP op dynamische poort bevestigd
- [done] Magic P2P `WEB2` relaymodus bevestigd
- [done] Actieve Dart FFI → `libdevconn.so` route geïdentificeerd
- [blocked] 5GenCare controlprotocol reconstrueren: op de x86-emulator niet met x86-Frida te capturen (app-libs zijn ARMv7 achter native-bridge; conscrypt droeg alleen Google/Firebase). Vereist ARM-omgeving; zie `docs/missing-protocol-pieces.md`
- [done] Live Magic-credentials (device id, SID, device-token, control-host, afgeleide magicUuid) uit app-opslag geëxtraheerd via `adb root` voor tunnelvalidatie (lokaal, git-ignored)
- [done] Magic WEB2: wireprotocol (discovery, relay-open, tokencrypto-tunnel) reconstrueren; exacte callback-ABI blijft deels open maar niet-blokkerend
- [done] Magic steady-state flows 9901/2288/3388 en versleutelde/ingekapselde dataplane bevestigd
- [done] Magic connect/handshake-pcap bij app-herstart vastgelegd
- [done] WEB2 relay-open request en formatstring byte-voor-byte bevestigd
- [done] Capturevelden redacterend gecorreleerd met `magicUuid` en `sessionName`
- [done] Geteste Go encoder/parser voor het bewezen WEB2 relay-open frame
- [done] `generate_sid_v1`/`magicUuid` exact gereconstrueerd en tegen runtime gevalideerd
- [done] Device-token als tunnelcryptosleutel en stateful encode/decode exact gereconstrueerd
- [done] Volledige capture offline naar RTSP/SDP/RTP-over-TCP gedecodeerd
- [done] Geteste Go Magic UUID- en tokencrypto-implementaties met onafhankelijke fixtures
- [done] Magic `app ...` controlrequest/-response op poort 8800 runtime vastgelegd; achtveldenvariant bewezen, laatste veld = connection mode (WEB2)
- [done] Magic WEB2-controlketen (discovery/relay-open/tokencrypto) volledig in Go gecodeerd en getest
- [done] Geanonimiseerde Magic-wirefixtures gemaakt (zie golden fixtures hieronder)
- [done] Standalone Go bridge-daemon `cmd/vm65-bridge`: bindt een lokale RTSP-over-TCP-poort en tunnelt elke clientverbinding byte-transparant via een eigen Magic WEB2-relaysessie (`internal/bridge`). Dit is exact de rol die de Android-app met zijn dynamische listenpoort (16667) speelt. Credentials komen uit hetzelfde lokale JSON-bestand als `tunnelcheck`. De 5GenCare-controlflow zit hier bewust niet in: de daemon krijgt de afgeleide inputs aangereikt en verzint/vernieuwt ze niet
- [in progress] Standalone Go Magic-handshake en tunnel: `internal/magic.Dial` voegt discovery, relay-open en tokencrypto samen tot een byte-transparante `net.Conn` (getest tegen een in-memory relay); nog nodig: validatie tegen een echte relay en 5GenCare-inputs
- [done] Tunnelvalidatie tegen de echte relay met geëxtraheerde live-credentials: `cmd/tunnelcheck` bewijst dat productie de afgeleide `magicUuid` + `app`-discovery accepteert. Stream haakt niet aan zonder 5GenCare-autorisatie (relay houdt de sessie open, maar geen camera-peer; EOF pas bij eerste data, ook met actieve camera/5s wachttijd)
- [done] Lokale RTSP-validatie zonder Android: `internal/rtspmock` (mock-camera + client) voert nu een **volledige** RTSP-sessie (OPTIONS→DESCRIBE→SETUP→PLAY met interleaved RTP→TEARDOWN) door de echte Magic WEB2-tunnel; de integratietest (`internal/bridge`) bewijst dat SDP én 12 high-entropy RTP-pakketten byte-exact aankomen. Plus een simpelere OPTIONS-round-trip. Race-detector-schoon, geen Android in de keten
- [done] Reconnect + diagnostiek: de bridge doet dial-retry met exponentiële backoff (`DialRetries`/`DialBackoff`) en logt een expliciete, gelabelde diagnose voor het 'relay open maar geen camera-peer'-geval (nul camerabytes ⇒ ontbrekende 5GenCare-autorisatie). Beide getest
- [done] Geanonimiseerde control- en Magic-wirefixtures: golden fixtures onder `internal/magic/testdata/` (app-request, achtveldenrespons, 139-byte relay-open) met round-trip-golden-test en `-update`-flag; puur synthetische placeholders, geen echte identiteit/secrets
- [done] go2rtc-integratie: `deploy/go2rtc/` levert een go2rtc-config (`rtsp/tcp`-bron naar de bridge) plus een `docker-compose.yml` die bridge+go2rtc samen draait, en een JSON health-endpoint (`internal/bridge.HealthHandler`, `-status`-flag) voor monitoring. Validatie tegen een echte, 5GenCare-geautoriseerde sessie blijft nodig
- [done] Home Assistant add-on: `homeassistant/vm65-bridge/` verpakt bridge+go2rtc (config.yaml, build.yaml, Dockerfile, bashio `run.sh`, DOCS). Watchdog haakt op de go2rtc-API. Credentials komen uit de add-on-opties; end-to-end blijft geblokkeerd op de 5GenCare-controlflow
- [done] amd64/aarch64 containerbuilds: multi-stage `Dockerfile` (statische CGO-loze binary op scratch), `Makefile` (`dist`/`docker`-targets) en GitHub Actions CI (`.github/workflows/ci.yml`: gofmt/vet/`test -race`, cross-compile amd64+arm64, multi-arch buildx). Beide arch-binaries cross-compileren bewezen schoon
- [todo] End-to-end- en reconnectvalidatie (tegen echte camera; geblokkeerd op de 5GenCare-controlflow)
- [in progress] Nieuwe statische route voor 5GenCare: Nursery 2.1.16 levert een
  arm64 Dart 3.9.2-snapshot die Blutter ondersteunt. Binary en snapshotmetadata
  zijn gevalideerd en Blutter heeft de Dart-AOT-code succesvol gedecompileerd.
  Bewezen zijn TLS naar `primary.moto.5gencare.com:3388`, LF-beeindigde
  tekstcommando's, `v3_session <userId> <token> <sessionId>`, `v3_dlist\n` en
  `accessToken = SHA1(deviceToken + "5GenCare.com")`.

`done` betekent dat het betreffende feit daadwerkelijk statisch of tijdens de eigen VM65-sessie is bevestigd. De volledige Magic WEB2-controlketen is nu bewezen, in Go gecodeerd en verpakt in een standalone bridge-daemon (`cmd/vm65-bridge`) met een offline bewezen lokale RTSP-route. Het resterende blok voor een tegen een echte camera werkende tunnel is de 5GenCare-controlflow (TLS via ARM native-bridge, niet zichtbaar via x86-Frida): zonder verse, geautoriseerde SID/device-token/stream-accessToken haakt de camera niet aan. De transportlaag is compleet; de add-on wacht uitsluitend op die controlflow.
