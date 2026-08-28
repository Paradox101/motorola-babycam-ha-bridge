# Motorola Nursery Bridge

This add-on pairs a Motorola Nursery account, finds every compatible camera and
publishes its Magic WEB2 RTSP stream. VM65 is validated on real hardware; other
models work when they expose the same required 5GenCare/Magic fields.

## First start

1. Start the add-on. It comes up unpaired and waits.
2. Click **Open Web UI**. The pairing page asks for your Motorola Nursery
   account address and sends a code to it.
3. Enter the code from the email. The cameras start straight away.

There is nothing to fill in beforehand and no second restart. The `email` and
`otp_code` options still work for an unattended setup, but the Web UI needs
neither, and a code entered there is never written to the configuration.

Codes expire. If one is refused as expired, use **Send a new code** on the same
page — the old code is discarded rather than retried.

The account session is stored privately and refreshed periodically. A rejected
session brings the pairing page back on the next start.

## Streaming modes

`stream_backend: bundled` is the default. The add-on starts go2rtc and generates
streams for every camera. Open the Web UI with the **Open Web UI** button: it is
served through Home Assistant Ingress, so it works over whatever address you
already use to reach Home Assistant — a local IP, a domain name, a reverse proxy
or Nabu Casa — and it inherits Home Assistant's own login. The first camera is
always also available as `vm65`; other names are derived from their Nursery app
names. The republished RTSP URL is:

```text
rtsp://<Home-Assistant-host>:<mapped-8555-port>/vm65
```

`stream_backend: external` keeps only go2rtc's RTSP republisher and disables its
WebRTC listener. In the Network section, map container `8555/tcp` to a free host
port, set `external_stream_port` to that host port and point the existing media
server at `rtsp://<stream_host>:<port>/vm65` (or another discovered stream
name). Camera tokens remain inside the add-on. Clear the host mappings for
`8556/tcp` and `8556/udp` if another server already uses those ports.

## Home Assistant

Enable `mqtt_discovery` and the add-on registers itself as a device with
diagnostics, plus one device per camera:

| Entity | Kind | What it tells you |
| --- | --- | --- |
| Connection | binary_sensor | Whether the bridge process is connected |
| Active sessions | sensor | How many stream sessions are open right now |
| Bridge restarts | sensor | How often a camera bridge had to be restarted |
| `<camera>` Link | binary_sensor | Whether that one camera is reachable |
| `<camera>` Snapshot | image | A still frame, in bundled mode |
| `<camera>` Temperature | sensor | Ambient temperature reported by supported cameras |

If the Mosquitto add-on is installed the broker address and credentials come
from Home Assistant automatically; fill in the `mqtt_*` options only to point at
a different broker. Everything is retained and republished after a broker
reconnect, and each camera has its own availability, so one failed camera shows
as unavailable while the others keep working.

### Live video

The add-on's own Web UI plays live video: open it from the sidebar and press
**Watch live** on a camera. That is WebRTC straight from the bundled media
server, so it starts in well under a second and needs no setup at all.

Inside Home Assistant itself, each camera arrives as a **camera entity** through
MQTT Discovery, refreshed every `camera_refresh_interval` seconds. That is
enough for a dashboard tile, a `camera.snapshot` action or an automation, and it
costs nothing to set up.

Home Assistant cannot discover a *stream* over MQTT — its camera platform reads
image bytes from a topic and has no stream URL at all — so live video on a
Home Assistant dashboard still needs one manual step:

1. **Settings > Devices & services > Add integration > Generic Camera**.
2. Leave the still image URL empty; the camera and image entities already cover
   stills.
3. Set the stream source to `rtsp://<stream_host>:<mapped-8555-port>/vm65`
   (use another stream name for the other cameras).
4. Choose `RTSP transport: TCP`.

The add-on log prints the exact stream URLs on start, and the Web UI has a
**Copy RTSP URL** button per camera.

Do not copy a temporary `/api/camera_proxy/...token=...` URL into dashboard
YAML — those tokens expire.

`stream_host` is the address Home Assistant itself uses to reach the RTSP
streams, so it must resolve **from Home Assistant**. Snapshots no longer use it:
they are fetched from the add-on by its Supervisor hostname, which always
resolves. The default `homeassistant.local` relies
on mDNS, which does not resolve in every setup (some container, VLAN and Docker
network configurations). If entities stay black, set `stream_host` to the fixed
IP address of the Home Assistant host.

## Options

