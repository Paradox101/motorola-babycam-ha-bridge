# 5GenCare control flow — static reconstruction from `libapp.so`

Statusdatum: 2026-08-28. Bron: **statische extractie** van leesbare strings uit
de Dart-AOT-snapshot `analysis/apktool-armv7/lib/armeabi-v7a/libapp.so`
(20 MB, 64.975 ASCII-strings). Dit vult het `[blocked]` 5GenCare-controlblok in
`docs/missing-protocol-pieces.md` zonder live capture — de 5GenCare-TLS loopt in
de ARM Dart/BoringSSL-laag, onzichtbaar voor x86-Frida, en de reFlutter-route
strandt op PairIP's native anti-tamper (`docs/reflutter-mitm-capture.md`).

**Aard van dit bewijs:** namen, endpoints, foutcodes, veldnamen, format-fragmenten
en crypto-primitieven zijn PROVEN aanwezig in de binary. De exacte byte-serialisatie,
de meeste commando-**nummers** en de precstelling van velden staan inline in de
gecompileerde Dart-code en vergen `blutter` (volledige Dart-decompile op Linux/WSL)
of een live capture op een ARM-toestel. Zie "Nog te halen".

## Architectuur: twee lagen

De app-servicelaag heet **`Fepe*`**. De relevante klassen:

- `FepeAuth`, `FepeAuthServiceInternal` — authenticatie/sessie.
- `FepeSocketClient` — de persistente control-**socket** (V3-session/dlist).
- `FepeDevice`, `FepeDeviceStreamResponse`, `FepeStreamResponse` — device/stream.
- `FepeError{api: ...}`, `FepeOptions.fromJson`, `fepe-service.json` — config/errors.

Twee onderscheiden transporten:

1. **Account-REST-API** (HTTPS, `*.5gencare.com`) — account/registratie/share.
2. **V3-session-controlsocket** (`FepeSocketClient`) — genummerde `CMD_*`-commando's,
   login/sessie, device-list, keepalive.

## 1. Account-REST-API (endpoints PROVEN)

```
/accountRegister
/accountLogin
/accountSignupOnetimepassword
/accountAddemail
/accountAddphone
/accountOwnerLogedin
/accountOwnerLogedinCreateshareID
/accountOwnerLogedinShareID
/ShareChillax        /ShareAddChillax
/share  /share_add
/device_list
/owner/streaming?accessToken=<token>
/share/streaming?accessToken=<token>&token=<shareToken>
```

Queryvlaggen: `?from_login=true|false`, `?babyToken=`, `?share_token=`,
`&token_expired=`. De streaming-URL's bevestigen de eerder gemeten vorm
(`_getPlayerURL`, `/owner/streaming?accessToken=…`).

## 2. V3-session-controlsocket (`FepeSocketClient`)

Loginketen (methodenamen PROVEN): `loginV3` → `loginV3Handler` (`_loginV3Handler:recv:`)
→ `sessionV3` / `_initingSessionV3` → `V3SessionResult` (`_login V3SessionResult => ok`,
`V3SessionResult.empty`). Er is een guest- én een master-token-pad:
`CMD_V3_SESSION loginMethod guest`, `_isV3SessionWithMasterToken`, `_tryMasterToken`.

Serialisatie-invoer (namen PROVEN, inhoud UNKNOWN): `loginInputString:`,
`sessionInputString:`, `loginMethod:`, `sessionId`.

Bekende commando's (`CMD_*`):

| Commando | Bewijs | Nummer |
|---|---|---|
| `CMD_DLIST` | device-list request | `-` (dlist-fouten: `-6`, `-16`) |
| `CMD_PROFILE_GET` | profiel lezen | **-92** (PROVEN) |
| `CMD_PROFILE_SET` | profiel schrijven | UNKNOWN |
| `CMD_PING` / `CMD_PONG` | keepalive | UNKNOWN |
| `CMD_V3_SESSION` | sessie/login | UNKNOWN |
| `CMD_V3SESSION_DELETED_ERROR` | sessie verwijderd | UNKNOWN |
| `CMD_V3SESSION_INVALIDATE_ERROR` | sessie ongeldig | UNKNOWN |

Wire-/frame-fragmenten (PROVEN aanwezig): `cmd:[`, `, _payload`, `startFrame=`/`endFrame=`,
`dlist:recv:`, `_write cmd:`, `_checkResponseAdd countResponseCmd:`, `lastSendCommand:`.
Keepalive/health: `checkHealth sendMsg ping`, `keepAlive:`, `_timerSendData:`,
`CMD_PING _appPause:`. Timeout/reconnect: `dlist timeout, to maintenance mode`,
`sessionV3 TimeOut`, `_addTimeoutV3Session`, `ERROR device: socket connect:`.

