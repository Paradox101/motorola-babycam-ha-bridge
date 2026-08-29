# Changelog

## 0.10.2

### Fixed

- Republished the add-on release after moving its build source to the fixed
  release branch, so local Home Assistant builds include the entrypoint fix.

## 0.10.1

### Fixed

- The add-on AppArmor profile permitted `/run.sh` to be read but not executed,
  causing S6 to exit immediately with `Permission denied` during startup.

## 0.10.0

A review of the add-on itself — its packaging, its entrypoint and the options
that reach it. The findings and the reasoning behind each fix are in
[docs/review-2026-08-29.md](https://github.com/Paradox101/motorola-babycam-ha-bridge/blob/main/docs/review-2026-08-29.md).

### Fixed

- **Still images and temperature needed MQTT to work at all.** Both were wired
  to the `mqtt_discovery` switch, which is off by default, so in a standard
  installation every camera card asked for a thumbnail that answered 404, "Save
  still" downloaded that 404, and no camera was ever asked for its temperature.
  The Web UI serves its own stills and reads temperature either way now; MQTT
  only decides whether an address for them is published to Home Assistant.
- **The MJPEG fallback could not deliver a picture through Ingress.** It is a
  response that never ends, and the Supervisor buffers ingress responses unless
  the add-on asks it not to. Since MJPEG is the transport that matters exactly
  where WebRTC cannot work — Nabu Casa, a reverse proxy — the last resort was
  missing when it was needed. The add-on now declares `ingress_stream`.
- **A credential refresh could silently break every stream.** The camera access
  token lives in the generated media server configuration, and go2rtc reads
  that file only when it starts. A refresh that rotated the session left go2rtc
  presenting a token the camera no longer accepts, until someone restarted the
  add-on. The media server is now restarted when — and only when — that file
  actually changed.
- **The advertised RTSP port was hard-coded.** Remapping container port 8555 on
  the host produced an RTSP URL in Home Assistant that nothing answered on, with
  no option to correct it.
- **A media server that died took the whole add-on with it**, including a full
  credential round-trip on the way back up, with every camera dark until it
  finished. go2rtc is now restarted on its own with a backoff, the way each
  camera bridge already was.
- **A TLS broker was dialled as if it were plain TCP.** Home Assistant can
  offer a broker on port 8883; the connection then never completed and the
  entities simply never appeared.
- **The add-on did not know its own version**, reporting `devel+<commit>` in the
  Web UI, in the MQTT device and in every bug report.
- A snapshot larger than the 8 MiB cap was truncated and then served as a valid
  picture for the whole cache window.
- A restart request whose content type carried a charset was refused.

### Added

- **Two repairs in the Web UI's diagnostics panel**: restart the media server,
  and refresh credentials now instead of at the next scheduled refresh.
- **A warning when `stream_host` is not an address that reaches this machine.**
  The page compares it with the address your browser actually used, which is
  the first thing to check when live video keeps falling back to MSE or a
  copied RTSP URL resolves nowhere.
- `rtsp_port` and `webrtc_port` options, which mean the same thing in both
  streaming modes. `external_stream_port` meant one in bundled mode and the
  other in external mode; it is deprecated but still wins where it is set, so
  existing configurations keep working.
- `mqtt_tls`, detected automatically for the broker Home Assistant provides.
- Names and descriptions for every option on the configuration page, and this
  changelog on the add-on's own Changelog tab.

### Changed

- The add-on now starts with Home Assistant by default.
- The base image moved from Alpine 3.19, which is end of life, to a pinned
  current one. The ffmpeg installed on top of it is what parses camera data.
- Only the still images each camera actually publishes are fetched at startup.
  Warming the whole allowed stream list opened two relay tunnels per camera
  where one was needed.
- The image the Supervisor builds is now built in CI as well, on every change.

## 0.9.2

Live video now has a working transport, after 0.9.1's on-card reasons turned
out to name three separate faults — one per transport: MSE was refused with a
403 because of an `Origin` check, MJPEG asked for a transcode go2rtc will not
do, and every transport was timed out before the relay tunnel could deliver a
keyframe.

## 0.9.1

The camera picture was covered by a stray badge, WebRTC advertised an address
nothing could reach, and a failed MJPEG attempt left a broken-image icon behind.

## 0.9.0

Live video that plays everywhere: three transports tried in order, with a badge
saying which one is carrying the picture and why the others were refused.

## 0.8.0

Cameras add themselves to Home Assistant, and the add-on got its own camera page
in place of the media server's debugging interface.

## 0.7.0

Pairing moved into the Web UI: no more filling in a code as a configuration
option and restarting twice.

Older entries are in the repository's
[CHANGELOG.md](https://github.com/Paradox101/motorola-babycam-ha-bridge/blob/main/CHANGELOG.md).
