# Security

The raw per-camera bridge endpoints stay on container loopback. External mode
exposes go2rtc's republished RTSP on container port `8555`; it should still be
limited to a trusted network because it is intended for local media clients.

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
