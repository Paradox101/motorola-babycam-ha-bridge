# VM65 runtimeanalyse: RTSP en Orbweb

## Doel en veiligheidsgrens

Deze workflow observeert uitsluitend de normale live-view van de Motorola Nursery-app met het eigen account en de eigen camera. Het Frida-script verandert geen argumenten of returnwaarden, roept geen API/camerafuncties aan en verstuurt geen extra requests. Account/control-passwords en tokens worden niet gelogd. De RTSP-gebruikersnaam en het RTSP-wachtwoord worden wel gelogd, omdat die expliciet nodig zijn om de eigen stream te identificeren. Bewaar het log daarom als geheim.

## Waarom Frida hier geschikt is

Frida is de eenvoudigste methode omdat de relevante VM65-data op drie goed hookbare grenzen samenkomt:

1. Dart roept `com.example.orbweb.OrbwebPlugin.startM2M(name, sid, cameraPassword, rtspUser, rtspPass, result)` aan.
2. `setupCameraInfo()` maakt een `CameraInfo` met SID, remote ports, RTSP-port, point en RTSP-credentials. Het gewone camera/control-password wordt bewust niet gelezen.
3. `OrbwebPlugin.getPath()` retourneert de URL die uiteindelijk naar Fijk/IJK gaat; `FijkPlayer.onMethodCall(setDataSource)` en `IjkMediaPlayer.setDataSource()` bevestigen de werkelijk afgespeelde URL.

Daarmee zijn directe Dart AOT-hooks op `CameraModel._getPlayerURL`, `_getStreamUrl`, `_startStream` en `findMagicP2PPort` niet noodzakelijk voor het primaire resultaat. Die functies staan in `libapp.so` zonder stabiele geëxporteerde symbolen. Native offset-hooks zouden build-/ASLR-afhankelijk en veel fragieler zijn. De Java plugin- en playergrenzen tonen hun output betrouwbaar.

## Statisch bevestigde signatures en defaults

Bronnen zijn de bestaande JADX-output:

- `com.example.orbweb.OrbwebPlugin.setupCameraInfo(String name, String sid, String cameraPassword, String rtspUser, String rtspPass)`
- `OrbwebPlugin.startM2M(String name, String sid, String cameraPassword, String rtspUser, String rtspPass, MethodChannel.Result)`
- `OrbwebPlugin.getPath(DeviceApi)` en `getPort(DeviceApi)`
- `OrbwebP2PManager.CreateP2PManagerFromID(...)`: overloads met 7, 8, 9 en 10 argumenten
- `ConnectWithAuth(P2PManager, String, String, P2P_OnConnectionReadyListener)`
- `CGI_GetRTSPInfo(int, String, String, CGI_GetRtspInfoListener)`
- `MapPort(P2PManager, int remotePort, P2P_CommonListener)`
- `P2PManager.StartConnectHost(ConnectResultListener)`
- `P2PManager.StartPortMapping(String serverId, int remotePort, int firstLocalPort)`
- `P2PManager.getLocalPort(int remotePort)`
- `M2MDeviceManager.getLocalPort(int)` en `getLocalPort(int,int)`
- `M2MDeviceManager.getRTSPPoint()`, `getSID()`, `getServerID()`, `getP2PType()`, `getLocalIP()`
- `ORBConnectTask.AddNewPort(int remotePort)`
- `TunnelAPIs.addClientPortMapping(String serverId, int localPort, int remotePort)`

Voor deze app zet `OrbwebPlugin.setupCameraInfo()` statisch:

```text
remote RTSP port = 6667
RTSP point       = blinkhd
remote ports     = [6667, 8080, audio port, 80, playback port]
camera auth user = orbweb_user       (niet gelogd met password)
```

`getPath()` bouwt `rtsp://{rtsp_user}:{url_encoded_rtsp_pass}@127.0.0.1:{localPort}/`. Opvallend: `blinkhd` wordt als `CameraInfo.RTSP_POINT` opgeslagen, maar `getPath()` voegt in deze pluginversie het point niet aan de URL toe. Het script rapporteert daarom zowel de werkelijk afgespeelde URL als het afzonderlijke RTSP point.

