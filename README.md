# motorola-vm65-bridge

Reverse-engineering the Motorola VM65 (Nursery app) camera path and rebuilding
its relay transport in Go, with the goal of viewing the camera without the
Android app — through go2rtc and Home Assistant.

The camera streams RTSP over the **Magic WEB2** relay (`vrelay-*.5gen.care`).
This repo reconstructs that transport from static analysis and runtime captures
of a single owned VM65 session, and packages it as a local bridge.

## Status in one paragraph

The Magic WEB2 transport is **fully reconstructed, coded in Go and tested**:
`app` discovery, relay-open, the device-token tunnel crypto and the derived
`magicUuid`. It is validated against the real production relay
(`cmd/tunnelcheck`) and end to end offline (`internal/bridge`). The one piece
**not** reconstructed is the **5GenCare control flow** that mints a fresh,
authorized session (SID, device token, streaming access token) and signals the
camera to attach. That flow runs ARM-side over TLS and could not be captured
from an x86 host. Until it is solved, every downstream component works but the
camera only sends video once valid, out-of-band credentials are supplied.

See [`docs/current-state.md`](docs/current-state.md) for the strict
PROVEN / LIKELY / UNKNOWN breakdown and
[`docs/missing-protocol-pieces.md`](docs/missing-protocol-pieces.md) for the
blocker.

## Layout

| Path | What it is |
| --- | --- |
| `internal/magic` | Reconstructed Magic WEB2 codecs + `Dial` (byte-transparent `net.Conn`) |
| `internal/bridge` | Local TCP → tunnel bridge, one relay session per client; health endpoint |
| `cmd/vm65-bridge` | The bridge daemon (exposes a local RTSP-over-TCP port) |
| `cmd/tunnelcheck` | Research validator against the real relay (needs live creds) |
| `deploy/go2rtc` | go2rtc config + docker-compose fronting the bridge |
| `homeassistant/vm65-bridge` | Home Assistant add-on (bridge + go2rtc) |
| `docs/` | Protocol notes, wire formats, capture write-ups |
| `analysis/`, `REPORT.md` | Static-analysis artifacts and the full report |

## Quick start

```sh
make test          # go test -race ./...
make build         # host binary
make dist          # static linux amd64 + arm64 binaries
```

Run the bridge with a local credentials file (see
[`docs/bridge.md`](docs/bridge.md) and
`deploy/go2rtc/creds.example.json`):

```sh
./vm65-bridge -listen 127.0.0.1:8554 -creds creds.json
```

Then front it with go2rtc for browsers / Home Assistant — see
[`deploy/go2rtc/README.md`](deploy/go2rtc/README.md).

## Scope and ethics

This targets a device the author owns, uses only data captured from the author's
own session, and sends nothing to Motorola/5GenCare during analysis. Credentials
are never committed. Redistribution/licensing of the vendor's native library was
not established; this project reimplements the protocol rather than shipping that
binary.
