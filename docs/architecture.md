# Architecture

## Data path

```text
Home Assistant / player
        |
        | WebRTC, MSE, snapshot or republished RTSP
        v
go2rtc (bundled or external)
        |
        | RTSP over TCP
        v
one local bridge listener per camera
        |
        | Magic WEB2 discovery, relay-open and encrypted tunnel
        v
5GenCare relay <-> Motorola Nursery camera
```

`vm65-setup` restores the persisted account session or performs email-OTP
pairing, requests the device list and writes `/data/cameras.json` atomically
with mode `0600`. Eligible devices are selected by required protocol fields,
not by a model-name allowlist.

`vm65-bridge` loads that registry, sorts cameras by UDID and starts an isolated
listener for each camera. Stream names are normalized from the device name and
deduplicated deterministically. An unnamed device becomes `camera-<udid>`. The
first camera is also available as `vm65` for compatibility.

The bridge opens one independent Magic tunnel for every local RTSP connection.
Relay dials have bounded exponential retry. A stopped camera listener changes
readiness but does not terminate other camera listeners.

## Media backends

In `bundled` mode, setup generates a private go2rtc configuration containing
all current cameras. The add-on supervises both processes, and readiness
requires go2rtc to respond successfully.

In `external` mode, go2rtc remains as an internal RTSP republisher so camera
tokens never need to leave the add-on. Its WebRTC listener is disabled. Map
container RTSP port `8555` to a free host port and let the existing media server
consume `rtsp://<host>:<port>/<stream-name>`. MQTT Discovery advertises the
`stream_host` and `external_stream_port` settings.

## Control and state

- `/data/5gencare-session.json`: pairing and account-session state;
- `/data/cameras.json`: all current camera credentials plus internal stream
  metadata;
- `/data/creds.json`: legacy first-camera file retained for upgrades;
- `/data/go2rtc.yaml`: generated only in bundled mode.

Writes are atomic and secret-bearing files are restricted. Every
`credential_refresh_interval` seconds the add-on refreshes the account/device
state and performs a controlled media-service restart.

## Home Assistant discovery

The MQTT service uses QoS 1 retained configuration messages, a retained shared
availability topic and a last will. It reconnects automatically and republishes
every camera after reconnect. Existing safe unique IDs remain stable.

MQTT is optional and therefore does not block media readiness.

## Compatibility surfaces

The repository URL, `vm65_bridge` slug, `vm65-bridge` and `vm65-setup` commands,
data paths, MQTT identifiers and first-camera `vm65` alias are intentionally
preserved from version 0.2.0.