DLIST-foutcodes (PROVEN): `ERROR -6 dlist N is not number`,
`ERROR -16 dlist dev.id error:`, `ERROR dlist parse N`, `DLIST_RESPONSE`.
Dit sluit aan op de runtime-observatie dat een eerste shard-`-6`-response de client
doorverwees naar een genummerde `*.moto.5gencare.com`-host.

## 3. Token-/SID-/accessToken-model (veldnamen PROVEN)

`sid` (`sid:`, `sid=`, `, SID:`), `deviceId`/`device_id`/`deviceIds`, `deviceToken`
(`deviceToken:`), `_deviceUuid`, `_magicUuid` (`MagicConnectStateModel(magicUuid:`),
`accessToken` (stream), `shareToken`/`share_token`/`babyToken`, `masterToken`,
`expireDate`/`token_expired`. Lokale opslag: SQLite-kolommen `device_token text NOT NULL`,
`token text NOT NULL`.

Dit bevestigt dat de app na login een account/master-id, tokenwaarden, een
session-id, per-device SID + device-token, en een per-stream accessToken bezit —
exact de inputs die `internal/bridge`/`cmd/vm65-bridge` nu als `Credentials` krijgt.

## 4. Crypto-primitieven (PROVEN aanwezig)

`AES_CBC_PKCS7Padding`, `RSA_ECB_PKCS1Padding`, `keyToMd5`, `package:crypto/src/md5.dart`,
`package:crypto/src/sha1.dart`, `base64`/`base64Encode`/`base64Decode`. Let op:
`StorageCipherAlgorithm`/`KeyCipherAlgorithm`/`encryptedSharedPreferences`/`AesHeader`
horen bij `flutter_secure_storage` (lokale opslag), niet noodzakelijk bij de wire.
RSA-ECB-PKCS1 + AES-CBC + MD5 zijn de kandidaten voor de login-/wachtwoordtransformatie
en/of controlpayload-encryptie; welke waar wordt toegepast is UNKNOWN zonder de
methodebodies.

## 5. Server/region/shard-redirect (PROVEN)

`changeServerIdAddress` (`loginV3Handler changeServerIdAddress2`),
`requestChangeServer new domain`, `_orbWebRegion`/`Current OrbWeb Region:`,
region-hosts o.a. `vrelay-cn2.5gen.care` (`App in CN and change control server to CN`).
Bevestigt de primary→shard-redirect en regio-afhankelijke controlhost uit
`docs/current-state.md`.

## Veld-inventaris (snake_case JSON-keys, PROVEN aanwezig)

195 snake_case-keys geëxtraheerd; de voor de tunnel/add-on relevante:

**V3-endpoints/commando's:** `v3_login`, `v3_loginset`, `v3_otp`, `v3_otpadd`,
`v3_session` (naast de camelCase-methoden `loginV3`/`sessionV3`). `from_login`,
`otp`/onetimepassword-pad bevestigd.

**Account/sessie:** `user_id`, `user_email`, `profile_id`, `entitlement_id`,
`access_control`, `module_access`, `session_start`, `session_start_with_rollout`,
`token_expired`, `token_limit`, `token_multi_access`, `max_devices`,
`number_of_device`, `allow_user_subscribe`.

**Device-list (`CMD_DLIST`/`device_list`) — per camera:** `device_token`,
`device_name`, `device_model`, `device_url`, `device_online`, `camera_fw_version`,
`camera_mac_address`, `camera_model`, `supported_camera`, `stream_type`,
`streaming_type_models`.

**Magic-relayparameters (uit de dlist-respons):** `magic_con_addr` (controlhost),
`magic_def_addr` (default/fallback), `magic_port` (targetpoort), `magic_bw`,
`magic_limit_minutes`, `magic_countries`/`STREAMING_MAGIC_COUNTRIES`, `magic_only`,
`magic_con_addr`+`magic_port` = exact de `ControlHost`/`TargetPort` die
`internal/bridge.Credentials` nodig heeft. `camera_find_magic_port`/
`camera_found_magic_port`/`camera_magic_connection_mode` bevestigen de FFI-flow.

**RTSP-stream (voor de player-URL):** `rtsp_user`, `rtsp_pass`, `rtsp_transport`,
`rtsp_flags`, `stream_type`. Samen met `accessToken` uit de streamrequest vormt
dit `rtsp://<rtsp_user>:<rtsp_pass>@127.0.0.1:<port>/owner/streaming?accessToken=…`.

**Share:** `share_token`, `create_share_token`, `edit_share_token`, `share_list`,
`share_token_allow`/`_reject`/`_limited`/`_unlimited`, `share_expired_date_null`.

**Server/region:** `server_ip`, `server_ddress` (sic), `file_server`,
`ok_and_change_server`, `china_region`.

