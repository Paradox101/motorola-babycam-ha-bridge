# Statische analyse Motorola Nursery 2.1.17

Analyse uitgevoerd op 27 augustus 2026. Er zijn geen verzoeken naar Motorola-, 5GenCare- of andere externe APIs verstuurd en er zijn geen accountgegevens gebruikt. De originele XAPK is niet gewijzigd. Werkbestanden staan onder `analysis/`.

## 1. APK overview

De bron is een XAPK (SHA-256 `F867733B7E7FC409D7F399E47C1FFC278C6B401149B077E547A74BC278F15AA9`) met een base APK en ABI-, density- en languagesplits. Base package: `com.fivegencare.com.motorola.nursery`; versie `2.1.17` (`versionCode 4337`); min SDK 24; target SDK 36; compile SDK 36. Dit volgt uit `analysis/apktool-base/apktool.yml` en `AndroidManifest.xml`.

De applicatie is Flutter. De launcher is `com.five.gen.care.chillaxbaby.MainActivity`; het grootste deel van de eigen logica is AOT-gecompileerd in `libapp.so`, waardoor geen gewone Dart-bronbestanden aanwezig zijn. JADX verwerkte 10.412 klassen met 42 decompilatiefouten; bij die gevallen blijft smali in `analysis/apktool-base/smali*` de controlebron.

Belangrijkste permissions: internet/netwerk/wifi, fine/coarse location, camera, microfoon, foreground service, wake lock, notificaties, media/storage, exact alarms en Firebase C2DM. De volledige manifestlijst staat in `analysis/apktool-base/AndroidManifest.xml`. Opvallend zijn de custom M2M-permissions `...permission.RECEIVER_M2M` en `...permission.SEND_M2M`.

Manifestcomponenten: 11 activities, 11 services, 6 receivers en 6 providers. Naast de Flutter launcher zijn er Firebase Messaging/background services, Firebase sessions/analytics, geolocation, billing/RevenueCat/Amazon IAP, LiveChat en sharing/image providers. De exacte namen en exported-flags zijn rechtstreeks in het manifest controleerbaar.

## 2. Cloud architecture

De productieconfiguratie in `assets/flutter_assets/assets/moto_fepe-service.json` definieert project `moto-nursery`, prefix `primary`, suffix `moto.5gencare.com`, opslagkey `MOTO_PROD` en `is_keep_relay=false`. De primaire control-host wordt daarom dynamisch `primary.moto.5gencare.com`. Een meegeleverde dev-config gebruikt `aws22.fepe1.5gencare.com`.

Er zijn drie relevante lagen:

1. Flutter/Dart opent een blijvende custom control-socket naar de 5GenCare-host. `libapp.so` bevat `SocketDataHandler`, `loginV3`, `sessionV3`, `CMD_DLIST`, `getValueHandler`, `setValueHandler`, share-, event-, push- en profile-handlers. Dit is geen aangetoonde REST/GraphQL-flow.
2. Voor camera-connectiviteit wordt Orbweb M2M SDK 2.3.42 gebruikt. `OrbwebM2MSDK.getHost()` kiest `rdz.orbwebsys.com`, `.com.cn` of een custom RDZ-domain. `ORBConnectObject` POST naar `https://{rdz}/api/device/connection`.
3. Orbweb maakt een lokale port mapping naar de camera via LAN, direct UDP/TCP P2P of relay. De speler opent vervolgens een RTSP-URL op `127.0.0.1:{mapped_port}`. De cloud levert dus identiteit/session/device-metadata en helpt rendezvous; de media loopt waar mogelijk P2P/LAN en anders relay.

Firebase (`project_id=fivegencare-motonursery`) verzorgt messaging/analytics/crash reporting. De Realtime Database URL is ingebed, maar statisch is niet bewezen dat camera-control of video daarover loopt.

## 3. API hostnames

De volledige genormaliseerde lijsten staan in `domains.txt` en `endpoints.txt`. Architectuurkritisch zijn:

