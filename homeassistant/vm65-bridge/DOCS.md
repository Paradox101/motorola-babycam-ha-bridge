# Motorola VM65 Bridge add-on

Tunnels a Motorola VM65 baby monitor to Home Assistant. The add-on runs two
processes:

- **vm65-bridge** — opens a Magic WEB2 relay session and exposes the camera as
  a local RTSP-over-TCP endpoint on `127.0.0.1:8554`.
- **go2rtc** — consumes that endpoint and republishes it as WebRTC / MSE / RTSP
  / snapshots. Its UI is on port `1984`.

```text
Home Assistant ──▶ go2rtc ──rtsp/tcp──▶ vm65-bridge ──Magic WEB2──▶ camera
```

## Requirements and current limitation

The Magic WEB2 transport is fully reconstructed and independently tested. The
one piece that is **not** reconstructed is the 5GenCare control flow that mints
a fresh session (SID, device token, streaming access token) and signals the
camera to attach. That flow runs ARM-side over TLS and could not be captured
from an x86 host. See the repository's `docs/missing-protocol-pieces.md`.

Consequently this add-on needs valid, out-of-band credentials in its options,
and video only flows while those represent an authorized session. Without them
the bridge opens the relay session and waits.

## Options

| Option | Description |
| --- | --- |
| `device_id` | Numeric device id from device discovery |
| `sid` | Camera SID |
| `device_token` | Opaque device token (also the tunnel crypto key) |
| `control_host` | Magic relay control host, e.g. `vrelay-de0.5gen.care` |
| `control_port` | Magic control port (default 8800) |
| `target_port` | Camera target port (default 6667) |
| `rtsp_user` / `rtsp_password` | RTSP credentials the app uses |
| `access_token` | Streaming accessToken (from the 5GenCare device model) |

## Using the stream

After starting the add-on, open the go2rtc UI (Web UI button, port 1984). The
stream is named `vm65`. Add it to Home Assistant with the WebRTC Camera / go2rtc
integration, or as a generic camera pointed at
`rtsp://<ha-host>:8555/vm65`.

## Building

The image builds `vm65-bridge` from source (`SOURCE_REF`, default `main`) and
copies `go2rtc` from its official image. If the bridge code is not yet on
`main`, build with `--build-arg SOURCE_REF=<branch>`.
