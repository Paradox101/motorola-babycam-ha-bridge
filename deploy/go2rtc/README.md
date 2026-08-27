# go2rtc + vm65-bridge

This stack fronts the Magic WEB2 tunnel (`vm65-bridge`) with
[go2rtc](https://github.com/AlexxIT/go2rtc), which turns the tunnelled
RTSP-over-TCP endpoint into WebRTC / MSE / RTSP / snapshots for browsers and
Home Assistant.

```text
player / Home Assistant
     │  WebRTC / MSE / RTSP / snapshot
     ▼
   go2rtc  ──rtsp/tcp──▶  vm65-bridge  ──Magic WEB2──▶  camera
```

## Usage

1. Copy `creds.example.json` to `creds.json` and fill in the camera's
   `device_id`, `sid`, `device_token` and relay `control_host`. Keep this file
   out of git.
2. In `go2rtc.yaml`, replace `<rtsp-user>`, `<rtsp-password>` and
   `<access-token>` with the values the app supplies to its player. The
   `accessToken` comes from the 5GenCare device/control model, not from RTSP.
3. `docker compose up --build`
4. Open the go2rtc UI at <http://localhost:1984>; the stream is named `vm65`.

## Home Assistant

Point the HA go2rtc/WebRTC Camera integration at this go2rtc instance, or use
the bundled add-on in `../../homeassistant/vm65-bridge`, which packages the
bridge and go2rtc together.

## Limitation

The tunnel transport is complete and independently tested, but the camera only
attaches and sends video once a valid **5GenCare-authorized** session exists
(fresh SID / device token / streaming accessToken). Producing those credentials
without the Android app is unresolved; see
[`../../docs/missing-protocol-pieces.md`](../../docs/missing-protocol-pieces.md).
Until then this stack establishes the relay session and waits for camera data.