**Foutcodes (streaming/p2p):** `error_p2p_disconnected`, `error_p2p_got_error`,
`error_timeout`, `error_connection_refused`, `error_not_network`, `failed_no_port`,
`m2m_error`, `m2m_port`, `magic_p2p_s_exp_invalid_data`, `magic_p2p_s_exp_permitted`,
`streaming_returned_not_found`, `streaming_exception_permitted`.

Conclusie: de **`device_list`-respons is de sleutel** — die levert per camera de
`device_token`, `magic_con_addr`, `magic_port`, `rtsp_user`/`rtsp_pass`; de
`accessToken` komt uit de streamrequest. Precies de `Credentials` die de bridge nu
handmatig krijgt, worden dus door één geautoriseerde `v3_login`+`device_list`-flow
geleverd. Dat is het te implementeren stuk voor een standalone, werkende add-on.

## Wire-framing van de controlsocket (orbweb-voorloper — PROVEN in Java)

De `com.orbweb.libcmdservice`-klassen (`CMDConnect`, `REQPacket`) zijn **Java** en
dus volledig gedecompileerd (`analysis/jadx-out/`). Zij implementeren de M2M-
commandosocket die de directe voorloper is van de 5GenCare-V3-controlsocket
(de Dart-app deelt de `Keep-Alive`-JSON-vorm en `CMD_*`-namen). Het frameformaat:

**Frame (`CMDConnect.SendBuffer`/`ReadCmd`, little-endian):**

```
[ int32 type = 1 ][ int32 length ][ payload : length bytes ]
```

- Header = 8 bytes: `putInt(1)` (type/magic) + `putInt(payloadLength)`, `ByteOrder.LITTLE_ENDIAN`.
- Payload = de commandostring, `ISO-8859-1`, aan de leeskant gesplitst op een
  separator (`\0`) en van whitespace ontdaan, dan als JSON geparset.
- Transport: TCP naar `127.0.0.1:<port>` (de lokale relay/tunnelpoort),
  `setKeepAlive(true)`, `SoTimeout=3000ms`, aparte keepalive- en leesthreads.

**Payload-vorm (`REQPacket`):**

```
{"CMD_ID":"<cmd>","<key>":<value>,"Keep-Alive":true}      (JSON)
CMD_ID=<cmd>&<key>=<value>                                 (URL-form variant)
```

Orbweb-commando's: `CMD_ALIVE`, `CMD_PORT`, `ping`/`ping_time`. De 5GenCare-V3-set
(`CMD_DLIST`, `CMD_PROFILE_GET=-92`, `CMD_PING`/`CMD_PONG`, `CMD_V3_SESSION`) is
V3-specifiek maar past in ditzelfde `{"CMD_ID":…,"Keep-Alive":true}`-schema.

**Status:** het frameformaat is **PROVEN voor de orbweb-M2M-socket** en **LIKELY**
identiek voor de 5GenCare-V3-controlsocket (`FepeSocketClient`); de laatste
bevestiging (byte-exacte V3-frames) vergt een ARM-capture (zie "Hoe dit te halen").
Dit is
niettemin een direct implementeerbaar startpunt voor een Go-controlclient:
`[1|len|json]`-framing + `{"CMD_ID":…}`-payload over TCP.

## Orbweb-commandovocabulaire (PROVEN in Java — referentievorm voor V3)

Volledige `CMD_ID`-JSON-templates uit `com.orbweb.*` (`%s`→`<v>`):

```
{"CMD_ID":"DEVICE_INFO_REQ"}
{"CMD_ID":"DEVICE_CGIQUERY_REQ"}
{"ACCOUNT":"<v>","PWD":"<v>","CMD_ID":"DEVICE_RTSPINFO_QUERY_REQ"}     -> rtsp_user/rtsp_pass
{"CMD_ID":"DEVICE_PAIR_REQ","ACCOUNT":"<v>","PAIR_KEY":"<v>"}
{"CMD_ID":"P2P_USER_PASSWORD_REQ","P2PSERVERID":"<v>","NAME":"<v>","PASSWORD":"<v>"}
{"CMD_ID":"P2P_USER_PASSWORD_RSP","STATUS":"0" | "-1"}                  -> P2P relay creds
{"CMD_ID":"PING","MSG":"<n>","Keep-Alive":true}  /  {"CMD_ID":"PONG","MSG":"<v>"}
{"CMD_ID":"ERROR_RSP","DESCRIPTION":"unknow command"}
generic: {"CMD_ID":"<cmd>","<key>":<val>,"Keep-Alive":true}
```