- `primary.moto.5gencare.com`: productie control socket, afgeleid uit `moto_fepe-service.json`.
- `rdz.orbwebsys.com` / `.com.cn`: Orbweb rendezvous; `OrbwebM2MSDK.getHost()`.
- `https://{rdz}/api/device/connection`: Orbweb device connection; `ORBConnectObject` rond regel 53.
- `vrelay-us00.5gen.care`, `vrelay-cn2.5gen.care`: relay/control literals in `libapp.so`.
- `aws1.moto.5gencare.com:8443`, `app-res.5gencare.com`: content/files.
- `d2fmwnja72gang.cloudfront.net/?domain=...`: HTTP-DNS lookup literal in `libapp.so` (`getBestHttpDns`).
- Firebase, Airtable, Helpdesk, LiveChat en app/deep-link hosts zijn ondersteunend en niet de primaire cameratransportlaag.

Geen concrete `ws://` of `wss://` URL werd gevonden. OkHttp bevat WebSocket-code als dependency, maar er is geen app-specifieke WebSocket-aanroep aangetoond.

## 4. Authentication flow

De statisch reconstrueerbare flow is:

1. De app bepaalt device UUID/platform/FCM-token en opent de 5GenCare control-socket (`package:fepe_core/src/utils/socket_data_handler.dart`, symbols in `libapp.so`).
2. Accountlogin gebruikt `loginV3`/`loginV3Handler`; aanwezige velden/literals omvatten login method, username/email/password, `device_id`, device UUID en push token. Social login is ook aanwezig, maar de exacte wire-serialisatie is door Dart AOT niet volledig zichtbaar.
3. Een geslaagde login retourneert een `V3SessionResult`; `sessionV3`/`sessionV3Handler` valideert of vernieuwt de sessie. Preferences bevatten `PREF_GUEST_MASTER_TOKEN`, `PREF_GUEST_SESSION_TOKEN`, `PREF_LOGIN_TYPE`, `PREF_FCM_TOKEN` en `PREF_SHARE_TOKEN`.
4. `loginSet` bindt/ververst server- en sessiestatus. Bij `CMD_V3SESSION_INVALIDATE_ERROR`/`...DELETED_ERROR` volgt relogin, eventueel met master token (`handleV3Session _tryMasterToken`). Er werd geen afzonderlijk OAuth refresh-token endpoint aangetroffen.
5. Device list volgt via `CMD_DLIST`/`dlistHandler`; objecten bevatten camera/device ID, token, SID, online status, IP, capabilities en access token.
6. Een camera-eigenaar vraagt een streampad via `/owner/streaming?accessToken=...`; een gedeelde gebruiker via `/share/streaming?accessToken=...&token=...`. Bron: literals plus `CameraModel._getPlayerURL` in `libapp.so`.

Er zijn token- en sessieobjecten, maar geen bewijs dat de access tokens JWTs zijn: geen app-specifieke JWT decode/claims-flow is gevonden. “Bearer” komt alleen generiek/in dependencies voor. De precieze socketframing en passwordtransformatie moeten later dynamisch of via verdere AOT-disassembly worden vastgesteld.

## 5. Device API

De Device API is hoofdzakelijk command-based. In `libapp.so` zijn onder meer aanwezig: `CMD_DLIST`, `login`, `v3_loginset`, `v3_session`, `registerDeviceHandler`, `deleteDeviceHandler`, `renameDeviceHandler`, `getValueHandler`, `setValueHandler`, `eventListHandler`, `eventSyncHandler`, `pushRegHandler`, `shareListV2Handler`, `createShareToken`, `caplistHandler` en `profileGet/SetHandler`.

`CameraModel`/`DeviceModel` bevatten `deviceToken`, `cameraId`, `CameraSID`, online/offline, firmware/version, local IP, P2P type en capability lists. `com.orbweb.liborbwebiot.OrbwebP2PManager.CGI_GetDeviceInfo()` (regels circa 355-398) leest `DeviceType`, `DeviceName`, `SerialNum`, hardware/software version en capabilities `PTZ`, `VIDEO_QUALITY`, `PICTURE_ADJUSTMENT`, `SUPPORT_NOTIFY`, `SUPPORT_PUSHTOTALK` en `SUPPORT_FISHEYE`.

Functies voor de gevraagde features zijn aantoonbaar aanwezig in de AOT-symbolen:

- temperatuur: `CameraSettingTemperatureViewModel`, temperature notification en `CameraEventPanelTemperature`;
- motion/sound: corresponderende setting viewmodels en notification/event parsers;
- snapshots/recordings: `/Snapshot`, `/Record`, `/Video`, camera snapshot/recording controls, TF-card viewmodel;
- pan/tilt: `CameraPan`, `CameraTilt`, Orbweb capability `PTZ`;
- two-way audio: `PushTalk.openSocket()/SendAACAudio()/SendAudio()` en `talkStatus START/STOP`;
- notifications: Firebase Messaging plus `CameraNotifyEvent`, `pushRegHandler`;
- status: `DeviceStatus`, `Device is offline`, `device online` en device update handlers.

