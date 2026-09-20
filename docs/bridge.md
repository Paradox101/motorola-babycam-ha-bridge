# Bridge command reference

`cmd/vm65-bridge` is the compatibility command for the Motorola Nursery Bridge
daemon. It exposes one local RTSP-over-TCP listener per camera and carries each
connection through an independent Magic WEB2 relay tunnel.

The command normally reads `/data/cameras.json`, produced by `vm65-setup`:

```sh
vm65-setup -email owner@example.test -registry /data/cameras.json
vm65-bridge \
  -listen 127.0.0.1:8554 \
  -registry /data/cameras.json \
  -status 127.0.0.1:8557
```

For standalone backward compatibility, `-creds` accepts a single-camera JSON
file. `-registry` takes precedence when set.

Useful flags include `-mqtt-host`, `-mqtt-port`, `-mqtt-username`,
`-mqtt-discovery-prefix`, `-stream-url`, `-shutdown-timeout`,
`-go2rtc-required` and `-go2rtc-url`. MQTT passwords are preferably supplied as
`VM65_MQTT_PASSWORD`; setup OTP is preferably supplied as `VM65_OTP_CODE`.

## Session lifetime

Every connection to a bridge listener becomes one relay session. Those sessions
are what `active_sessions` counts, in `/status`, in the MQTT sensor and on the
Web UI card: connections held by the media server, never a headcount of people
watching — a browser never reaches the bridge, and one camera routinely accounts
for several connections (the live stream plus snapshot fetches).

A session ends when either side closes, when the camera sends nothing for 60
seconds, or at shutdown. The idle timeout exists because a relay whose peer
vanished stops sending without closing its socket: nothing errors, both copy
directions block, and the session would otherwise hold two sockets and a
goroutine forever while still counting as active. TCP keepalive on both sockets
catches the same fault from the other side.

A session occupies its slot from the moment it is accepted, so the relay-open
sequence is bounded too. A media server gives up on a source after a few
seconds and reconnects, while a dial with retries and backoff runs far longer;
a session dialling for a client that already left is pure waste. So the bridge
reads the client while it dials — the opening request is replayed toward the
relay once the tunnel is up — and abandons the dial the moment that client
goes away. A total dial budget of 25 seconds bounds the case where the client
does wait. Only then does the concurrency cap apply: at most sixteen sessions
per camera, with a logged warning for each refusal. It is the last-resort guard
against a client reconnecting faster than its sessions end, not a throttle on
the burst a media server produces when it reconnects all of its consumers at
once.

The relay can also simply say no. Its answer to the `app` discovery request
carries a connection number, and the native client accepts a response only when
that number is positive; a non-positive one is the relay reporting that it holds
no registration for the camera — powered off, rebooting or between Wi-Fi
reconnects. The bridge reports that as `relay refused the session`, with the
response line, and treats it as the complete answer it is: the dial is not
retried (over five hours of refusals against the real relay, no retry a second
later ever succeeded), and for ten seconds afterwards new clients wait instead
of asking the relay the same question — a media server with waiting consumers
reconnects the instant a dial fails, and every reconnect would otherwise be
another request to a relay that just said no. A client that leaves during that
wait ends its session without the relay hearing about it; one that stays gets a
real attempt once the cooldown has passed, because the camera may be back. The
cooldown is `Config.RefusalCooldown`; a negative value disables it. The clients
that waited out the same cooldown do not all ask at once: a media server whose
read timeout is shorter than the cooldown reconnects while its previous
connection is still held, so several sessions reach the end of the cooldown
together. One of them asks the relay; a refusal is the others' answer too,
logged as `the relay refused this camera while this session waited`, and a
success sends them on to open relay sessions of their own.

The relay's positive answer comes in two forms. The eight-field one names the
relay hosts, echoes the target port, gives the camera's LAN endpoint and the
connection mode. The four-field one, which the relay sends right after the
camera has registered again, names only the relay hosts; the bridge opens the
session from it with the target port it asked for and logs it as a short
answer in `relay session open`.

Bind raw bridge listeners to loopback unless a trusted external media server
must connect. The raw endpoint adds no authentication beyond the opaque camera
RTSP URL. See [architecture](architecture.md), [operations](operations.md) and
[security](security.md).
