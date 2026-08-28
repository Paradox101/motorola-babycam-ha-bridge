# Motorola VM65 runtime result

Datum sessie: 2026-08-27  
App: Motorola Nursery 2.1.17 (`com.fivegencare.com.motorola.nursery`)  
Testomgeving: Android 11 Google APIs-emulator, x86_64 met ARM native translation, Frida 17.17.0

## Resultaat

```text
Camera:
Motorola VM65 CONNECT

Camera LAN IP:
Niet beschikbaar / niet gebruikt in deze sessie

Camera SID:
<REDACTED_CAMERA_SID>

Server ID:
Niet als legacy Orbweb serverId beschikbaar in deze Magic P2P-flow
(device_id in de Magic-call: <REDACTED_DEVICE_ID>; Motorola-cloudshard tijdens login: 4.moto.5gencare.com)

Connection type:
relay (Magic P2P WEB2, magicConnectionMode 2)

Remote RTSP port:
6667

Local mapped port:
16667 (127.0.0.1)

RTSP point:
owner/streaming?accessToken=<session-token>

RTSP username:
<RTSP_USERNAME>

RTSP password:
<RTSP_PASSWORD>

Actual player RTSP URL:
rtsp://<RTSP_USERNAME>:<RTSP_PASSWORD>@127.0.0.1:16667/owner/streaming?accessToken=<TEMPORARY_ACCESS_TOKEN>

Orbweb required:
YES — de actieve implementatie is de propriëtaire Magic P2P/WEB2-tunnel, niet de legacy Java-M2M-flow
```

## Runtimebewijs

Frida onderschepte zowel `FijkPlayer.setDataSource` als `IjkMediaPlayer.setDataSource` met exact bovenstaande URL. Android `ss -tpna` bevestigde dat PID 6238 op `127.0.0.1:16667` luisterde en dat de speler daarmee via loopback verbonden was.

De eigen persistente diagnostische log van de app (`runtime-logs/flog.db`) bevat voor dezelfde succesvolle live-view:

- `connectDevice ... targetPort:6667 ... controlIp:vrelay-us00.5gen.care, tryDirect:1`
- `Service ready callback ... targetPort:6667, listenPort:16667, errorCode:0`
- `isUsedMagicP2P:true`
- `magicConnectionMode: 2`
- `connectLabel:WEB2`
- `Magic server: vrelay-de0.5gen.care`
- `magic_p2p_streaming_succeeded`
- `videoRenderStart:true` en `liveStreamState:started`

Tijdens het livebeeld had het app-proces externe TCP-sessies naar onder andere `52.55.137.219` op poorten 2288, 3388 en 5588. Er was geen verbinding naar een camera-adres op het lokale LAN. Samen met `WEB2` is dit sterk en expliciet bewijs voor de relay/webroute in deze sessie.

## Interpretatie

De feitelijke keten was:

```text
Fijk/IJK player
  -> RTSP op 127.0.0.1:16667
  -> Magic P2P lokale tunnel
  -> WEB2 / vrelay-*.5gen.care
  -> VM65 target port 6667
```

De statisch gevonden waarde `blinkhd` hoort bij de oudere `OrbwebPlugin`/M2M-code. Die codepath vuurde tijdens deze sessie niet. De werkelijk gebruikte RTSP-resource is `/owner/streaming` met een tijdelijke `accessToken`.

`CGI_GetRTSPInfo`, `AliveUrl`, `AliveUrls` en `CHANNEL_ID` kwamen niet voor: de huidige VM65 Magic-flow construeert de speler-URL zonder die legacy CGI-call. Ook `p2pType` bleef `-99`, omdat dit veld bij de niet-gebruikte M2M-flow hoort; de relevante Magic-classificatie is `WEB2`.

## Home Assistant-conclusie

Directe LAN-RTSP is met deze runtimegegevens niet aangetoond en is daarom niet getest. De bewezen stream is alleen lokaal bereikbaar zolang de Motorola-app en Magic P2P-tunnel actief zijn. Een toekomstige bridge zal dus waarschijnlijk een Android-helper nodig hebben die de sessie opzet en `127.0.0.1:16667` naar een RTSP-restreamer/go2rtc exporteert. Bouw hiervan is nog niet uitgevoerd.

## Beveiliging

`runtime-logs/vm65-session.log` en `runtime-logs/flog.db` bevatten camera- en sessiegeheimen. Deel of commit deze bestanden niet. De `accessToken` is sessiegebonden en moet niet als permanente Home Assistant-credential worden beschouwd.
