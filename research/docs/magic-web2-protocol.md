# Magic P2P / WEB2 protocol

Status: native component en semantische API bewezen; het initiële WEB2-relayrequest is nu byte-voor-byte bewezen. Relayrespons, authenticatie en tunnelcodering zijn nog niet volledig gereconstrueerd.

## Client API

`libapp.so` gebruikt Dart FFI en roept de C-export `magicp2p_connect_device_v1` in `libdevconn.so` aan. De ARM Thumb-wrapper is 120 bytes en bouwt een interne parameterstructuur van `0xb0` bytes voordat hij de gemeenschappelijke connectroutine aanroept. De exacte veld-offsets van die structuur zijn nog niet betrouwbaar benoemd.

Runtimeparametersemantiek:

| Parameter | Status |
|---|---|
| session name / client UUID plus targetport | PROVEN |
| numeriek device-ID | PROVEN |
| SID | PROVEN |
| opaque device-token | PROVEN |
| targetpoort 6667 | PROVEN |
| relay-controlhost | PROVEN |
| `tryDirect=1` | PROVEN |
| device name | PROVEN |
| timeout 300000 ms | PROVEN |
| exacte C-volgorde/types | UNKNOWN |

## Resultaatcallback

De serviceready-callback levert minimaal `targetPort`, dynamische `listenPort` en `errorCode`. Een afzonderlijke statusroute levert mode 2 / `WEB2`. Exact callbackprototype en alle eventcodes zijn UNKNOWN.

## Native bouwstenen

- Socketlaag: `magic_nwk_socket_*`, `magic_nwk_connect_*`, DNS, TCP en UDP.
- Tunnel/async-I/O: `magicp2p_aio_app_bridge_create`, `magicp2p_aio_data_send`, `magicp2p_aio_data_recv`, `magicp2p_aio_data_process`.
- Tokenlaag: `token_crypto_init_buffer`, `token_crypto_encode`, `token_crypto_decode`, `token_crypto_send`, `token_crypto_receive`.
- Ingebouwde crypto: SHA-256 en HMAC-SHA256-functies. De aanwezigheid hiervan bewijst nog niet welk handshakeveld ermee wordt berekend.
- NAT/STUN: NAT-check/reportfuncties en STUN-hoststrings.

## `magicUuid` / `generate_sid_v1`

De v1-wrapper ontvangt in volgorde `sessionName`, numeriek device-ID, SID, device-token, targetpoort, controlhost, `tryDirect`, device-naam en timeout. `generate_sid_v1` construeert de 78-byte `magicUuid` als volgt:

```text
lower(hex8(deviceId) || SID[0:3] || deviceToken[0:3] ||
      hex(HMAC-SHA256(key=seed[0:32], message=SID)))

seed = sprintf("%08x%-20s%-27s%-20s", deviceId, SID, deviceToken, SID)
```

Bronnen: `magicp2p_connect_device_v1` (`0x16e30`), `generate_sid_v1` (`0x194fc`), formatstring op `0x13ed2`, en `crypto_auth_hmacsha256` (`0x16160`). De redacterende validator `tools/runtime-protocol-capture/validate_magic_uuid.py` reproduceert met de echte runtime-input precies de gelogde `magicUuid` (`all_match: true`) zonder waarden te tonen. De Go-implementatie staat in `internal/magic/identity.go` met een onafhankelijke synthetische testvector.

## Relay discovery/controlrequest

Voor WEB2 opent `FUN_00017cf0` eerst een verbinding met de geconfigureerde Magic-controlhost. De native default controlpoort is `8800` (`0x2260`). Het plaintext requestformat is:

```text
app <magicUuid> <targetPort> 2 <sessionName>\n
```

De serverparser accepteert op basis van het aantal spatievelden vier vormen:

```text
app <num>
app <num> <value>
app <num> <streamHost> <controlHost>
app <num> <streamHost> <controlHost> <targetPort> <directIp> <directPort> <mode>
```

Bron: `FUN_00017cf0`, outbound formatstring `app %s %d %d %s\n` op `0x15324`, responseformats op `0x13794`, `0x14eba`, `0x141a0` en `0x146e8`. De parser vereist dat het eerste veld case-insensitive `app` is en dat `num > 0`. Bij de achtveldenvariant controleert hij het geretourneerde targetpoortveld, vult stream-/controlhost en directe endpointvelden en stelt de streamrelaypoort in op `9901` (`0x26ad`).

### Runtime-bewezen (capture 2026-08-27)

Een verse app-start met tcpdump op de emulator legde het volledige plaintext `app`-request/response-paar op TCP/8800 vast. Het request is byte-voor-byte `app <magicUuid> <targetPort> 2 <sessionName>\n`. De response is de **achtveldenvariant** en correleert byte-perfect met dezelfde sessie:

```text
request : app <magicUuid:78> 6667 2 <sessionName:36>\n         (127 bytes)
response: app 48 <streamHost> <controlHost> 6667 <cameraLanIp> 77 2\n
```

