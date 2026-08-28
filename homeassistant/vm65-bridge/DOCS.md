# Motorola Nursery Bridge

This add-on pairs a Motorola Nursery account, finds every compatible camera and
publishes its Magic WEB2 RTSP stream. VM65 is validated on real hardware; other
models work when they expose the same required 5GenCare/Magic fields.

## First start

1. Set `email` to the Motorola Nursery account address.
2. Start the add-on. It sends an email code and stops with `PAIRING_REQUIRED`.
3. Set `otp_code` to that code and start again.
4. After a successful start, clear `otp_code`.

The account session is stored privately and refreshed periodically. A rejected
session triggers a new pairing request.

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

The recommended route is MQTT Discovery: enable `mqtt_discovery`. If the
Mosquitto add-on is installed, the broker address and credentials come from Home
Assistant automatically; fill in the `mqtt_*` options only to point at a
different broker. Camera entities are retained, republished after MQTT reconnect
and marked unavailable when the add-on disconnects. In bundled mode each entity
also gets a snapshot image from go2rtc, so it shows a thumbnail in the
dashboard.

`stream_host` is the address Home Assistant itself uses to reach the streams, so
it must resolve **from Home Assistant**. The default `homeassistant.local` relies
on mDNS, which does not resolve in every setup (some container, VLAN and Docker
network configurations). If camera entities stay black, set `stream_host` to the
fixed IP address of the Home Assistant host.

For manual setup, use a Generic Camera or go2rtc/WebRTC integration with the
republished RTSP URL. Add the resulting camera entity to the dashboard; do not
copy a temporary `/api/camera_proxy/...token=...` URL into dashboard YAML.

## Options

| Option | Meaning |
| --- | --- |
| `email` | Motorola Nursery account email |
| `otp_code` | Temporary email pairing code; clear after pairing |
| `control_host` | Magic relay control host |
| `stream_backend` | `bundled` or `external` |
| `mqtt_discovery` | Publish Home Assistant camera discovery |
| `mqtt_host`, `mqtt_port` | MQTT broker address |
| `mqtt_username`, `mqtt_password` | Optional broker credentials |
| `mqtt_discovery_prefix` | Discovery prefix, normally `homeassistant` |
| `stream_host` | Hostname advertised in MQTT RTSP URLs |
| `external_stream_port` | Host port advertised in external mode |
| `shutdown_timeout` | Graceful child-process shutdown limit in seconds |
| `credential_refresh_interval` | Session/device refresh interval in seconds |

All version 0.2.0 option names remain valid.

## Ports and health

| Container port | Service |
| --- | --- |
| `1984/tcp` | go2rtc API: camera snapshots and direct access (the Web UI uses Ingress) |
| `8555/tcp` | bundled go2rtc RTSP |
| `8556/tcp,udp` | bundled go2rtc WebRTC |
| `8557/tcp` | `/healthz`, `/readyz`, `/status` |

If a host port is occupied, change the host-side value only. The Web UI needs no
published port at all — Ingress reaches go2rtc inside the add-on — but keep
`1984/tcp` mapped in bundled mode so Home Assistant can fetch camera snapshots.

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

## Privacy and backups

The add-on data directory contains account and camera secrets. Protect Home
Assistant backups accordingly. Logs and status output are redacted. See the
repository [operations](../../docs/operations.md) and
[security](../../docs/security.md) guides for recovery and exposure advice.
