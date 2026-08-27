# vm65-bridge

`cmd/vm65-bridge` exposes a Motorola VM65 camera as a plain, local
RTSP-over-TCP endpoint. Every byte a local RTSP client sends is carried
transparently to the camera through the reconstructed Magic WEB2 relay
(`internal/magic`), and every byte the camera sends comes back the same way.

This is exactly the role the Android app plays with its dynamic listen port
(16667 in the measured session): the player connects to
`127.0.0.1:<port>` and speaks RTSP as if the camera were on the LAN.

```text
RTSP player / go2rtc / Home Assistant
        │  rtsp://…@127.0.0.1:8554/owner/streaming?accessToken=…
        ▼
   vm65-bridge  (internal/bridge)
        │  one Magic WEB2 relay session per client connection
        ▼
   Magic control host  →  relay stream host (:9901)  →  camera
```

## Scope and the one missing piece

The bridge performs **only** the proven Magic WEB2 transport: `app` discovery,
relay-open and the device-token tunnel crypto. It deliberately does **not**
perform the **5GenCare control flow** — the part that mints a fresh SID, device
token and stream access token and signals the camera to attach. That flow runs
ARM-side over TLS and is not reconstructable from an x86 host; see
`docs/missing-protocol-pieces.md` and `docs/current-state.md`.

Consequence: the bridge takes the 5GenCare outputs as **inputs** (a credentials
file) and does not fabricate or refresh them. Against the real relay it will
open the session, exactly as `cmd/tunnelcheck` proves, but the camera only
attaches once a valid 5GenCare-authorized session exists. The bridge's local
RTSP path itself is validated end to end, offline, with no Android in the loop
(`internal/bridge` end-to-end test).

## Credentials

The bridge reads the same local JSON file shape as `cmd/tunnelcheck`. This file
is git-ignored and must never be committed.

```json
{
  "device_id":    123456,
  "sid":          "…",
  "device_token": "…",
  "control_host": "vrelay-de0.5gen.care",
  "control_port": 8800,
  "target_port":  6667
}
```

`control_port` (default 8800) and `target_port` (default 6667) may be omitted.

## Running

```sh
go build ./cmd/vm65-bridge
./vm65-bridge -listen 127.0.0.1:8554 -creds runtime-logs/creds/creds.json
```

Bind to loopback: the tunnel carries an unauthenticated RTSP stream. Use `-v`
for debug logging. The process shuts down cleanly on SIGINT/SIGTERM, closing
the listener and every live tunnel.

## go2rtc integration

The bridge endpoint is an ordinary interleaved (TCP) RTSP source. Point go2rtc
at it with the RTSP URL the app uses, forcing TCP transport:

```yaml
streams:
  vm65:
    - "rtsp://<rtsp-user>:<rtsp-password>@127.0.0.1:8554/owner/streaming?accessToken=<temporary-token>#rtsp/tcp"
```

The RTSP user/password and `accessToken` are the same values the app supplies to
its player; the `accessToken` comes from the 5GenCare control/device model, not
from RTSP. Home Assistant then consumes the go2rtc stream as usual. Both the
go2rtc recipe and the Home Assistant add-on remain gated on obtaining a valid
5GenCare-authorized session.
