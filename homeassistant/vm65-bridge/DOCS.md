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
streams for every camera. Open the Web UI on port 1984. The first camera is
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

The recommended route is MQTT Discovery: enable `mqtt_discovery` and configure
the broker fields. Camera entities are retained, republished after MQTT
reconnect and marked unavailable when the add-on disconnects.

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
| `1984/tcp` | go2rtc Web UI/API |
| `8555/tcp` | bundled go2rtc RTSP |
| `8556/tcp,udp` | bundled go2rtc WebRTC |
| `8557/tcp` | `/healthz`, `/readyz`, `/status` |

If a host port is occupied, change the host-side value only. The Supervisor
watchdog uses `/healthz`; `/readyz` additionally checks camera listeners and,
in bundled mode, go2rtc.

## Privacy and backups

The add-on data directory contains account and camera secrets. Protect Home
Assistant backups accordingly. Logs and status output are redacted. See the
repository [operations](../../docs/operations.md) and
[security](../../docs/security.md) guides for recovery and exposure advice.
