# Security

This is a review of the add-on against Home Assistant's
[app security guidance](https://developers.home-assistant.io/docs/apps/security/),
and what the implementation does about it.

## Web UI authentication

The Web UI is go2rtc's own page, and go2rtc has no authentication. It used to be
the Ingress target directly, with its port also published on the host, so two
things were true at once: anyone on the local network could read `/api/config`,
which contains the camera access token and the RTSP password, and anyone could
ask go2rtc to build a stream from an arbitrary source — `exec:` is one of its
source schemes, which makes that command execution inside the container.

The Ingress target is now the bridge, and go2rtc binds container loopback. The
bridge proxies a request to it only when all of these hold:

- the peer is on the Supervisor network `172.30.32.0/23`;
- the request carries `X-Remote-User-Id`, the header the Supervisor adds to
  every Ingress request once a Home Assistant session has been authenticated.
  The add-on never sees the session token itself, which is the point of the
  header;
- the request is a read, or one of the two WebRTC/MSE negotiation posts;
- the request names no stream other than the ones this add-on configured, and
  does not address the configuration or process-control endpoints.

Any `X-Remote-User-*` and `X-Forwarded-*` header a client sends is dropped and
replaced before the request is forwarded.

## Add-on rights

The add-on asks for none of the rights that lower an add-on's security rating:
no `host_network`, `host_pid`, `privileged`, `full_access` or `devices`, no
Supervisor `hassio_role`, and no `hassio_api`, `homeassistant_api` or
`auth_api`. It declares `services: mqtt:want` only. `tools/ci/check_addon.py`
fails the build if any of those appears.

`auth_api` is deliberately not used: the add-on holds no local login of its own.
The only credentials it takes are for the vendor account, and the Home Assistant
user backend cannot validate those. Both secret options are typed `password` in
the schema and reach the process through the environment rather than the command
line.

Pairing itself no longer goes through the configuration at all. The pairing page
sits behind the same Ingress gate as the Web UI, so the Supervisor has already
authenticated a Home Assistant user before the form is reachable, and the emailed
code is exchanged for a session in place rather than being stored as an add-on
option. Clearing `otp_code` automatically would have required `hassio_api` and a
Supervisor role, which is exactly the privilege this add-on declines; leaving the
option unused avoids the question. The page posts JSON only, so another site
cannot post to it without a preflight this server never answers, and it renders
from inlined markup under `default-src 'none'` — nothing it needs comes off the
network.

An AppArmor profile ships as `apparmor.txt`; the Supervisor loads it
automatically and matches its name against the add-on slug.

## Network exposure

Only the media ports are published on the host. The Web UI, the snapshot
endpoint and the health endpoint are reached over the internal Supervisor
network, so they need no host port at all, and both listeners refuse a peer
outside that network in the process as well.

The snapshot URL published over MQTT carries a token, kept in
`/data/snapshot-token` so a restart does not invalidate a URL Home Assistant
already holds. It is not logged.

The raw per-camera bridge endpoints stay on container loopback. Bundled and
external mode both expose go2rtc's republished RTSP on container port `8555`,
and **that stream is not authenticated**: anyone who can reach the host on that
port can watch the camera. It is published because Home Assistant's camera
integration and external media servers connect to it. Keep it on a trusted
network, or unset the host mapping in the add-on's Network panel and reach the
video through WebRTC in the Web UI instead.

Pairing state, device tokens, access tokens, RTSP credentials and MQTT password
are secrets. The add-on passes OTP and MQTT password through environment
variables rather than process arguments. Generated state and media config files
are written atomically with private permissions. Logs use redacted summaries
and never intentionally print these values.

Repository policy rejects tracked packet captures, APK/XAPK files, archives,
native vendor libraries and credential/session JSON. Historical research is
kept under `research/` and excluded from production images.

Home Assistant backups containing add-on data must be encrypted and protected
like account credentials. Rotate MQTT credentials and re-pair the Nursery
account if a backup or add-on data directory is exposed.

Dependencies are pinned to release versions in the add-on Dockerfile and Go
module graph. CI runs tests, vet, Staticcheck, the race detector, ShellCheck,
repository policy checks and both supported cross-builds. Report suspected
credential leakage privately before publishing details.