## 6. Camera/video architecture

De sterkste statische reconstructie van “open camera” naar beeld is:

1. Dashboard selecteert `CameraModel` en initialiseert `CameraLiveStreamService` (`DashboardCameraViewModel`, `_initLiveStreamService`, symbols in `libapp.so`).
2. `_findStreamType` kiest LAN/Magic P2P/Orbweb/relay op basis van model, land/config, bereikbaarheid en entitlement. Literals tonen `STREAM_TYPE_MAGIC_ONLY`, `isStreamM2M`, `isUsedRelay` en automatische omschakeling na 30 minuten (`change_type_livestream_time`).
3. `_getPlayerURL` gebruikt camera owner/share access token en vraagt de streaminggegevens. Voor Magic P2P zoekt `findMagicP2PPort`; anders initialiseert de app Orbweb.
4. `OrbwebP2PManager.CreateP2PManagerFromID()` gebruikt camera SID en server-ID. `P2PManager.StartConnectHost()` opent LAN/P2P/relay en `MapPort()` projecteert remote poorten lokaal.
5. Authenticatie naar de camera vindt plaats via mapped port 9001: `ConnectWithAuth()` → `CGI_Auth(getLocalPort(9001), serverID, credential1, credential2)`.
6. `CGI_GetRTSPInfo()` (command 1004) retourneert JSON met `STATUS`, `MAX_Channels` en `AliveUrl[]` (`CHANNEL_ID`, `URL`). `DeviceApi.getRTSPPoint()` biedt hetzelfde concept hoger in de SDK.
7. `_getStreamUrl`/`_startStream` bouwt een URL zoals `rtsp://127.0.0.1:{mapped_port}/...`. Een ingebedde variant gebruikt vaste RTSP basic credentials; de waarden zijn in deze repository geredigeerd. Of deze variant voor de VM65 daadwerkelijk wordt gekozen moet dynamisch worden bevestigd.
8. IJKPlayer/FFmpeg decodeert RTSP; native libraries bevatten RTSP demuxing en hardware decoding. Foutpaden schakelen tussen Magic P2P en Orbweb/relay.

Dit is geen WebRTC-architectuur: er zijn geen app-specifieke SDP/ICE/WebRTC classes of signaling URLs. STUN komt wel voor, maar intern in Orbweb voor NAT traversal, niet als WebRTC ICE stack. HLS/m3u8 en RTMP zijn niet aangetoond als live-cameraflow.

## 7. P2P/WebRTC/streaming libraries

De proprietary SDK is Orbweb M2M/P2P 2.3.42 (`OrbwebP2PManager.getVersion()`). `TunnelAPIs` laadt `liborbwebm2m.so` en exposeert JNI voor `startConnClient`, `startClientLan`, port mappings, peer address en connection type. `TunnelAPIs` definieert LAN=1, TCP=2, UDP=4, P2P=6/7, relay=8 en relay-LAN=9.

Native strings identificeren `CSTUNUDP`, `CSTUNTCPEx`, NAT detect, TCP shunt, `CTcpRelayConnection`, direct connection, `CP2PProxy`, AES tunnelinitialisatie en relay fallback. Dit is een eigen tunnel-SDK, niet WebRTC. `libdevconn.so` en Dart/JNI glue verbinden de Flutterlaag hiermee.

Voor playback zijn IJKPlayer (`libijkplayer`, `libijkffmpeg`, `libijksdl`) en daarnaast FFmpegKit aanwezig. OkHttp, Volley en WebSocket implementations zitten in de APK, vooral voor plugins/integraties. Retrofit-, GraphQL-, gRPC- en MQTT-appinterfaces zijn niet gevonden. Aanwezigheid van librarycode alleen is niet als actief protocol geïnterpreteerd.

## 8. Native libraries

De complete lijst met grootte en rol staat in `native-libraries.txt`. De ABI-split bevat uitsluitend `armeabi-v7a`; een toekomstige bridge kan de Android `.so` niet rechtstreeks als normale x86_64 Linux-library gebruiken. `liborbwebm2m.so` is de cruciale closed-source component. FFmpeg/IJK vormen de mediaconsument; `libapp.so` bevat Dart AOT businesslogic.