Interpretatie richting de 5GenCare-V3-flow (LIKELY): het account authenticeert
(`ACCOUNT`/`PWD` in M2M; in V3 vervangen door `v3_login` + tokens), waarna
`DEVICE_*`-queries de device-list + RTSP-info leveren en `P2P_USER_PASSWORD_REQ`
de relay-/P2P-credentials (`P2PSERVERID`/`NAME`/`PASSWORD`). `PING`/`PONG` met
`MSG` is de keepalive (komt overeen met de Dart-`CMD_PING`/`CMD_PONG`). De V3-set
(`CMD_DLIST`, `CMD_PROFILE_GET`, `CMD_V3_SESSION`) is de opvolger van deze M2M-
commando's in hetzelfde `{"CMD_ID":…,"Keep-Alive":true}`-schema.

## Nog te halen (voor een volledige standalone 5GenCare-client)

1. Exacte serialisatie van `loginInputString` / `sessionInputString` (veldvolgorde,
   scheidingstekens, welke crypto op het wachtwoord/challenge).
2. De volledige `CMD_*`-nummer-tabel en de exacte frame-encoding (`cmd:[ … ] _payload`,
   `startFrame`/`endFrame`, lengteprefix/delimiter).
3. Precieze `CMD_DLIST`-request/response-velden en de bron/expiry van het
   stream-`accessToken`.
4. Heartbeat-interval en sessievernieuwing (`CMD_PING`/`CMD_PONG`, master-token-refresh).

### Hoe dit te halen

- **Nieuwe arm64-route (2026-08-28).** De eerder onderzochte 2.1.17-XAPK
  leverde uitsluitend armeabi-v7a en viel daarom buiten Blutters bereik. Een
  afzonderlijk verkregen Motorola Nursery **2.1.16** splitpakket (versionCode
  4310) bevat daarentegen `config.arm64_v8a.apk` met 64-bits `libapp.so`,
  `libflutter.so` en `libdevconn.so`. Blutters `extract_dart_info.py` herkent
  deze snapshot succesvol als Dart **3.9.2**, snapshot
  `97ff04a728735e6b6b098bdf983faaba`, target `android arm64` met compressed
  pointers. Daarmee is volledige statische Dart-AOT-decompilatie opnieuw een
  concrete route naar `loginV3`, `sessionV3` en `CMD_DLIST`.
- **Blutter-decompile voltooid.** Na activering van WSL2/Virtual Machine
  Platform is Blutter gebouwd en heeft het de Dart-AOT-code succesvol naar
  benoemde assembly per oorspronkelijk Dart-bronpad uitgepakt. Dit bewijst onder
  meer TCP/TLS naar poort `3388`, LF-beeindigde plaintextcommando's,
  `v3_session <userId> <token> <sessionId>`, `v3_dlist\n` en de lokale
  streamtokenafleiding `SHA1(deviceToken + "5GenCare.com")`.
- **Aanbevolen: live capture op een fysiek ARM-Android-toestel.** Installeer de
  **originele** app uit de Play Store (PairIP-licentie geldig, géén tamper, dus
  géén native anti-tamper-kill en géén herstartlus), draai native ARM-`frida-server`
  en hook de Flutter-BoringSSL (`SSL_read`/`SSL_write`) met
  `scripts/arm/frida_flutter_boringssl.js`. Dit levert de byte-exacte `v3_login`/
  `v3_session`/`CMD_DLIST`-frames als plaintext — het enige resterende stuk.
  Waarom dit niet op deze machine kan: de x86-emulator toont de ARM-Dart/BoringSSL-
  modules niet aan x86-Frida, en de reFlutter-resign-omweg stuit op PairIP's native
  anti-tamper (`docs/reflutter-mitm-capture.md`).
- **Als tussenstap** kan een handmatige Ghidra-analyse van de 32-bit
  `libapp.so`-AOT (`analysis/ghidra-projects/vm65.rep`) de `loginV3`/`sessionV3`-
  serialisatie benaderen, maar zonder Dart-symbolen is dat arbeidsintensief.

### Reproduceerbaarheid arm64-pakket

Lokale input blijft git-ignored. Hashes voor identificatie:

| Bestand | SHA-256 |
|---|---|
| `motorola-nursery-2-1-16.xapk` | `b109a5bc8b3af3e831a85adb9b0ddaba71d3249e411eb68f1872394f3ec9ac13` |
| arm64 `libapp.so` | `ec92b2a667ac80a8d056a1c253003a820d327870d9d5c55ff64eb0b887c6a2b2` |
| arm64 `libdevconn.so` | `d28ac08a442f1290b3eb2c13a3ac859477e3412fd07f12d7ea04b3482f0df3b2` |
| arm64 `libflutter.so` | `ee37eb8be8e01a0d840558f75e0a09c4ca135d721c2ca6145c050e4f89785129` |
