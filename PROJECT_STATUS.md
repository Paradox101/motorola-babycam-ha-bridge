# Project status

Statusdatum: 2026-08-27

- [done] Statische APK/XAPK-inventarisatie
- [done] Runtime VM65 live-view-observatie
- [done] Camera-targetpoort 6667 bevestigd
- [done] Tijdelijke loopback-RTSP op dynamische poort bevestigd
- [done] Magic P2P `WEB2` relaymodus bevestigd
- [done] Actieve Dart FFI → `libdevconn.so` route geïdentificeerd
- [in progress] 5GenCare controlprotocol: framing en commando-serialisatie reconstrueren
- [in progress] Magic WEB2: exports, ABI, callgraph en wireprotocol reconstrueren
- [done] Magic steady-state flows 9901/2288/3388 en versleutelde/ingekapselde dataplane bevestigd
- [done] Magic connect/handshake-pcap bij app-herstart vastgelegd
- [done] WEB2 relay-open request en formatstring byte-voor-byte bevestigd
- [done] Capturevelden redacterend gecorreleerd met `magicUuid` en `sessionName`
- [done] Geteste Go encoder/parser voor het bewezen WEB2 relay-open frame
- [in progress] Relayrespons en `token_crypto`-sleutelinitialisatie reconstrueren
- [in progress] Geanonimiseerde control- en Magic-wirefixtures maken
- [todo] Standalone Go controlclient
- [todo] Standalone Go Magic-handshake en tunnel
- [todo] Lokale RTSP-validatie zonder Android
- [todo] go2rtc-integratie
- [todo] Home Assistant add-on
- [todo] amd64/aarch64 containerbuilds
- [todo] End-to-end- en reconnectvalidatie

`done` betekent dat het betreffende feit daadwerkelijk statisch of tijdens de eigen VM65-sessie is bevestigd. Er is nog geen standalone tunnel en dus nog geen functionele add-on.
