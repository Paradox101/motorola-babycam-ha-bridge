# Missing protocol pieces

## Control socket

Known:

- Commandfamilies `v3_login`, `v3_session`, `CMD_DLIST`, get/set-value.
- Shardredirect van primary/nummerhost en de semantische responsevelden.
- De app bezit na device discovery SID, numeriek device-ID, device-token, relayhost en streaming-accessToken.

Unknown:

- Wireframing, requestencoding, logintransformatie, heartbeat en tokenexpiry.

Needed runtime observation:

- Pre- en post-TLS `connect`, `send`/`recv`, `SSL_write`/`SSL_read` met socketcorrelatie, richting, lengte, timing en veilige redactie.

Target function:

- Dart `SocketImplement`/`SocketDataHandler`; native `connect`, `send`, `recv`, `SSL_write`, `SSL_read` waar zichtbaar.

Expected output:

- Geanonimiseerde frames die request/responsegrenzen en commando-indexen aantonen.

## Magic WEB2

Known:

- Dart FFI roept `magicp2p_connect_device_v1` aan.
- Input omvat sessionName, device-ID, SID, device-token, targetpoort 6667, controlhost, tryDirect en timeout.
- Succescallback bevat targetpoort, dynamische listenpoort en foutcode.
- Uitkomst van de gemeten sessie is `WEB2` mode 2 via `vrelay-de0.5gen.care`.
- Het WEB2-openingsrequest naar TCP/9901 is 139 bytes plaintext met bewezen formatstring `v%03d %03d %05d %03d %s %04d %s`.
- De gemeten invulling begint met `v002 034 06667 078`, gevolgd door een 78-byte identifier en een 36-byte identifier.
- `relay_header` bouwt dit frame; `FUN_00018144` verzendt het via `magic_nwk_connect_send`.
- `generate_sid_v1`, device-tokenbootstrap en de stateful relaytransformatie zijn exact gereconstrueerd en tegen de runtimecapture gevalideerd.
- De volledige TCP/9901-capture decodeert naar geldige RTSP, SDP en interleaved RTP.
- Voorafgaand relay-discoveryrequest: `app <magicUuid> <targetPort> 2 <sessionName>\n`; native default controlpoort 8800.
- **PROVEN (capture 2026-08-27):** de `app`-controlresponse is de achtveldenvariant `app <num> <streamHost> <controlHost> <targetPort> <directIp> <directPort> <mode>`. `num` matcht het relay-open connectionnummer, `streamHost` matcht de 9901-tunnelbestemming, `directIp` matcht de mislukte directe poging, en het laatste veld is de **connection mode** (2 = WEB2). Go-codec: `internal/magic/control_discovery.go`.

Unknown:

- Foutvarianten, reconnectgedrag en de kortere (native bekende) responsevormen. Callback-ABI is nog onvolledig maar blokkeert een native Go-client niet.

Resterende runtime observation:

- Een reconnect-/foutscenario voor de alternatieve responsevormen. Basiscapture (TCP/8800 + 9901 vanaf verse app-start) is gedaan: `runtime-logs/vm65-8800.pcap`.

Target function:

- `FUN_00017cf0`, `magic_nwk_connect_to`, `magic_nwk_socket_send` en `magic_nwk_socket_recv_timeout` op de Magic-controlverbinding.
