# Project status

Statusdatum: 2026-08-28

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
- [in progress] Geanonimiseerde control- en Magic-wirefixtures maken
- [todo] Standalone Go controlclient
- [done] Standalone Go Magic-handshake en tunnel: `internal/magic.Dial` voegt discovery, relay-open en tokencrypto samen tot een byte-transparante `net.Conn`; gevalideerd tegen zowel een in-memory relay als de echte relay
- [done] Tunnelvalidatie tegen de echte relay met geëxtraheerde live-credentials: `cmd/tunnelcheck` bewijst dat productie de afgeleide `magicUuid` + `app`-discovery accepteert. Stream haakt niet aan zonder 5GenCare-autorisatie (relay houdt de sessie open, maar geen camera-peer; EOF pas bij eerste data, ook met actieve camera/5s wachttijd)
- [done] Lokale TCP→Magic RTSP-bridge: `internal/bridge` biedt de bewezen tunnel als lokale `net.Listen`-poort aan (per verbinding een verse sessie, bidirectioneel byte-transparant), end-to-end getest tegen een in-memory relay; `cmd/magicbridge` is de CLI
- [done] go2rtc-integratie: de HA add-on bundelt go2rtc, dat de lokale bridge-RTSP als stream `motorola` serveert (`addon/motorola-magic-bridge/run.sh`); de exacte go2rtc-release-asset uit de Dockerfile is bereikbaar bevestigd (HTTP 200)
- [done] End-to-end mediapijplijn bewezen: `internal/e2e` streamt een echte RTSP-dialoog (OPTIONS/DESCRIBE/SETUP/PLAY) + 25 interleaved RTP-frames byte-voor-byte door de échte `bridge`-code en de token-crypto Magic-tunnel, met `internal/relaysim` als relay + geautoriseerde-camera-stand-in en `internal/rtspmini` als echte RTSP-camera/-client. Stabiel over 20+ runs. `cmd/streamdemo` draait dezelfde keten live en zichtbaar. Alleen de 5GenCare-camera-attach wordt gesimuleerd
- [done] Flaky-testbug in de relay-test-doubles verholpen: relay-open heeft geen delimiter, dus naïef één-Read-framen faalde bij TCP-coalescing (hang/EOF). Nieuwe `magic.ReadRelayOpenFrame` framet op lengte; regressietest dekt het coalescing-geval; 100× schoon
- [in progress] Home Assistant add-on: `addon/motorola-magic-bridge/` (config.yaml, build.yaml, Dockerfile, run.sh, DOCS.md) is compleet en installeerbaar; de transportlaag is nu end-to-end bewezen te streamen. Enige rest voor een échte live-camera: de 5GenCare-autorisatie (credentials uit een geautoriseerde app-sessie + camera-signalering)
- [in progress] amd64/aarch64/armv7-containerbuilds: `magicbridge` cross-compileert schoon voor alle drie (CGO_ENABLED=0); de Go-buildstage en de go2rtc-download zijn afzonderlijk geverifieerd. Een volledige `docker build` is lokaal niet uitgevoerd omdat er op deze researchmachine geen containerruntime is (Docker/podman ontbreken, WSL zonder distro)
- [todo] Standalone Go 5GenCare-controlclient (geblokkeerd: verse SID/device-token/accessToken vereisen ARM-Frida-capture; zie `docs/missing-protocol-pieces.md`)
- [todo] Live-camera end-to-end met echte hardware (geblokkeerd op 5GenCare-attach) en reconnectvalidatie

`done` betekent dat het betreffende feit daadwerkelijk statisch, tijdens de eigen VM65-sessie, of via een geslaagde build/test in dit project is bevestigd. De volledige Magic WEB2-controlketen is bewezen, in Go gecodeerd, verpakt als lokale RTSP-bridge en Home Assistant add-on (`addon/motorola-magic-bridge/`, go2rtc erin gebundeld), en de mediapijplijn is nu end-to-end aantoonbaar (`internal/e2e`, `cmd/streamdemo`). Het enige resterende blok voor een échte live-camera is de 5GenCare-controlflow (TLS via ARM native-bridge, niet zichtbaar via x86-Frida): die levert de verse, geautoriseerde sessie en signaleert de camera om aan de relay te koppelen. Zonder dat stuk draait de add-on en stroomt gesimuleerde media correct; een echte camera koppelt pas met 5GenCare-autorisatie.
