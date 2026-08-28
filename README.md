# Motorola Nursery Bridge

Motorola Nursery Bridge connects compatible Motorola Nursery/5GenCare cameras
to local RTSP clients and Home Assistant through the reconstructed Magic WEB2
relay protocol. VM65 is the tested reference model; production code does not
filter cameras by model name.

The Home Assistant add-on can provide bundled WebRTC or run go2rtc only as a
small RTSP republisher for an existing media server. MQTT Discovery can register
every detected camera automatically while retaining the historical `vm65`
stream alias and `vm65_bridge` add-on slug.

## Current capabilities

- account pairing by email OTP, with persisted session restore and refresh;
- automatic discovery of all cameras with the required 5GenCare fields;
- one isolated Magic WEB2 bridge per camera;
- optional bundled go2rtc with RTSP, WebRTC, MSE and snapshots;
- optional external-media-server mode;
- retained, reconnect-safe Home Assistant MQTT Discovery;
- liveness, readiness and sanitized status endpoints;
- graceful shutdown and periodic credential refresh;
- Linux amd64 and arm64 builds.

Compatibility with other Motorola models depends on their using the same
5GenCare device fields, Magic WEB2 relay and RTSP path. Only VM65 has been
validated on real hardware so far.

## Home Assistant quick start

1. Add `https://github.com/Paradox101/motorola-vm65-bridge` as a custom add-on
   repository.
2. Install **Motorola Nursery Bridge**.
3. Set `email`, start once, copy the emailed code into `otp_code`, then start
   again.
4. Open the add-on Web UI. The first stream remains `vm65`; additional streams
   use stable names derived from the camera names.
5. Enable `mqtt_discovery` and configure the broker to create camera entities
   automatically, or add `rtsp://<HA-host>:<configured-RTSP-port>/vm65`
   manually.

See [the add-on manual](homeassistant/vm65-bridge/DOCS.md) for bundled and
external mode, port mapping, troubleshooting and upgrades.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/vm65-bridge` | Compatibility command for the production daemon |
| `cmd/vm65-setup` | Account pairing and credential/bootstrap command |
| `internal/app` | Multi-camera registry and lifecycle supervision |
| `internal/bridge` | Local TCP-to-Magic tunnel bridge |
| `internal/fivegencare` | Pairing, sessions, device discovery and secure state |
| `internal/magic` | Magic WEB2 discovery, relay and tunnel protocol |
| `internal/mqttdiscovery` | Reliable Home Assistant MQTT Discovery publisher |
| `homeassistant/vm65-bridge` | Locally built Home Assistant add-on |
| `deploy/go2rtc` | Standalone deployment example |
| `docs` | Architecture, operations, security and release documentation |
| `research` | Historical captures, analysis notes and research-only tooling |

The repository name, binary names and add-on slug retain `vm65` for upgrade
compatibility; the product and runtime are model-independent.

## Development

Go 1.27 is required.

```sh
make check
make dist
```

CI additionally runs the race detector, Staticcheck, ShellCheck, Bats, add-on
metadata policy checks, cross-compilation and non-publishing container builds.
See [architecture](docs/architecture.md), [operations](docs/operations.md),
[security](docs/security.md) and [releases](docs/releases.md).

## Responsible use

Use this project only with cameras and accounts you are authorized to access.
The implementation was derived from the author's own device traffic. Vendor
binaries, captures and credentials are not shipped in production artifacts.
