# Control wire format observations

## Bewezen plaintextvorm

Applicatiecommando's zijn ASCII/UTF-8-compatibele woorden, gescheiden door één of meer spaties. Outbound voorbeelden eindigen met LF. Meerdere opdrachten kunnen direct na elkaar op dezelfde TLS-socket worden geschreven.

```text
command arg1 arg2 ...\n
next-command ...\n
```

Velden met zichtbare tekst gebruiken percent-encoding (`VM65%20CONNECT`). Opaque tokens zijn alfanumeriek/URL-safe, maar hun alfabet en maximale lengte zijn niet volledig vastgesteld.

## Responseparser

Handlers tonen responses als lijsten zoals:

```text
[v3_login, -6, 1.moto.5gencare.com]
[v3_dlist, 1, <device record...>]
[ping]
```

Dit bewijst tokenisatie, niet zelfstandig of inbound records uitsluitend door LF begrensd zijn. De gerichte TLS-plaintextcapture in `tools/runtime-protocol-capture/` moet dit bevestigen.

## Framingstatus

| Onderdeel | Status |
|---|---|
| TLS record framing | standaard TLS; niet gelijk aan appframes |
| Outbound appterminator | LF bewezen |
| Inbound appterminator | UNKNOWN |
| Lengteprefix | niet waargenomen, maar nog niet uitgesloten |
| Escaping | percent-encoding voor tekst bewezen; overige escaping UNKNOWN |
| Charset | ASCII-subset bewezen; volledige UTF-8-ondersteuning LIKELY |

## Capturevereiste

De volgende capture moet alleen de normale eigen login/sessionrestore en één `v3_dlist` observeren. Default output bevat geen ruwe hex en redigeert e-mail, OTP en opaque tokens. Indien binaire framing niet uit redacted plaintext blijkt, moet lokaal een versleutelde pcap worden gecombineerd met alleen lengte/timing van de plaintext-hook; secrets worden niet in fixtures opgenomen.

