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

De eerste relayrespons is 130 bytes met hoge entropie. Daarna volgen onder andere een clientrecord van 176 bytes en de bulkdatastroom. Er zijn geen plaintext RTSP-markers gevonden. `libdevconn.so` bevat een eigen keten van `token_crypto_init_buffer`, `magic_crypt_hash`, `magic_crypt_encode` en `magic_crypt_decode`; koppeling van identifiers/keymateriaal aan deze routines wordt nog onderzocht.

## Wirevelden die nog ontbreken

Voor de relayrespons en gecodeerde tunnelrecords bestaat nog geen betrouwbare offsettabel. Die wordt pas toegevoegd wanneer callgraph en decodepad voldoende bewijs leveren.

## Belangrijkste onbekenden

- relayresponsstructuur en succes-/foutcodes;
- device-/sessionauthenticatie;
- nonce/challenge en HMAC-input/sleutel;
- payloadencryptie en tunneldata-encapsulatie;
- keepalive/reconnect/close.

Tot deze onderdelen bewezen zijn, bestaat er bewust geen Go-`PerformHandshake()` die succes simuleert.