| Responsveld | Betekenis | Correlatie/bewijs |
|---|---|---|
| `num` = 48 | intern connectionnummer | identiek aan `048` in het relay-open frame `v002 048 06667 078 …` op 9901 — PROVEN |
| `streamHost` | relay-streamhost | exact het IP waarnaar de TCP/9901-tunnel opende — PROVEN |
| `controlHost` | relay-controlhostnaam (`vrelay-…-…NN.5gen.care`) | PROVEN |
| `targetPort` = 6667 | camera-targetpoort | gelijk aan request en runtimecallback — PROVEN |
| `directIp` | camera-LAN-endpoint | gelijk aan de mislukte directe poging `→ <cameraLanIp>:<directPort>` (0 payloadbytes) — PROVEN |
| `directPort` = 77 | direct endpoint-poort | PROVEN |
| **laatste veld** = 2 | **magic connection mode (2 = WEB2)** | lost de voorheen onbekende achtste veldsemantiek op — PROVEN |

Een tweede request in dezelfde capture met `targetPort=0` leverde `app 47 <ander-IP> vrelay-eu-nl01.5gen.care 0 <cameraLanIp> 77 2`, wat de veldposities onafhankelijk bevestigt. De capture staat lokaal in `runtime-logs/vm65-8800.pcap` (git-ignored wegens identifiers); de geredigeerde flow-analyse in `runtime-logs/vm65-8800-flows.json`. De Go-codec staat in `internal/magic/control_discovery.go`.

## Gemeten state machine

```text
connectDevice(... targetPort=6667, controlIp=vrelay-..., tryDirect=1)
  -> lokale listener toegewezen (16667 in de gemeten sessie)
  -> directe poging/relayselectie
  -> uiteindelijke mode WEB2 (2)
  -> service-ready(targetPort=6667, listenPort=16667, errorCode=0)
  -> lokale RTSP-client verbindt
  -> audio/video start
```

## Steady-state wire-observatie

Een 1,200-packet pcap van de reeds actieve eigen stream toont drie gescheiden TCP-rollen:

| Remote poort | Richting/patroon | Interpretatie | Status |
|---:|---|---|---|
| 9901 | circa 577 kB inbound streamdata; veel segmenten van 1440/816 bytes | Magic stream/dataplane | PROVEN als flow; functienaam LIKELY |
| 2288 | herhaalde 48-byte outbound en 52-byte inbound records | Magic control/keepalive | PROVEN patroon; betekenis LIKELY |
| 3388 | gelijke 27-byte TLS-records rond control-ping | 5GenCare controlsocket | PROVEN |

De 9901-sample heeft circa 7.786 bits/byte entropie en bevat geen plaintextmarkers `RTSP/1.0`, `OPTIONS`, `DESCRIBE`, `accessToken` of `owner/streaming`. De 296-byte clientpayload naar 9901 heeft eveneens hoge entropie. Daarmee is bewezen dat de WEB2-relaywire **niet simpelweg plaintext RTSP over een ongeframed TCP-proxy** is. Encryptie of een hoog-entropische codec/encapsulatie is actief; de precieze laag blijft UNKNOWN.

De 2288-recordgroottes zijn stabiel, maar drie observaties zijn onvoldoende om de volledige keepalivecadans vast te leggen. Payloads zijn niet in documentatie gekopieerd.

## WEB2 relay-open request

Een herstart van de app tijdens capture leverde een nieuwe TCP-verbinding naar de relay op. Het eerste clientpayload naar relaypoort `9901` is 139 bytes plaintext en heeft exact deze vorm:

```text
v002 034 06667 078 <78 bytes> 0036 <36 bytes>
```

De formatstring staat in `libdevconn.so` op `0x13deb` en wordt door `relay_header` (`0x19758`) gebruikt:

```c
"v%03d %03d %05d %03d %s %04d %s"
```

`FUN_00018144` roept deze functie aan voordat `magic_nwk_connect_send` de relayverbinding opent. De aanroep geeft versie `2`, het connectionnummer op structure-offset `0xc4`, targetpoort op offset `0xc0`, `magicUuid` op offset `0x14c` en de 36-byte `sessionName` door. Dit is gecorreleerd met de app-log van exact dezelfde sessie: beide lengtes en beide waarden komen bytegelijk overeen. Het debugformat `client_id=... target_port=%d num=%d ...` benoemt offset `0xc4` als `num`.