Opvallende functies/strings in `liborbwebm2m.so`: alle `Java_com_orbweb_m2m_TunnelAPIs_*` JNI exports, `ConnTunnelClient/Server`, `CP2PProxy`, STUN UDP/TCP, relay, port mapping, device/server ID, session ticket en OpenSSL/cURL. Geen MQTT-symbolen en geen WebRTC-engine (`libwebrtc`, PeerConnection, SDP/ICE API) gevonden.

## 9. TLS/certificate pinning

`AndroidManifest.xml` zet `usesCleartextTraffic="true"` en verwijst niet naar een Network Security Config. Er is geen app-specifieke `CertificatePinner.Builder.add`, pin hash (`sha256/...`), custom `X509TrustManager`, permissive `HostnameVerifier` of eigen `SSLContext` gevonden. De OkHttp TLS/pinner-klassen zijn standaard librarycode. Dart bevat de standaard `SecurityContext`/bad-certificate runtime-symbolen, maar geen bewijs dat de app een callback registreert of pins toepast.

Orbweb gebruikt ingebouwde cURL/OpenSSL-code. `OrbwebM2MSDK.initConfig()` maakt `.config`, roept `ORBConnectionManager.setConfigPath()` aan en verwijdert een bestaande `cert.pem`; dat wijst eerder op ingebouwde/system CA-validatie dan op een door de app meegeleverd pinbestand. Toch kan de native Orbweb-laag afwijkend valideren; dat is statisch niet volledig bewezen.

Conclusie: gewone HTTPS/Dart-verzoeken zijn waarschijnlijk via een debugging proxy te bekijken wanneer de proxy-CA op een testtoestel daadwerkelijk wordt vertrouwd. Vanaf Android 7 vertrouwen apps standaard geen user-installed CA zonder network-security opt-in, dus een stock toestel kan alsnog instrumentation/system-CA-installatie vereisen. De custom control-socket en Orbweb P2P/RTSP zijn geen gewoon HTTPS-proxyverkeer. Dit is een technische observatie, geen uitvoering van interceptie.

## 10. Relevant classes/files

Zie `interesting-classes.txt`. De belangrijkste controlepunten zijn:

- `analysis/apktool-base/AndroidManifest.xml` en `apktool.yml`;
- `assets/flutter_assets/assets/moto_fepe-service.json`;
- `libapp.so`: AOT symbols `CameraModel._getPlayerURL`, `_getStreamUrl`, `_startStream`, `loginV3Handler`, `sessionV3Handler`, `dlistHandler`;
- `com/orbweb/liborbwebiot/OrbwebM2MSDK.java`: `getHost`, `setRDZDomain`, `setupDomainAddress`;
- `com/orbweb/m2m/ORBConnectObject.java`: `/api/device/connection`;
- `com/orbweb/liborbwebiot/OrbwebP2PManager.java`: `ConnectWithAuth`, `CGI_GetDeviceInfo`, `CGI_GetRTSPInfo`, `MapPort`, `CreateP2PManagerFromID`;
- `com/orbweb/m2m/TunnelAPIs.java`: JNI tunnel API;
- `com/orbweb/libcmdservice/PushTalk.java`: two-way audio.

## 11. Example API flow

Onderstaand is een architectuurvoorbeeld, geen kant-en-klaar requestrecept en niet extern getest:

```text
resolve primary.moto.5gencare.com
  -> open proprietary control socket
  -> v3_login(username/password, device UUID, login method)
  <- master/session token + account/profile identifiers
  -> v3_session / loginset(session token)
  -> CMD_DLIST
  <- DeviceModel(cameraId, deviceToken, SID, serverId, online, capabilities)
  -> owner/streaming(accessToken) OR share/streaming(accessToken, shareToken)
  <- stream/session metadata
  -> Orbweb rendezvous: POST https://rdz-host/api/device/connection
  -> CreateP2PManagerFromID(SID, serverId, LAN/P2P/relay candidates)
  -> map camera port 9001 locally; CGI_Auth
  -> CGI_GetRTSPInfo / getRTSPPoint
  <- AliveUrl[channel] / RTSP point
  -> map RTSP camera port locally
  -> play rtsp://127.0.0.1:{mappedPort}/{rtspPoint}
```

