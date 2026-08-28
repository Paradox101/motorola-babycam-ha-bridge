# Optional WebRTC backend and MQTT Discovery

## Goal

Allow the Home Assistant add-on to run without its bundled go2rtc/WebRTC
server and automatically register the VM65 camera in Home Assistant through
the existing Mosquitto broker.

## Configuration

Add-on options:

- `stream_backend`: `bundled` (default) or `external`.
- `mqtt_discovery`: boolean, default `false` for backwards compatibility.
- `mqtt_host`: default `core-mosquitto`.
- `mqtt_port`: default `1883`.
- `mqtt_username` and `mqtt_password`.
- `mqtt_discovery_prefix`: default `homeassistant`.
- `stream_host`: hostname advertised in the discovery RTSP URL, default
  `homeassistant.local`.

In `bundled` mode, the bridge and go2rtc start as today. In `external` mode,
the bridge exposes the VM65 RTSP stream and go2rtc is not started. The stable
source for an external server is `rtsp://<Home-Assistant-IP>:8556/vm65`.

## Runtime and health

The bridge status endpoint is exposed on a dedicated port and is used for the
add-on watchdog, so health checks work in both modes. Legacy go2rtc port
mappings remain published for compatibility, but no go2rtc process listens on
them in `external` mode.

## MQTT Discovery

After pairing and device-list retrieval, the setup command stores the camera
name and stable identifier in `creds.json`. The long-running bridge process
publishes one retained MQTT discovery config under a deterministic VM65 device
identifier. The config includes camera name, unique ID, availability topic,
and the RTSP stream source. Availability is published as `online` after
startup and `offline` on shutdown. Discovery is skipped when `mqtt_discovery`
is false or credentials are incomplete; stream operation itself must still
work.

## Compatibility and safety

Defaults preserve current behavior. MQTT credentials are add-on secrets and
must never appear in logs. Discovery payloads contain no account/session
tokens. Existing pairing state in `/data` remains valid.

## Verification

- Validate add-on schema for both backend values and MQTT options.
- Test run.sh branches for bundled/external startup and clean shutdown.
- Unit-test deterministic discovery topics/payload and redaction.
- Build the amd64 add-on image and verify the camera appears through MQTT.