## Benodigdheden

- Een testtoestel of Android-emulator waarop de app normaal werkt.
- Root-toegang voor `frida-server`, **of** een eigen herverpakte debugkopie met Frida Gadget. De originele APK in deze workspace blijft ongewijzigd. De serverroute is eenvoudiger en verdient de voorkeur.
- Android platform-tools (`adb`).
- Python 3 en `frida-tools` op de pc.
- De CPU-ABI passende `frida-server`. De XAPK bevat alleen `armeabi-v7a`; op een fysiek ARM64-toestel kan de app doorgaans in 32-bit mode draaien, maar de serverbinary moet bij het toestel-OS passen (`arm64` voor een arm64 OS).

Gebruik exact dezelfde Frida major/minor-versie op host en toestel. Installeer bijvoorbeeld:

```powershell
python -m pip install --upgrade frida-tools frida
frida --version
adb shell getprop ro.product.cpu.abi
adb shell getprop ro.product.cpu.abilist
```

Download vervolgens `frida-server-<dezelfde-versie>-android-<abi>.xz` uit de officiële Frida releases en pak het lokaal uit. De concrete versie verandert regelmatig; leid de serverversie af van `frida --version` in plaats van een hardcoded nummer.

## Testtoestel voorbereiden

Onderstaande commando's veronderstellen een rooted testtoestel en een uitgepakte binary `frida-server` in de huidige map:

```powershell
adb devices
adb push .\frida-server /data/local/tmp/frida-server
adb shell su -c "chmod 755 /data/local/tmp/frida-server"
adb shell su -c "/data/local/tmp/frida-server -D &"
frida-ps -Uai
```

Als `su -c "... &"` de server direct stopt, gebruik twee terminals:

```powershell
# Terminal 1
adb shell
su
/data/local/tmp/frida-server

# Terminal 2
frida-ps -Uai
```

Een emulator is bruikbaar als Google Play/app-login/cameranetwerk functioneren en de ABI-split kan draaien. Voor LAN-detectie is een fysiek toestel op hetzelfde wifi-netwerk als de VM65 meestal betrouwbaarder; emulator-NAT kan de gekozen route naar relay/P2P veranderen.

## App starten en log vastleggen

Package: `com.fivegencare.com.motorola.nursery`. Start de app onder Frida zodat vroege plugin-/JNI-calls niet worden gemist:

```powershell
New-Item -ItemType Directory -Path .\runtime-logs -Force | Out-Null
frida -U -f com.fivegencare.com.motorola.nursery -l .\scripts\frida_vm65.js -o .\runtime-logs\vm65-session.log
```

Bij recente Frida-versies wordt de spawned app automatisch hervat. Als de CLI een gepauzeerde prompt toont, voer `%resume` in. Als spawn door Pairip/licentiecontrole niet werkt, start de app normaal en attach daarna:

```powershell
frida -U -N com.fivegencare.com.motorola.nursery -l .\scripts\frida_vm65.js -o .\runtime-logs\vm65-session.log
```

Gebruik daarna alleen normale appbediening:

1. Log normaal in; typ geen wachtwoord in de Frida-console.
2. Wacht tot de eigen VM65 online wordt getoond.
3. Open precies één camera/live-view.
4. Laat beeld circa 20–30 seconden lopen, zodat LAN/P2P/relay en alle port mappings voltooid zijn.
5. Sluit live-view en stop Frida met `Ctrl+C`.

Output verschijnt zowel in de terminal als in `runtime-logs/vm65-session.log`. Deel dit bestand niet ongefilterd: het bevat bewust de RTSP-credentials van de eigen camera.

## Belangrijkste logevents