De namen van de control commands zijn betrouwbaar uit AOT-strings; veldvolgorde, framing, poorten van de control socket en crypto zijn nog niet volledig statisch gereconstrueerd.

## 12. Feasibility for Home Assistant

Een bridge is technisch waarschijnlijk haalbaar, maar niet als simpele cloud-REST-naar-RTSP adapter.

- Hoogste kans: een Android-sidecar/emulator die de officiële ARMv7 Orbweb SDK gebruikt, de camera sessie opzet en de lokale RTSP-poort exporteert naar go2rtc. Dit hergebruikt de moeilijkste proprietary NAT traversal.
- Goede lokale variant: als VM65 op hetzelfde LAN een direct RTSP-punt aanbiedt en de app alleen credentials/point hoeft op te vragen, kan een Python bridge na authenticatie rechtstreeks verbinden. Dit moet op het eigen netwerk dynamisch bevestigd worden.
- Moeilijkste pure-Python variant: 5GenCare socket protocol plus Orbweb rendezvous/NAT traversal opnieuw implementeren. Het control protocol is mogelijk reconstrueerbaar; de closed Orbweb tunnel met STUN/TCP/UDP/relay en key exchange maakt volledige herimplementatie kostbaar.
- Een cloud-only URL die permanent aan go2rtc kan worden gegeven is onwaarschijnlijk: de gevonden RTSP URL wijst naar een tijdelijke lokale mapped port en vereist een actieve sessie/tunnel.

De beste Home Assistant architectuur is daarom aanvankelijk: Motorola cloud-auth → helper met Orbweb tunnel → lokaal RTSP → go2rtc/Home Assistant. Tokenopslag, sessievernieuwing en reconnect bij P2P/relaywisseling moeten expliciet worden ontworpen.

## 13. Recommended next steps

1. Voer op een eigen testaccount gecontroleerde dynamische observatie uit van DNS, bestemmingspoorten en control-socket framing, zonder acties buiten normale appbediening. Leg vooral `loginV3`, `sessionV3`, `CMD_DLIST` en stream-response vast.
2. Log op een testtoestel de MethodChannel/JNI-aanroepen rond `CameraModel._getPlayerURL`, `CreateP2PManagerFromID`, `MapPort` en `_startStream`; hiermee worden SID, remote/local ports en RTSP point zichtbaar zonder video-inhoud te hoeven onderscheppen.
3. Test vanaf hetzelfde LAN of de in `CGI_GetRTSPInfo.AliveUrls` teruggegeven URL direct bereikbaar is, en welke credentials de VM65 daadwerkelijk accepteert.
4. Prototype eerst een Android helper/service die `liborbwebm2m.so` via de bestaande Java API laadt en één mapped RTSP-poort bindt. Exporteer alleen naar localhost/Docker-netwerk en laat go2rtc restreamen.
5. Reverse-engineer daarna pas de AOT socketserialisatie (liefst via runtime tracing; anders Ghidra/Blutter op `libapp.so`). Documenteer command IDs, framing, reconnect, token expiry en rate limits.
6. Behandel ingebedde Google/API keys als identifiers, niet als secrets. Roteer of publiceer niets en stuur geen probe-requests. Gebruik uitsluitend eigen camera/account voor vervolgtests.

### Configuratiebevindingen

`res/values/strings.xml` bevat Firebase project `fivegencare-motonursery`, app ID `1:257391575237:android:7725fc79fd7ee46f7d30ac`, Realtime Database URL en een Google API key. Dit zijn gebruikelijke clientconfiguratie-identifiers en geven geen bewezen toegang. `google-services.json` zelf is niet aanwezig. Verder zijn subscription/device catalog JSONs, talenbestanden, Firebase metadata, Flutter `AssetManifest.json` en productie/dev service-configs aanwezig.

### Beperkingen

Dit rapport maakt onderscheid tussen bewezen actieve appflow en enkel meegebundelde librarycode. Door Flutter AOT zijn exacte Dart-method bodies niet als Java gedecompileerd; functie- en bronpadnamen, literals, JNI/Java-callchains en native strings leveren wel sterke architectuurbewijzen. Zonder netwerkverkeer of accountgebruik zijn exacte control-socketpoort, wireframing, tokenformaten en het VM65-specifieke RTSP-pad bewust niet als feit ingevuld.
