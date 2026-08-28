# Motorola Nursery Bridge Production Hardening Design

## Objective

Turn the currently working Motorola Nursery bridge and Home Assistant add-on into a
maintainable, production-ready project. Repository cleanup happens first as a
deliberate full reorganization; runtime and release hardening follow only after
the reorganized baseline is green.

The product is model-independent. It discovers cameras from the Motorola
Nursery/5GenCare account and carries each compatible camera through the shared
Magic WEB2/RTSP path. VM65 is the validated reference model, not a hard-coded
product boundary. Other models are reported as compatible only after evidence
shows that they use this protocol path.

## Constraints

- Existing Home Assistant add-on options from version `0.2.0` remain valid.
- The existing repository URL, add-on slug `vm65_bridge`, persisted data paths,
  command names, and MQTT unique IDs remain valid compatibility interfaces.
- The add-on continues to build locally in Home Assistant Supervisor; no GHCR
  runtime images are published.
- Releases are initiated by signed or annotated semantic-version Git tags.
- After the first fully verified hardened release candidate, repository history
  is intentionally replaced by one new `main` root commit. Every other local
  and remote branch and every existing tag is deleted. This one-time destructive
  migration is performed only after all production checks pass.
- Linux `amd64` and `arm64`/Home Assistant `aarch64` remain supported.
- Valuable reverse-engineering material is retained under `research/`.
- Secrets, credentials, session files, packet captures, APKs, extracted vendor
  binaries, and local toolchains are never committed or included in build
  contexts or release artifacts.
- Production code must not depend on files under `research/`.

## Target Repository Layout

```text
cmd/
  vm65-bridge/       compatibility command for the production daemon
  vm65-setup/        compatibility command for account bootstrap
internal/
  app/               runtime orchestration and shutdown
  bridge/            local RTSP-to-Magic relay
  config/            validated runtime configuration
  fivegencare/       account, session, device and refresh protocol
  health/            liveness and readiness state
  magic/             Magic WEB2 wire protocol
  mqttdiscovery/     MQTT session and Home Assistant discovery
  testutil/          shared test-only infrastructure
homeassistant/
  vm65-bridge/       self-contained Home Assistant add-on
deploy/
  go2rtc/            standalone deployment example
docs/
  architecture.md
  configuration.md
  operations.md
  security.md
  releases.md
research/
  analysis/
  captures/
  docs/
  scripts/
  tools/
tools/
  ci/
  release/
.github/workflows/
  ci.yml
  release.yml
```

The first cleanup commit is primarily mechanical: move research files, remove
temporary downloads and proven duplicates, strengthen ignore rules, and repair
links. Functional runtime changes are not mixed into this commit. Research
history remains accessible through Git renames.

## Repository and Security Boundary

`cmd/`, `internal/`, `homeassistant/`, and `deploy/` are production surfaces.
`docs/` documents the current supported system. Historical findings, Frida and
Ghidra work, packet-analysis utilities, captured endpoint inventories, and old
status reports live under `research/` and are explicitly non-production.

The root `.gitignore` and `.dockerignore` use deny-by-default patterns for
credential/session names, captures, archives, APKs, native libraries, analysis
databases, temporary toolchains, and generated output. A CI secret scan checks
tracked files and fails on known credential keys or capture/archive extensions.
Examples contain unmistakably fake values. Add-on build context contains only
the files required to build and run the add-on.

Dead code is removed only when reachability, references, and tests demonstrate
that it is unused. Working but unsupported experiments move to `research/` and
receive a short README explaining their status.

## Runtime Architecture

The long-running bridge process owns orchestration. The existing
`vm65-bridge` binary name is retained for compatibility. It loads validated
configuration, obtains credentials through a credential provider, starts the
bridge and health server, optionally establishes MQTT discovery, and supervises
all components until cancellation. Components expose explicit `Start`, status,
and bounded `Close` behavior; process signals cancel a shared context.

The bridge retries transient relay failures with capped exponential backoff and
jitter. Individual viewer sessions may reconnect without restarting the
process. Persistent authorization failures mark readiness false and trigger a
credential refresh instead of a tight retry loop.

One account may expose multiple compatible cameras. The application maintains a
camera registry keyed by stable UDID and creates an independent bridge endpoint
and MQTT entity for each selected device. URL-safe stream names are derived from
the device name with deterministic collision handling. For an existing
single-camera installation, the historical `vm65` stream alias continues to
point at the first selected compatible camera.

`vm65-setup` remains available for manual/bootstrap use, but shared pairing,
session restore, refresh, device selection, and atomic persistence logic moves
into `internal/fivegencare` and is callable by the long-running application.
Credentials are written with mode `0600` using a temporary file plus atomic
rename. A failed refresh keeps the last known valid credentials and reports a
sanitized error.

## Credential and Session Lifecycle

At startup the provider restores persisted session candidates and validates the
full device list. A device is eligible when it supplies the identifiers and
credentials required by the Magic WEB2/RTSP transport; model-name matching is
informational and is not used as a VM65-only filter. When a session candidate is
rejected, the provider tries remaining candidates. If no
session is usable, the add-on exposes a pairing-required state with actionable
logging and exits cleanly only when user input is required.

During operation, refresh is triggered by expiry metadata when available and by
classified authorization failures otherwise. Refresh attempts use bounded
exponential backoff. Successful refresh atomically updates persisted state and
the credentials used for new relay sessions. Existing relay sessions are not
forcibly interrupted unless their credentials have already failed.

Logs may contain hostnames, status codes, retry counts, device counts, and
redacted stable identifiers. They must never contain passwords, OTP values,
access tokens, device tokens, full session identifiers, MQTT credentials, or
serialized server responses containing those values.

