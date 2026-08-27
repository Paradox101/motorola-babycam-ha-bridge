# Current state

Statusdatum: 2026-08-27. Dit document scheidt waarnemingen strikt in **PROVEN**, **LIKELY** en **UNKNOWN**.

## Bronnen

- `REPORT.md`, `runtime-analysis.md`, `vm65-runtime-result.md`
- `endpoints.txt`, `domains.txt`, `interesting-classes.txt`, `native-libraries.txt`
- JADX-output in `analysis/jadx-out/`
- apktool-output en ARMv7-libraries in `analysis/apktool-armv7/`
- Flutter AOT-library `libapp.so`
- Runtime-observatie `runtime-logs/vm65-session.log`
- Interne appdiagnostiek `runtime-logs/flog.db` (gevoelig; git-ignored)
- `scripts/frida_vm65.js` en `scripts/run-vm65-frida.ps1`

## PROVEN — statisch

- De app is Motorola Nursery 2.1.17, package `com.fivegencare.com.motorola.nursery`.
- Flutter-code is AOT-gecompileerd in ARMv7 `libapp.so`; gewone Dart-broncode is niet aanwezig.
- De actieve Magic-library is `libdevconn.so` (ELF32, little-endian, ARM, `ET_DYN`). SHA-256: `0d8e38e9ff9ec0f22cb5351444e230ea84780082e143ae93c680f96092644a4a`.
- `libdevconn.so` heeft alleen ELF-dependencies op `liblog.so`, `libm.so`, `libdl.so` en `libc.so`; er is geen JNI-export gevonden.
- Publieke exports omvatten `magicp2p_connect_device_v0`, `_v1`, `_v3`, disconnectfuncties, `magicp2p_register_callback`, `magicp2p_location_set`, `magicp2p_generate_sid_v1`, async-I/O-functies en NAT-diagnostiek.
- `FepeMagicP2pPlugin.java` is slechts een lege Flutter-platformplugin voor `getPlatformVersion`. Magic wordt dus niet via deze Java MethodChannel uitgevoerd.
- `libapp.so` bevat de Dart-FFI-symbolnamen `magicp2p_connect_device_v1`, `magicp2p_register_callback`, `magicp2p_generate_sid_v1`, `magicp2p_disconnect_device_v1` en `magicp2p_location_set`. De actieve callchain is daarmee Dart FFI → native C-export.
- `libdevconn.so` bevat de modi `LAN2`, `WEB2` en Magic-varianten, relay/DNS/STUN-hostnamen, socketcode, tunneladministratie en diagnostische formats met `local_port`, `target_port`, control- en streamserver.
- `liborbwebm2m.so` plus Java-klassen onder `com.orbweb.*` implementeren een oudere M2M-route. Deze is relevant als vergelijkingsmateriaal, maar niet de gemeten VM65-route.
- `libapp.so` bevat controlcomponenten `SocketImplement`, `SocketDataHandler`, `loginV3`, `loginV3Handler`, `sessionV3`, `sessionV3Handler`, `CMD_DLIST`, `dlistHandler`, `getValueHandler` en `setValueHandler`.

## PROVEN — runtime

- Een normale live-view gebruikte Magic P2P, `magicConnectionMode:2`, `connectLabel:WEB2` en `isUsedMagicP2P:true`.
- De Magic-call kreeg device-ID, SID, device-token, relay-controlhost, `tryDirect:1`, targetpoort 6667 en timeout 300000 ms.
- De callback meldde `targetPort:6667`, `listenPort:16667`, `errorCode:0`.
- Het appproces luisterde daadwerkelijk op `127.0.0.1:16667`; Fijk/IJK maakte hiermee een lokale TCP-verbinding.
- De speler opende `rtsp://<rtsp-user>:<rtsp-password>@127.0.0.1:16667/owner/streaming?accessToken=<temporary-token>`.
- Audio en video startten en de app registreerde `magic_p2p_streaming_succeeded`.
- Een steady-state pcap bewees gescheiden externe flows op 9901 (bulkstream), 2288 (48/52-byte controlrecords) en 3388 (TLS-control). De relaystream bevatte geen plaintext RTSP-markers en had hoge entropie.
- De sessie gebruikte `vrelay-de0.5gen.care` en externe TCP-connecties; er was geen zichtbare directe socket naar het camera-LAN-adres.
- De volledige TCP/9901-capture is offline gedecodeerd met de device-token en de uit `libdevconn.so` gereconstrueerde stateful transformatie. De plaintext bevat de complete RTSP/Digest/SDP/RTP-over-TCP-dialoog.
- De camera levert door de tunnel H.264-video (`camera`) en PCMA/8000-audio (`micphone`).
- Een verse app-start-capture (2026-08-27) legde het plaintext `app`-controlrequest én -response op TCP/8800 vast. De response is de achtveldenvariant; `num`, `streamHost` en `directIp` correleren byte-perfect met respectievelijk het relay-open connectionnummer, de 9901-tunnelbestemming en de mislukte directe poging. Het laatste veld is de connection mode (2 = WEB2).
- `generate_sid_v1` is exact gereconstrueerd; de afgeleide `magicUuid` matcht de runtimewaarde zonder dat de validator secrets toont.
- Control-login gebruikt een blijvende command-socket. Runtime-responsevoorbeelden beginnen logisch met `v3_login`; een eerste shard-response `-6` verwees de client door naar een genummerde `*.moto.5gencare.com` host.
- De app verkreeg een account/master-ID, twee tokenachtige waarden en een session-ID in de succesvolle `v3_login`-response. Waarden zijn hier bewust niet opgenomen.