- `OrbwebPlugin.startM2M`: SID plus RTSP-user/pass; account-password staat als `<redacted>`.
- `OrbwebPlugin.setupCameraInfo`: remote ports, RTSP-port en `blinkhd`.
- `ORBConnectTask.AddNewPort` / `PORT_MAPPING`: remote → local mapping.
- `TunnelAPIs.startConnClient*`: SID/RDZ en native connect-resultaat.
- `DeviceApi.getP2PType`: 1=LAN, 2=P2P/TCP, 4=P2P/UDP, 8=relay, 9=relay/LAN.
- `DeviceApi.getLocalIP` / `ORBConnectTask.peerAddress`: LAN/peer IP.
- `RTSP_URL`, `FijkPlayer.setDataSource`, `IjkMediaPlayer.setDataSource`: definitieve URL.
- `CGI_GetRTSPInfo.raw`: legacy CGI JSON met `STATUS`, `AliveUrl`, `CHANNEL_ID`, `URL`, indien deze codepath wordt gebruikt.
- `SUMMARY`: gecorreleerde waarden in één JSON-regel.

Zoek na afloop compact:

```powershell
rg 'RTSP_URL|SUMMARY|PORT_MAPPING|CGI_GetRTSPInfo|DeviceApi.getP2PType|ORBConnectTask.AddNewPort' .\runtime-logs\vm65-session.log
```

## Java/JNI versus native hooks

`liborbwebm2m.so` exporteert onder meer:

```text
Java_com_orbweb_m2m_TunnelAPIs_startConnClient
Java_com_orbweb_m2m_TunnelAPIs_startConnClient2
Java_com_orbweb_m2m_TunnelAPIs_startClientLan
Java_com_orbweb_m2m_TunnelAPIs_addClientPortMapping
Java_com_orbweb_m2m_TunnelAPIs_GetClientTunnelConnType
Java_com_orbweb_m2m_TunnelAPIs_GetClientTunnelPeerAddress
```

Het script hookt primair de Java native declarations, omdat Frida daar `jstring`-waarden veilig als Java strings ziet. Als controle worden de relevante JNI exports ook met `Interceptor` gehaakt. De native mappinghook leest alleen de integer local/remote ports; accountdata wordt daar niet geïnterpreteerd. Als de JNI exports bij injectietijd nog niet geladen zijn, blijven de Java-hooks volledig bruikbaar. Voor een volgende iteratie kan een `android_dlopen_ext`-hook de native attach uitstellen tot `liborbwebm2m.so` geladen is, maar dit is voor de gevraagde waarden waarschijnlijk niet nodig.

## CGI_GetRTSPInfo en moderne VM65-flow

De oudere SDK-code bevat `OrbwebP2PManager.CGI_GetRTSPInfo()` en parseert:

```json
{"STATUS": 0, "MAX_Channels": 1, "AliveUrl": [{"CHANNEL_ID": 0, "URL": "..."}]}
```

De huidige `OrbwebPlugin` configureert de Motorola-camera daarentegen al met RTSP-poort 6667, point `blinkhd` en Dart-aangeleverde RTSP-credentials. Het is daarom mogelijk dat bij de VM65-live-view geen `CGI_GetRTSPInfo` event verschijnt. Dat is een betekenisvolle uitkomst: de werkelijk gebruikte URL blijft zichtbaar via `getPath`/Fijk/IJK.

## In te vullen eindresultaat

Neem de laatste complete `SUMMARY` plus `RTSP_URL` uit het log:

```text
RTSP URL:
rtsp://[username:password@]HOST:PORT/PATH

Connection type:
LAN / P2P / relay

Local mapped port:
...

Camera RTSP port:
6667 (statisch verwacht; runtime bevestigen)

RTSP point:
blinkhd (afzonderlijk CameraInfo-veld; mogelijk niet opgenomen in getPath URL)
```

Zonder een aangesloten toestel en een door de gebruiker geopende live-view kan deze workspace de concrete SID, gemapte poort, credentials en definitieve URL niet zelf invullen. Het script verzamelt die waarden tijdens één normale sessie zonder extra netwerkacties.