| Option | Meaning |
| --- | --- |
| `email` | Motorola Nursery account email; optional, the Web UI asks for it |
| `otp_code` | Legacy unattended pairing code; leave empty and use the Web UI |
| `control_host` | Magic relay control host |
| `stream_backend` | `bundled` or `external` |
| `mqtt_discovery` | Publish Home Assistant camera discovery |
| `mqtt_host`, `mqtt_port` | MQTT broker address |
| `mqtt_username`, `mqtt_password` | Optional broker credentials |
| `mqtt_discovery_prefix` | Discovery prefix, normally `homeassistant` |
| `temperature_poll_interval` | Seconds between temperature readings (10–3600, default 30) |
| `camera_refresh_interval` | Seconds between stills pushed to the Home Assistant camera entity (5–3600, default 60; `0` publishes none and creates no camera entity) |
| `stream_host` | Hostname advertised in MQTT RTSP URLs (not used for snapshots) |
| `external_stream_port` | Host port advertised in external mode |
| `shutdown_timeout` | Graceful child-process shutdown limit in seconds |
| `credential_refresh_interval` | Session/device refresh interval in seconds |

All version 0.2.0 option names remain valid.

## Ports and health

| Container port | Published | Service |
| --- | --- | --- |
| `8099/tcp` | no | Web UI, pairing page and camera snapshots, through Ingress |
| `8555/tcp` | yes | bundled go2rtc RTSP |
| `8556/tcp,udp` | yes | bundled go2rtc WebRTC |
| `8557/tcp` | no | `/healthz`, `/readyz`, `/status` |

Only the media ports are published on the host. Everything Home Assistant itself
uses — the Web UI, the camera snapshots and the watchdog — travels over the
internal Supervisor network, which needs no host port and no name that resolves
on the LAN. If a published port is occupied, change the host-side value only.

go2rtc's own API is no longer reachable from the network at all. It binds
container loopback, and the Web UI reaches it through the add-on, which checks
the Home Assistant user identity the Supervisor attaches to every Ingress
request first. That API is unauthenticated: it returns the generated
configuration — camera access token and RTSP password included — and builds a
stream from any source it is given, `exec:` among them. The add-on's proxy
therefore refuses the configuration endpoints, refuses anything but a read, and
refuses any stream name it did not configure itself.

The Supervisor watchdog uses `/healthz`, which fails when no camera bridge is
serving at all. A single failed camera does not trip it: the add-on restarts
that bridge itself with a backoff, and restarting everything would drop the
cameras that are still streaming. `/readyz` is stricter and additionally checks
that every camera listener is up and, in bundled mode, that go2rtc answers.
`/status` reports live session counts and how often a bridge was restarted.

## Credential refresh

Every `credential_refresh_interval` seconds the add-on fetches fresh camera
credentials and signals the bridge to adopt them. Cameras whose credentials did
not change keep streaming, so a routine refresh no longer interrupts a live
picture. go2rtc is not restarted.

## Pairing

Until the account is paired, the Ingress page is the pairing form rather than
the stream view, and the add-on serves its health endpoint so the Supervisor
watchdog leaves it alone while you read your email. Once pairing succeeds the
add-on continues its normal startup by itself; reload the page and it becomes
the go2rtc Web UI.

The page is reachable only through Ingress: it requires the Home Assistant user
identity the Supervisor attaches to the request, and refuses connections from
outside the Supervisor network. Only administrators see the add-on panel.

## The Web UI

The Web UI is the add-on's own page, not go2rtc's. It lists cameras rather than
stream names, and per camera shows a still, live WebRTC video on demand, whether
the relay tunnel is up, how many people are watching and the temperature when
the camera reports one. Each card can copy the RTSP URL and restart just that
camera's bridge — the repair worth having, because a tunnel that went bad
recovers from it while every other camera keeps streaming.

go2rtc stays behind the page and is reached only for the media endpoints the
player needs. Its own interface and API are no longer served at all.

## Snapshots

Each camera gets a `Snapshot` image entity through MQTT Discovery in bundled
mode. Home Assistant fetches it from the add-on, which fetches a still frame
from go2rtc and caches it.

The add-on serves the image itself rather than pointing Home Assistant at
go2rtc because Home Assistant abandons an image fetch after ten seconds and
renders anything slower as a 500 from its image proxy. Producing a still frame
from cold takes longer than that: the relay tunnel, the camera stream and an
ffmpeg transcode all have to start. So a fetch that runs over keeps running
after the request that started it, a recent frame is served without touching
the camera, and a frame from the last few minutes is preferred over an error.

If the snapshot entity stays unavailable:

- the URL is authorized by a token from `/data/snapshot-token`, and it reaches
  Home Assistant through a retained MQTT message. If the broker lost its
  retained messages, restart the add-on to republish them;
- the entity only exists in bundled mode. In external mode the media server
  owns snapshots;
- the add-on log records the reason go2rtc gave for a failed still frame.

## Privacy and backups

The add-on data directory contains account and camera secrets. Protect Home
Assistant backups accordingly. Logs and status output are redacted. See the
repository [operations](../../docs/operations.md) and
[security](../../docs/security.md) guides for recovery and exposure advice.