## LIKELY

- `WEB2` is de web/relayfallback van Magic nadat directe connectiviteit is geprobeerd. Dit wordt sterk ondersteund door `tryDirect:1`, de uiteindelijke `WEB2`-classificatie en relay-TCP-sockets.
- De stream-`accessToken` komt uit het device/controlmodel en niet uit RTSP of `CGI_GetRTSPInfo`.
- `libdevconn.so` is grotendeels platform-C en zou conceptueel porteerbaar zijn, maar de aanwezige binary is ARMv7/Bionic en redistribution/licentie is niet vastgesteld.
- Een kleine Bionic-compatibilitylaag kan technisch onderzoekbaar zijn, maar biedt geen amd64/aarch64-productroute zolang geen geschikte librarybuild of volledige protocolimplementatie bestaat.

## UNKNOWN

### 5GenCare control

- Bestemmingspoort(en) en eventueel TLS-gebruik per controlverbinding.
- Exacte framegrens: delimiter, lengteprefix en/of escaping.
- Exacte requestvolgorde en serialisatie voor normale accountlogin (de gemeten log bevat ook een `guest`-loginpad).
- Passwordtransformatie/challenge, sessievernieuwing en heartbeat.
- Volledige `CMD_DLIST` request/responsevelden en de precieze bron/expiry van het streaming-accessToken.

### Magic WEB2

- ~~Runtime-invulling van het voorafgaande Magic `app ...` controlresponse op controlpoort 8800 en de semantiek van het laatste achtveldenveld.~~ **Opgelost (capture 2026-08-27):** achtveldenvariant runtime-bewezen; laatste veld = connection mode (2 = WEB2). Zie `docs/magic-web2-protocol.md` en `internal/magic/control_discovery.go`.
- Foutcodes en alternatieve direct/LAN-controlresponses (kortere responsevormen nog niet gecaptured).
- Reconnect- en closegedrag bij een verbroken relay.
- Callbackstructuren zijn nog niet volledig getypeerd, maar blokkeren de native Go-route niet meer.

## Actieve callchain

```text
CameraModel.findMagicP2PPort / _findPort (Dart AOT, libapp.so)
  -> Magic SDK Dart FFI wrapper (libapp.so)
  -> magicp2p_connect_device_v1(...) (libdevconn.so)
  -> relay/direct selection
  -> callback(targetPort=6667, listenPort=<dynamic>, errorCode)
  -> CameraModel._getStreamUrl / _getPlayerURL
  -> FijkPlayer / IjkMediaPlayer
  -> RTSP on 127.0.0.1:<listenPort>
```

## Exact ontbrekende onderdeel voor een Android-loze tunnel

De volledige Magic WEB2-controlketen is nu bewezen en in Go gecodeerd: `app`-discovery (`control_discovery.go`), `magicUuid` (`identity.go`), relay-open frame (`relay_open.go`) en de device-tokencrypto-tunnel (`token_crypto.go`). Relayselectie en alle responsevelden zijn per capture 2026-08-27 bevestigd.

Het enige resterende blok voor een zelfstandige Android-loze tunnel is de **5GenCare-controlflow**: zonder bestaande app moeten verse SID, device-token, stream-accessToken en relayparameters worden verkregen en vernieuwd. Deze flow is TLS en gaat via de ARM-vertaalde native code, dus niet zichtbaar via x86-Frida-hooks; een andere observatiemethode is nodig.
