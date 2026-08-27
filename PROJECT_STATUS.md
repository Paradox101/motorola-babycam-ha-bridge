# Project status

Statusdatum: 2026-08-27

- [done] Statische APK/XAPK-inventarisatie
- [done] Runtime VM65 live-view-observatie
- [done] Camera-targetpoort 6667 bevestigd
- [done] Tijdelijke loopback-RTSP op dynamische poort bevestigd
- [done] Magic P2P `WEB2` relaymodus bevestigd
- [done] Actieve Dart FFI → `libdevconn.so` route geïdentificeerd
- [in progress] 5GenCare controlprotocol: framing en commando-serialisatie reconstrueren
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
- [todo] Standalone Go Magic-handshake en tunnel
- [todo] Lokale RTSP-validatie zonder Android
- [todo] go2rtc-integratie
- [todo] Home Assistant add-on
- [todo] amd64/aarch64 containerbuilds
- [todo] End-to-end- en reconnectvalidatie

`done` betekent dat het betreffende feit daadwerkelijk statisch of tijdens de eigen VM65-sessie is bevestigd. De volledige Magic WEB2-controlketen is nu bewezen en in Go gecodeerd; het resterende blok voor een standalone tunnel is de 5GenCare-controlflow (TLS via ARM native-bridge, niet zichtbaar via x86-Frida). Er is nog geen standalone tunnel en dus nog geen functionele add-on.