| Offset | Lengte | Encoding | Gemeten waarde | Betekenis | Status/bewijs |
|---:|---:|---|---|---|---|
| 0 | 4 | ASCII | `v002` | protocolversie 2 | PROVEN: formatstring + capture |
| 4 | 1 | ASCII | spatie | separator | PROVEN |
| 5 | 3 | ASCII-decimaal | `034` | intern connectionnummer (`num`) | PROVEN: native debugformat + callsite |
| 8 | 1 | ASCII | spatie | separator | PROVEN |
| 9 | 5 | ASCII-decimaal | `06667` | camera-targetpoort 6667 | PROVEN: native callsite + runtime |
| 14 | 1 | ASCII | spatie | separator | PROVEN |
| 15 | 3 | ASCII-decimaal | `078` | lengte eerste identifier | PROVEN |
| 18 | 1 | ASCII | spatie | separator | PROVEN |
| 19 | 78 | ASCII | geredigeerd | `magicUuid`, door `generate_sid_v1` afgeleid | PROVEN: bytecorrelatie capture/app-log |
| 97 | 1 | ASCII | spatie | separator | PROVEN |
| 98 | 4 | ASCII-decimaal | `0036` | lengte tweede identifier | PROVEN |
| 102 | 1 | ASCII | spatie | separator | PROVEN |
| 103 | 36 | ASCII | geredigeerd | `sessionName` | PROVEN: bytecorrelatie capture/app-log |

Er is geen afsluitende newline of NUL-byte in het TCP-payload. Capturebewijs staat lokaal in `runtime-logs/vm65-handshake2.pcap`; dit bestand is wegens identifiers en sessiedata uitgesloten van versiebeheer.

Voor oudere/andere modes bevat dezelfde functie ook:

```c
"%03d %05d %03d %s"
```

Deze variant is niet gebruikt door de gemeten WEB2-sessie en wordt daarom nog niet als werkend protocolpad geïmplementeerd.

## Relayrespons en tunneldata

Na het plaintext relay-open request is elke richting onafhankelijk stateful gecodeerd. De key is exact de device-token: `FUN_000162dc` kopieert zijn derde argument naar connection-offset `0x1fc`, waarna `FUN_00018144` die string aan beide `token_crypto_init_buffer`-instanties geeft. De v1-wrapper bewijst dat dit argument de device-token is.

Voor een tokenlengte `L` ziet de eerste gecodeerde write per richting er zo uit:

| Offset | Lengte | Betekenis |
|---:|---:|---|
| 0 | `L` | bootstrapbytes, native gegenereerd in bereik `0x15..0xff` |
| `L` | 1 | initiële state-marker |
| `L+1` | rest | getransformeerde tunnelbytes |

Latere writes bevatten alleen getransformeerde bytes; er is geen lengteheader. TCP is dus na de eenmalige bootstrap een continue byte-stream.

```text
rolling = 0xaa
rolling = rolling XOR (key[i] >> 1) XOR ((bootstrap[i] AND 0x7f) << 1)
key[i] = rolling AND 0xff
state = marker MOD L

c = p XOR key[state]
state = (state + ((key[state] + p) OR 1)) MOD L
```

Decoderen gebruikt `p = c XOR key[state]` en dezelfde state-update. Bronnen: `token_crypto_init_buffer` (`0x19484`), `token_crypto_send` (`0x192f0`), `token_crypto_receive` (`0x193ae`), `magic_crypt_hash` (`0x1fe78`) en encode/decode (`0x1fd84`/`0x1fdfe`).

De offline tool `decode_magic_stream.py` decodeerde hiermee de volledige eigen capture: 2.001 outbound bytes en 1.074.991 inbound bytes. De eerste clientwrite van 176 bytes is 28 bootstrapbytes plus een RTSP `OPTIONS` van 148 bytes; de eerste serverwrite van 130 bytes is 28 bootstrapbytes plus een `RTSP/1.0 200 OK` van 102 bytes. Daarmee is de transformatie end-to-end bewezen. De Go-code staat in `internal/magic/token_crypto.go` en is getest met een onafhankelijke vector en gesplitste TCP-bootstrap.

De gedecodeerde dialoog bevestigt Digest-authenticatie, `OPTIONS`, `DESCRIBE`, twee `SETUP`-requests, `PLAY`, `GET_PARAMETER`, RTP interleaved over dezelfde TCP-tunnel, H.264-video (`camera`), PCMA/8000-audio (`micphone`) en de resource `/owner/streaming?accessToken=<temporary>`.

## Wirevelden die nog ontbreken

De relaydatacodering, de RTSP-tunnel én de voorafgaande `app ...` controlresponse zijn nu runtime-bewezen. Nog nodig is bevestiging van reconnect-/foutvarianten en de kortere responsevormen.

## Belangrijkste onbekenden

- foutcodes en alternatieve direct/LAN-responses (de kortere responsevormen zijn native bekend maar niet gecaptured);
- reconnect-/closegedrag van de relaycontrolstate;
- 5GenCare sessie- en stream-tokenvernieuwing buiten Magic (TLS; niet zichtbaar via x86 Frida wegens ARM native-bridge).

De Magic WEB2-controlketen (discovery → relay-open → tokencrypto-tunnel) is nu end-to-end bewezen en in Go gecodeerd. Een geïntegreerde Go-`PerformHandshake()` blijft bewust achterwege tot de 5GenCare-controlflow (verse SID/device-token/accessToken) is gereconstrueerd, omdat die de inputs levert.
