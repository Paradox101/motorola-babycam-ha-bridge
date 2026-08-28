# Research archive

This directory contains historical reverse-engineering notes, scripts, static
analysis helpers, and capture tooling used while reconstructing the Motorola
Nursery/5GenCare and Magic WEB2 protocols.

It is not part of the supported runtime or Home Assistant add-on. Nothing under
`cmd/`, `internal/`, `homeassistant/`, or `deploy/` may depend on this directory.
Scripts here may require private packet captures, vendor applications, rooted or
emulated Android devices, and third-party tools that are intentionally not
committed.

Never add credentials, session state, OTP codes, access/device tokens, packet
captures, APK/XAPK archives, extracted native libraries, or analysis databases
to Git. The repository ignore and CI policies enforce these exclusions.

Historical documents can describe obsolete limitations. Current supported
behavior is documented in the root `README.md` and `docs/`.
