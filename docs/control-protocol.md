# Motorola/5GenCare control protocol

Status: gedeeltelijk gereconstrueerd; nog niet als standalone client geïmplementeerd.

## Transport

| Eigenschap | Waarde | Status | Bewijs |
|---|---|---|---|
| Host bootstrap | `primary.moto.5gencare.com` | PROVEN | `SocketImplement` runtime-log |
| Account shard | `<n>.moto.5gencare.com` | PROVEN | `v3_login`/`v3_otp` redirect met status `-6` |
| TCP-poort | `3388` | PROVEN | `_initSocket ... port:3388` |
| Beveiliging | TLS via Dart `SecureSocket.secure` | PROVEN | opeenvolgende runtime-logregels |
| Applicatieencoding | tekstcommando's met spatiegescheiden velden | PROVEN | gelogde outbound commando's en geparseste responses |
| Outbound terminator | LF (`\n`) | PROVEN voor meerdere commando's | gelogde commandostrings bevatten afsluitende newline |
| Inbound framing | vermoedelijk regelgebaseerd | LIKELY | responses worden als tokenlijsten afgehandeld; ruwe TLS-plaintext ontbreekt nog |

Er is geen HTTP/REST-laag aangetoond voor login en device discovery.

## Bootstrap en shardredirect

1. Open TCP naar `primary.moto.5gencare.com:3388`.
2. Upgrade de socket met TLS/SNI voor de geconfigureerde hostname.
3. Stuur het gekozen authenticatiecommando.
4. Een response met status/user-ID `-6` en een hostname betekent: sluit de socket, cache de nieuwe shard en herhaal daar.

Geobserveerd, met waarden gemaskeerd:

```text
request:  v3_login <client-uuid> undefined\n
response: v3_login -6 1.moto.5gencare.com
```

Voor de eigen accountflow werd eveneens gezien:

```text
request:  v3_otp <client-uuid> email <redacted-email> 6\n
response: v3_otp -6 4.moto.5gencare.com
```

De betekenis van het laatste veld `6` is nog UNKNOWN; het mag niet als protocolconstante worden geïmplementeerd zonder callsite- of wirebewijs.

## Authenticatieroutes

### Guest bootstrap — PROVEN

```text
v3_login <client-uuid> undefined\n
```

Succesresponse, semantisch:

```text
v3_login <user-id> <session-token> <master-token-or-account-token> <session-id> <shard-host>
```

De exacte naam van responseveld 3 blijft UNKNOWN; de app bewaart meerdere master-/sessiontokenvoorkeuren en de log alleen bewijst de positie en opaque aard.

### E-mail/OTP en loginset — PROVEN voor de gemeten interactieve login

```text
v3_otp <client-uuid> email <email> 6\n
v3_loginset <user-id> <client-uuid> email <email> <one-time-code>\n
```

`v3_otp` retourneerde `<user-id>`, een gemaskeerde OTP-indicator en shardhost. Na normale invoer in de app verstuurde `v3_loginset`; de succesresponse heeft dezelfde token/sessionvorm als `v3_login`.

Dit bewijst **geen** onbeheerde username/password-login. Voor add-onconfiguratie met alleen username/password is nog een andere bewezen loginroute of een veilig eenmalig device-authorizationmechanisme nodig.

### Session restore — gedeeltelijk PROVEN

`sessionV3` bestaat en leest user-ID, token en session-ID uit lokale sessieopslag. De gemeten poging had `token=null` en leverde `V3SessionResult.empty`; daardoor is geen geldig request/responseframe vastgelegd. Exacte commandovorm en vernieuwingsregels zijn UNKNOWN.

## Post-login secret negotiation

Na een succesvolle login stuurt de app:

```text
secret <six-character-client-secret>\n
```

De server antwoordt logisch als:

```text
secret 1 <dotted-token>
```

De betekenis en generatie van beide waarden zijn nog UNKNOWN. Ze mogen niet worden overgeslagen: het controlkanaal verstuurt devicecommando's pas in de hierop volgende sessie.

## Device list

Outbound wordt gecombineerd met andere LF-beëindigde commando's:

```text
profileget\n
paylist\n
msgcount\n
v3_dlist\n
babylist\n
```

Eigen VM65-response:

```text
v3_dlist 1 <device-id> <udid> VM65CONNECT VM65%20CONNECT <device-token> <sid> NL
```

Veldmapping:

| Index | Betekenis | Status |
|---:|---|---|
| 0 | `v3_dlist` | PROVEN |
| 1 | device count | PROVEN |
| 2 | numeriek device-ID | PROVEN |
| 3 | UDID | PROVEN |
| 4 | model | PROVEN |
| 5 | URL-/percent-encoded naam | PROVEN |
| 6 | opaque device-/Magic-token | PROVEN semantiek vanuit latere `connectDevice` call |
| 7 | camera SID | PROVEN |
| 8 | landcode | PROVEN |

Meerdere-camera-recordlengte en eventuele extra capabilityvelden zijn UNKNOWN.

## Streamparameters

De app roept Magic aan met waarden uit het cameramodel:

```text
deviceId, sid, deviceToken, targetPort=6667, controlIp, tryDirect, timeout
```

De RTSP-URL gebruikt daarnaast een tijdelijke `accessToken`. De exacte controlopdracht waarmee dit token wordt verkregen is nog niet geïsoleerd. `getValueHandler`/`setValueHandler`-traffic rond live-view is een onderzoeksdoel; hardcoding van de gemeten token is verboden.

## Heartbeat en reconnect

- Een inbound `ping` verscheen ongeveer iedere 30 seconden. PROVEN.
- Of de client expliciet `pong`, `ping` of een ander antwoord stuurt is UNKNOWN door ontbrekende plaintextcapture.
- Shardredirect sluit en heropent TLS. PROVEN.
- Expiry van session-, device- en streamtokens is UNKNOWN.

## Error handling

- `-6 <hostname>`: shardredirect, PROVEN.
- `0` na `v3_dlist`: lege lijst, PROVEN.
- Overige negatieve/positieve statuscodes: UNKNOWN.

## Implementatieblokkade

Een parser voor de bewezen tekstrecords kan veilig worden gebouwd, maar `Authenticate()` kan nog niet als zelfstandig werkend worden aangemerkt. Ontbrekend zijn een unattended loginroute/sessionrestore, geldig secret-negotiatiegedrag, inbound framegrens en stream-tokenopdracht.

