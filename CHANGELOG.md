# Changelog

## 0.6.0

Security review against Home Assistant's
[app security guidance](https://developers.home-assistant.io/docs/apps/security/),
plus the snapshot fix that review turned up. Existing add-on options stay valid.

**Upgrade note.** The go2rtc API is no longer published on the host. If you
pointed something at `http://<host>:1984` yourself, use the Web UI through
Ingress instead. RTSP (`8555`) and WebRTC (`8556`) are unchanged, so cameras
already added through the Generic Camera integration keep working.

### Fixed

- **Camera snapshots returned 500.** Two causes, both fixed. The add-on image
  had no `ffmpeg`, which go2rtc needs to turn an H264 keyframe into a JPEG, so
  every still-image request failed at the source. And Home Assistant abandons an
  image fetch after ten seconds, which a cold camera cannot meet: the relay
  tunnel, the camera stream and the transcode all have to start first. The
  bridge now serves the still image itself — one fetch per camera at a time,
  continuing after the requester gave up, with a recent frame answering
  immediately and a slightly stale frame preferred over an error.
- **The snapshot URL depended on `stream_host` resolving from Home Assistant**
  and on a published host port. It is now fetched from the add-on by its
  Supervisor hostname, which always resolves and needs no port mapping.

### Security

- **The Web UI is authenticated.** Ingress pointed straight at go2rtc, which has
  no authentication of its own, and go2rtc's port was published on the host as
  well. Anyone on the local network could therefore read `/api/config` — the
  camera access token and RTSP password are in it — and could make go2rtc build
  a stream from any source, `exec:` included, which is command execution inside
  the container. Ingress now points at the bridge, go2rtc binds container
  loopback, and a request only reaches it when it comes from the Supervisor
  network, carries the `X-Remote-User-Id` header the Supervisor attaches to an
  authenticated Ingress session, is a read or a WebRTC/MSE negotiation, and
  names a stream this add-on configured.
- **Only the media ports are published.** The Web UI, snapshots and the health
  endpoint travel over the internal Supervisor network; the watchdog polls the
  container port directly. Both listeners also refuse peers outside that network
  themselves.
- **The published snapshot URL carries a token**, persisted in
  `/data/snapshot-token` so a restart does not break a URL Home Assistant
  already holds. It is never logged.
- An **AppArmor profile** ships with the add-on, and the sidebar panel is
  administrator-only.
- `tools/ci/check_addon.py` now fails the build on a published go2rtc API port,
  a missing AppArmor profile, a missing `ffmpeg`, a watchdog that depends on a
  host port mapping, or any Supervisor API role or host privilege being
  requested.

### Changed

- `ingress_port` is `8099`, served by the bridge rather than by go2rtc.
- `-snapshot-url-base` now names the bridge's own public base URL rather than a
  go2rtc address; `-ingress`, `-ingress-trusted-cidr` and `-snapshot-token-file`
  are new.

## 0.5.2

### Fixed

- Add-on builds now invalidate the cached source-clone layer whenever add-on
  metadata changes. This prevents a new add-on version from running stale
  bridge binaries that do not recognize newly added command-line options.

## 0.5.1

### Added

- A dedicated Home Assistant add-on icon for Motorola Nursery Bridge.

## 0.5.0

### Added

- Home Assistant MQTT temperature sensors for compatible Motorola Nursery
  cameras. The bridge reads the camera's `temperature_reading` capability over
  Motorola's authenticated TLS control channel and publishes Celsius values
  with dedicated availability tracking.
- Add-on option `temperature_poll_interval`, in seconds, with a default of 30
  and supported range of 10 through 3600 seconds.

## 0.4.0

Bug-fix release from a full review of the 0.3.0 tree. Existing add-on options
stay valid; no configuration change is required to upgrade.

### Fixed

- **The relay handshake could block forever.** `magic.Dial` applied the context
  only to the TCP connects, so a relay that accepted the connection and then
  went silent hung the dial indefinitely: `DialTimeout` never applied, the retry
  never ran, and each stuck client leaked a goroutine and two sockets. The whole
  opening sequence now runs under one deadline and aborts on cancellation.
- **The same fault in the 5GenCare client.** `Client.Timeout` covered only the
  TLS connect, not the response read. Because credentials are fetched before the
  bridge starts, a silent host left the add-on "starting" forever with no log
  line and no health port. Every exchange now has a deadline.
- **MQTT Discovery never created a camera entity.** The payload was published as
  an MQTT `camera` with `stream_source`, but that platform requires an image
  `topic` and has no stream URL key, so Home Assistant rejected every payload.
  Cameras are now published as `image` entities fed by a snapshot URL.
- **The Web UI link did not work off the local network.** `webui` built a URL
  from the host port, which is unreachable over a domain name, reverse proxy or
  Nabu Casa. The Web UI is served through Home Assistant Ingress instead.
- **A failed camera bridge was never restarted** and the watchdog could not see
  it: `/healthz` only reported a flag set once at startup. Bridges now restart
  with exponential backoff, and `/healthz` fails when no bridge is serving.
- **`/status` reported zeros** for `active_sessions` and `reconnects_total`.
- **The credential refresh dropped every live stream** every six hours, whether
  or not anything had changed.
- **The add-on built from the pre-rename repository URL**, which worked only
  through GitHub's rename redirect.
- **`make check` always failed** on a clean tree.
- **The repository policy check always exited 1**, so CI had been red on `main`
  since 28 August: `git grep` exits 1 when it matches nothing, and that code
  became the script's exit code even after it printed that the policy passed.
- **The container build never verified the committed checksums**, because only
  `go.mod` was copied before `go mod download`.

### Added

- Home Assistant diagnostics over MQTT: bridge connectivity, active sessions,
  bridge restarts, and a per-camera link sensor, grouped under a bridge device
  with its software version.
- Per-camera availability, so one failed camera shows as unavailable while the
  others keep working.
- Camera snapshot images in bundled mode.
- MQTT broker settings taken from Home Assistant when Mosquitto is installed.
- Credential refresh in place via `SIGHUP`, leaving unchanged cameras streaming.
- `-version` on both commands, and the build version in logs and `/status`.
- `govulncheck` and a `go mod tidy` drift check in CI.

### Changed

- The add-on no longer requests the unused `share:rw` mapping.
- Live video is added once through the Generic Camera integration; Home
  Assistant has no MQTT Discovery path for an RTSP stream. The add-on logs the
  exact URLs at startup.
- The add-on builds from a `release` branch instead of the version tag. A tag
  cannot exist before the commit that names it, so pinning to one broke every
  add-on build and update between the release commit and the tag push. The
  branch always exists, `SOURCE_REF` is no longer bumped per version, and
  publishing to Home Assistant no longer depends on tagging. The build also
  explains itself now instead of failing with a bare "Remote branch not found".

## 0.3.0

Multi-camera support, Home Assistant add-on, MQTT Discovery and health
endpoints. See the repository history for details.
