# Motorola Magic Bridge

Expose a Motorola Nursery / 5GenCare camera (the `Magic` P2P `WEB2` relay
transport) as a plain local **RTSP** stream that Home Assistant can consume via
the bundled **go2rtc**, without the Android app in the media path.

This add-on packages the reverse-engineered, test-proven Magic WEB2 transport
(`internal/magic`, `internal/bridge` in the parent repository) behind go2rtc.

## What works and what does not

**Proven and implemented here (the transport):**

- Magic `app`-discovery on the control host (TCP/8800).
- WEB2 relay-open frame on the stream host (TCP/9901).
- The stateful device-token tunnel crypto that makes the relay byte-transparent.

Given valid inputs, the add-on opens the relay session exactly as the app does,
and `cmd/tunnelcheck` in the parent repo has validated this against the **real**
production relay.

**Not implemented — the external blocker (5GenCare authorization):**

- Obtaining/refreshing a fresh `sid`, `device_token` and stream `access_token`
  without the app.
- The 5GenCare-side control flow that **signals the camera to attach** to the
  relay session.

Consequence: even with correct credentials, an independently opened session
holds open on the relay but the **camera only sends media after the 5GenCare
control side has authorized that session**. This control flow runs over TLS on
the ARM side of the app and is not yet reconstructed (see
`docs/missing-protocol-pieces.md`). Until it is, you must supply credentials
captured from an authorized app session, and they are only valid while that
authorization holds.

## Configuration

All values below come from an authorized app session (extracted from app
storage, e.g. via `adb root`). They are secrets — treat them accordingly.

| Option          | Meaning                                                     |
|-----------------|-------------------------------------------------------------|
| `device_id`     | Numeric device id.                                          |
| `sid`           | Camera SID used to derive the `magicUuid`.                  |
| `device_token`  | Opaque device token; also the tunnel crypto key.           |
| `control_host`  | Magic relay control host, e.g. `vrelay-de0.5gen.care`.      |
| `target_port`   | Camera RTSP target port (observed: `6667`).                 |
| `rtsp_user`     | RTSP username in the camera URL (optional).                 |
| `rtsp_password` | RTSP password in the camera URL (optional).                 |
| `rtsp_path`     | RTSP path, default `/owner/streaming`.                      |
| `access_token`  | Stream access token appended as `?accessToken=` (optional). |

## Using the stream

- go2rtc web UI / API: `http://<home-assistant>:1984`
- RTSP: `rtsp://<home-assistant>:8554/motorola`

Add it to Home Assistant with the **go2rtc** or **Generic Camera** integration
pointing at the RTSP URL above.

## Building

The Go bridge is part of the parent Go module at the repository root, so the
image must be built with the **repository root** as the Docker build context:

```sh
docker build -f addon/motorola-magic-bridge/Dockerfile \
  --build-arg BUILD_FROM=ghcr.io/home-assistant/amd64-base:latest \
  --build-arg BUILD_ARCH=amd64 \
  -t motorola-magic-bridge:local .
```

The bridge binary itself is pure Go standard library and cross-compiles for
`amd64`, `aarch64` (arm64) and `armv7` (arm) with `CGO_ENABLED=0`.

## Verifying the media pipeline

You can prove the add-on's transport actually streams real RTP media end to end,
with no camera, credentials or network, using the bundled demo:

```sh
go run ./cmd/streamdemo --frames 25
# => SUCCESS: 25 H.264 RTP media frames streamed intact through the bridge + Magic token tunnel.
```

It wires a real RTSP camera stand-in, a relay simulator that speaks the real
Magic wire codecs (app-discovery, relay-open, device-token crypto), the exact
bridge `cmd/magicbridge` runs, and a real RTSP client. Only the 5GenCare-side
camera authorization is simulated. The same flow is asserted automatically in
`go test ./internal/e2e/`.

## Standalone (without Home Assistant)

The bridge runs on its own:

```sh
go run ./cmd/magicbridge --creds runtime-logs/creds/creds.json --listen 0.0.0.0:8554
# then: ffplay rtsp://<rtsp-user>:<rtsp-pass>@127.0.0.1:8554/owner/streaming?accessToken=<token>
```
