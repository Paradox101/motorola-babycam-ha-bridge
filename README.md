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
- Home Assistant MQTT Discovery: per-camera snapshot, link and temperature
  entities (when supported), plus bridge diagnostics, all retained and reconnect-safe;
- Home Assistant Ingress for the Web UI, authenticated against the Home
  Assistant session the Supervisor identifies, with go2rtc kept on container
  loopback;
- cached snapshot images per camera, served by the bridge so a cold camera still
  produces a picture inside the timeout Home Assistant allows;
- automatic restart of a failed camera bridge, with exponential backoff;
- liveness, readiness and sanitized status endpoints with live session counters;
- graceful shutdown, and credential refresh that does not interrupt live streams;
- Linux amd64 and arm64 builds.

Compatibility with other Motorola models depends on their using the same
5GenCare device fields, Magic WEB2 relay and RTSP path. Only VM65 has been
validated on real hardware so far.

## Home Assistant quick start

1. Add `https://github.com/Paradox101/motorola-babycam-ha-bridge` as a custom add-on
   repository.
2. Install **Motorola Nursery Bridge**.
3. Set `email`, start once, copy the emailed code into `otp_code`, then start
   again.
4. Open the add-on Web UI. It is served through Home Assistant Ingress, so it
   works over any address you already use to reach Home Assistant, and it
   requires a signed-in Home Assistant user. The first stream remains `vm65`;
   additional streams use stable names derived from the camera names.
5. Enable `mqtt_discovery` for the camera, temperature and diagnostic entities; the broker
   settings come from Home Assistant when Mosquitto is installed. Live video is
   added once through the Generic Camera integration — Home Assistant has no
   MQTT Discovery path for an RTSP stream — using the URLs the add-on logs on
   start.

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
| `internal/ingress` | Authenticated reverse proxy for the Ingress Web UI |
| `internal/snapshot` | Cached camera still images for Home Assistant |
| `internal/netguard` | Listener restriction to the Supervisor network |
| `internal/buildinfo` | Build version reported by both commands and `/status` |
| `homeassistant/vm65-bridge` | Locally built Home Assistant add-on |
| `deploy/go2rtc` | Standalone deployment example |
| `docs` | Architecture, operations, security and release documentation |
| `CHANGELOG.md` | What changed in each release |
| `research` | Historical captures, analysis notes and research-only tooling |

The repository name, binary names and add-on slug retain `vm65` for upgrade
compatibility; the product and runtime are model-independent.

## Development

Go 1.27 is required.

```sh
make check
make dist
```

CI additionally runs the race detector, Staticcheck, govulncheck, a `go mod
tidy` drift check, ShellCheck, Bats, add-on metadata policy checks,
cross-compilation and non-publishing container builds.
See [architecture](docs/architecture.md), [operations](docs/operations.md),
[security](docs/security.md) and [releases](docs/releases.md).

## Responsible use

Use this project only with cameras and accounts you are authorized to access.
The implementation was derived from the author's own device traffic. Vendor
binaries, captures and credentials are not shipped in production artifacts.