## Health and Readiness

The HTTP status service exposes separate endpoints:

- `/healthz`: process liveness; returns 200 while the supervisor loop runs.
- `/readyz`: returns 200 only when credentials are usable, the bridge listener
  is bound, and all enabled required integrations are ready; otherwise 503.
- `/status`: sanitized diagnostic JSON including component state, uptime,
  reconnect counters, active sessions, and last-error category.

MQTT is optional and therefore does not block stream readiness unless discovery
is enabled. Bundled go2rtc readiness is checked through its local API before the
add-on becomes ready. The Supervisor watchdog targets `/healthz`; operational
checks can use `/readyz`.

## go2rtc Lifecycle

Bundled and external modes preserve their `0.2.0` option names and defaults.
In bundled mode, the entrypoint renders a protected configuration file, starts
the bridge, starts go2rtc, waits for both readiness conditions, forwards TERM
and INT, and terminates the remaining process if either required child exits.
Shutdown has a fixed grace period followed by forced termination.

External mode does not start or require go2rtc. It exposes the raw bridge port
and publishes the configured external RTSP URL. Port declarations remain
backward compatible while optional services are disabled internally.

The go2rtc version is pinned to an explicit tested release rather than
`latest`. A release check reports when the pin and documentation disagree.

## MQTT Discovery

MQTT uses a maintained MQTT client library rather than a partial wire
implementation. The client supports connection timeouts, authentication,
keepalive, automatic reconnect, Last Will (`offline`), retained discovery and
availability messages, and republishing after reconnect.

Discovery is emitted per compatible camera and uses a stable unique ID derived
from its UDID, a sanitized topic-safe object ID, the device-reported name and
model, and its configured stream URL. No `VM65` model string is hard-coded.
Config or device-list changes republish
the retained discovery document. Clean shutdown publishes `offline`; an
unclean disconnect relies on the broker Last Will. MQTT failures are visible in
status and logs but never stop streaming.

## Home Assistant Add-on Configuration

The add-on schema retains every version `0.2.0` key and default. Startup performs
cross-field validation with clear messages, including required email during
pairing, valid host/port combinations, required MQTT fields when discovery is
enabled, and a usable stream host for external mode. Password options are read
without echoing them or placing them on command lines.

Secrets are passed through protected files or environment variables and are
not visible in process listings. Generated files live under `/data`, use mode
`0600`, and are never stored in `/share` by default.

The add-on Dockerfile uses the Supervisor-provided base image for both
architectures, pins Go and go2rtc versions, copies/builds a deterministic source
revision, and avoids deprecated `build.yaml` configuration where supported.

## Tests and Continuous Integration

The mandatory pull-request and branch workflow runs:

1. `gofmt` verification.
2. `go vet ./...`.
3. `go test -race ./...`.
4. Static analysis with pinned `staticcheck`.
5. Linux `amd64` and `arm64` cross-compilation for both commands.
6. ShellCheck for add-on scripts.
7. YAML and Home Assistant add-on metadata validation.
8. Docker build checks for both supported architectures.
9. Repository-boundary, forbidden-artifact, documentation-link, and secret
   checks.

Tests cover session restore and refresh classification, atomic persistence,
relay retry and shutdown, health/readiness transitions, go2rtc supervision,
MQTT reconnect/discovery/LWT behavior, configuration validation, and redaction.
Network protocol tests use local fakes and do not require real credentials.

## Version and Release Process

The add-on version is the canonical project version. A release utility validates
that `homeassistant/vm65-bridge/config.yaml`, documentation, and tag agree.
Versions follow SemVer. Normal development uses the next unreleased version in
the manifest; a release is initiated with an annotated `vX.Y.Z` tag.

The tag workflow reruns all CI checks, verifies a clean version match, builds
both architecture artifacts and add-on images without publishing containers,
generates checksums, and creates a GitHub Release containing binaries, checksums,
and release notes. Home Assistant continues to clone and build the tagged source
locally from the repository.

## One-time History Reset

After the hardened tree passes the full local and CI-equivalent verification,
the working tree is recreated on an orphan branch and committed as a single new
root commit. That root becomes `main` via `--force-with-lease`; all other local
branches, remote branches, and old tags are removed. A fresh release tag may be
created only after the new root is visible on the remote.

Before mutation, the procedure records the exact remote URL, current branch and
all local/remote refs in the execution log and verifies that the only remote in
scope is the intended project repository. It does not retain an archive branch
or tag, because the explicit objective is removal of accessible old history.
Git hosting providers may retain unreachable objects temporarily for internal
garbage collection, and external forks or clones cannot be revoked.

## Documentation

The user-facing product name becomes **Motorola Nursery Bridge**. The historical
repository name, add-on slug, binary names, paths, and entity unique IDs remain
unchanged where renaming would break existing installations. The root README
describes only the current supported path, quick start, Home
Assistant installation, security warning, and links to operational docs.
Architecture, configuration, operations/troubleshooting, security, and release
procedures each have one authoritative document. Historical claims and obsolete
blockers move into `research/docs/` and are clearly dated.

## Delivery Order

1. Full repository reorganization and security boundary.
2. Green baseline: formatting, tests, lint, cross-build and CI.
3. Configuration and credential/session lifecycle.
4. Runtime supervision, shutdown, reconnect and health/readiness.
5. Reliable MQTT Discovery and go2rtc lifecycle.
6. Add-on validation and deterministic multi-architecture builds.
7. Current documentation and tag-driven release workflow.
8. One-time verified history reset to a single `main` root commit.

Each stage is independently reviewable and must leave the repository buildable.
No runtime behavior change is hidden inside the initial file-move stage.
