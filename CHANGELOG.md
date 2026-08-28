# Changelog

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
