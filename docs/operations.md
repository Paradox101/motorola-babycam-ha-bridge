# Operations

## Health endpoints

The add-on exposes container port `8557` (host port `8558` by default):

- `/healthz`: process liveness, used by the Supervisor watchdog;
- `/readyz`: credentials, all camera listeners and required go2rtc are ready;
- `/status`: sanitized JSON counters and categorized last-error state.

MQTT connectivity is reported but does not make the camera stream unavailable.

## Pairing and recovery

On first start, set the account email and leave `otp_code` empty. The add-on
requests a code and exits with `PAIRING_REQUIRED`. Enter the code and restart.
After successful pairing, clear `otp_code`; the persisted session is used.

If the service reports a rejected session, it clears only the invalid session
and requests a new email code. Do not delete all add-on data unless pairing
state itself is damaged.

The service refreshes credentials every six hours by default. Configure
`credential_refresh_interval` between 300 and 86400 seconds. Refresh performs a
short controlled restart of the media processes.

## Backup and restore

Home Assistant add-on backups include the add-on data directory. Treat backups
as secrets because they contain account sessions and camera tokens. Restoring
the add-on data preserves pairing; if Motorola rejects the restored session,
complete email pairing again.

## Troubleshooting

`PAIRING_REQUIRED` is an expected state, not a crash. Enter the new email code
and restart.

If `/healthz` works but `/readyz` returns 503, inspect `/status` and the add-on
log. Typical causes are a camera listener failing to bind or bundled go2rtc not
starting.

If Home Assistant reports a port already in use, change the left/host-side port
in the add-on Network section. Container ports must remain unchanged. In
external mode, map container `8555/tcp` to a free host port and set
`external_stream_port` to that same host port. WebRTC port `8556` is unused in
that mode and its host mapping may be cleared.

If video works in the go2rtc UI but a dashboard camera causes authentication
errors, add the camera through its entity or MQTT Discovery. Do not store a
handwritten `/api/camera_proxy/...?...token=` URL in a dashboard.

For MQTT problems, verify broker host, port, username, password and discovery
prefix. Reconnecting the broker should republish discovery automatically.

## Updating

Refresh the custom add-on repository, install the offered version and review
the build log. The add-on is built locally by Supervisor for amd64 or aarch64;
it does not pull a project runtime image from GHCR.
