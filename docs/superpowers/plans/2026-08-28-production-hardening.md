# Motorola Nursery Bridge Production Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize and harden the Motorola Nursery Bridge repository, runtime, Home Assistant add-on, CI, and tag-driven release process without breaking version `0.2.0` installations.

**Architecture:** First establish a strict production/research boundary with a green CI baseline. Then introduce focused configuration, credential, lifecycle, health, and MQTT components behind compatibility-preserving commands and add-on options. Finish with deterministic local add-on builds, current documentation, and validated semantic-version tag releases.

**Tech Stack:** Go 1.27, POSIX shell/bashio, Home Assistant add-on metadata, go2rtc, MQTT 3.1.1/5 client library, Docker Buildx, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-08-28-production-hardening-design.md`

## Global Constraints

- Existing Home Assistant add-on options from version `0.2.0` remain valid.
- Existing repository URL, add-on slug `vm65_bridge`, data paths, command names, MQTT unique IDs, and the single-camera `vm65` stream alias remain valid.
- Home Assistant Supervisor builds the add-on locally; no container image is published.
- Linux `amd64` and `arm64`/Home Assistant `aarch64` are mandatory.
- VM65 is the validated reference model, but production code must not filter devices by VM65 model name.
- Production code cannot import or read files under `research/`.
- Secrets, captures, APKs, archives, native vendor binaries, local toolchains, and session files cannot enter Git, Docker contexts, logs, or release artifacts.
- Every task ends with formatting and focused tests; every phase ends with `go vet ./...` and `go test -race ./...`.
- The final verified tree replaces repository history with one `main` root
  commit; all other branches and existing tags are deleted locally and remotely.

---

### Task 1: Establish the production/research boundary

**Files:**
- Move: `analysis/` to `research/analysis/`
- Move: `scripts/` to `research/scripts/`
- Move: research-only `tools/*.py`, `tools/APKEditor.jar`, and `tools/runtime-protocol-capture/` to `research/tools/`
- Move: historical root reports and inventories to `research/docs/`
- Create: `research/README.md`
- Modify: `.gitignore`
- Modify: `.dockerignore`
- Modify: tracked Markdown links that point to moved material

**Interfaces:**
- Produces: `research/` as the only home for reverse-engineering artifacts; `tools/` reserved for production maintenance.
- Preserves: all Go package paths, add-on paths, command paths, and runtime behavior.

- [ ] **Step 1: Capture the baseline and classify tracked files**

Run:

```powershell
git status --short
git ls-files | Sort-Object
rg -n "analysis/|scripts/|tools/runtime-protocol-capture|REPORT.md|runtime-analysis.md|PROJECT_STATUS.md" -g "*.md"
```

Expected: clean worktree except this plan; every move candidate and inbound documentation link is listed.

- [ ] **Step 2: Move research material with Git history**

Use `git mv` to create these mappings:

```text
analysis/                       -> research/analysis/
scripts/                        -> research/scripts/
tools/runtime-protocol-capture/ -> research/tools/runtime-protocol-capture/
tools/APKEditor.jar             -> research/tools/APKEditor.jar
tools/elf_*.py                  -> research/tools/
REPORT.md                       -> research/docs/REPORT.md
runtime-analysis.md             -> research/docs/runtime-analysis.md
vm65-runtime-result.md          -> research/docs/vm65-runtime-result.md
PROJECT_STATUS.md               -> research/docs/PROJECT_STATUS.md
domains.txt                     -> research/docs/domains.txt
endpoints.txt                   -> research/docs/endpoints.txt
interesting-classes.txt         -> research/docs/interesting-classes.txt
native-libraries.txt            -> research/docs/native-libraries.txt
docs/5gencare-control-static.md -> research/docs/5gencare-control-static.md
docs/arm-5gencare-capture.md    -> research/docs/arm-5gencare-capture.md
docs/control-protocol.md        -> research/docs/control-protocol.md
docs/control-wire-format.md     -> research/docs/control-wire-format.md
docs/magic-web2-protocol.md     -> research/docs/magic-web2-protocol.md
docs/missing-protocol-pieces.md -> research/docs/missing-protocol-pieces.md
docs/native-library-reuse.md    -> research/docs/native-library-reuse.md
docs/reflutter-mitm-capture.md  -> research/docs/reflutter-mitm-capture.md
```

- [ ] **Step 3: Add explicit boundary documentation and ignore rules**

Create `research/README.md` stating that its contents are historical, unsupported, may require private captures, and are excluded from production builds. Extend ignores with:

```gitignore
# Sensitive runtime and research artifacts
**/creds*.json
**/*session*.json
**/*credentials*.json
**/*.pcap
**/*.pcapng
**/*.har
**/*.apk
**/*.xapk
**/*.so
**/*.zip
**/.env*
!**/.env.example
/.tmp-*
```

Mirror the sensitive/generated patterns in `.dockerignore` and explicitly exclude `research/`, `.git/`, `.github/`, `docs/`, and local caches from production Docker context.

- [ ] **Step 4: Repair links and verify no production dependency crosses the boundary**

Run:

```powershell
rg -n "analysis/|scripts/|tools/runtime-protocol-capture|REPORT.md|runtime-analysis.md|PROJECT_STATUS.md" -g "*.md" -g "*.go" -g "*.sh" -g "Dockerfile*"
rg -n "research/" cmd internal homeassistant deploy Dockerfile go.mod
git diff --check
```

Expected: documentation links use their new paths; the second command returns no production dependency.

- [ ] **Step 5: Run the unchanged production baseline and commit**

Run:

```powershell
gofmt -w cmd internal
go vet ./...
go test -race ./...
```

Expected: all commands exit 0. Commit with `chore: separate research from production`.

---

### Task 2: Make formatting, linting, security boundaries, and cross-builds enforceable

**Files:**
- Create: `tools/ci/check-repository.ps1`
- Create: `tools/ci/check-addon.py`
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`
- Modify: `go.mod`

**Interfaces:**
- Produces: deterministic local and GitHub CI commands; repository/add-on validation scripts.
- Consumes: production/research boundary from Task 1.

- [ ] **Step 1: Write failing repository policy fixtures/checks**

`tools/ci/check-repository.ps1` must fail if `git ls-files` contains capture/archive/vendor-binary extensions or credential/session filenames, and if production files reference `research/`. Add a self-test mode that feeds one forbidden path and expects failure.

- [ ] **Step 2: Run the policy self-test and verify it fails before implementation**

Run `pwsh -File tools/ci/check-repository.ps1 -SelfTest`.

Expected: non-zero because checks are not yet implemented.

- [ ] **Step 3: Implement repository and add-on validators**

Implement exact forbidden extensions `.pcap`, `.pcapng`, `.har`, `.apk`, `.xapk`, `.so`, `.zip`; forbidden basenames matching `creds*.json`, `*session*.json`, `*credentials*.json`; and production roots `cmd`, `internal`, `homeassistant`, `deploy`, root Dockerfile. `check-addon.py` parses YAML, requires both `amd64` and `aarch64`, validates unique host ports, verifies option/schema parity, rejects `latest` image tags, and checks the watchdog endpoint.

- [ ] **Step 4: Replace CI with pinned, separated jobs**

Create jobs for:

```text
quality: gofmt -l, go vet, staticcheck, repository policy, add-on YAML
test:    go test -race ./...
build:   CGO_ENABLED=0 GOOS=linux GOARCH={amd64,arm64} for vm65-bridge and vm65-setup
shell:   shellcheck homeassistant/vm65-bridge/run.sh
docker:  buildx build for linux/amd64 and linux/arm64 without push
```

Pin actions to immutable major releases and pin the `staticcheck` version. Set workflow permissions to `contents: read`.

- [ ] **Step 5: Verify and commit**

Run `go vet ./...`, `go test -race ./...`, both cross-builds, both validators, ShellCheck, and `git diff --check`. Commit with `ci: enforce production repository policy`.

---

### Task 3: Introduce typed configuration and secret-safe invocation

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `cmd/vm65-bridge/main.go`
- Modify: `homeassistant/vm65-bridge/run.sh`
- Modify: `homeassistant/vm65-bridge/config.yaml`

**Interfaces:**
- Produces: `config.Load(args []string, lookupEnv func(string) (string, bool)) (Config, error)` and `Config.Validate() error`.
- Produces: `config.Config` fields for listen/status addresses, credential path, stream backend/URL, MQTT settings, and shutdown timeout.
- Preserves: all existing CLI flags and add-on option names.

- [ ] **Step 1: Write table-driven failing validation tests**

Cover invalid backend, empty credential path, invalid ports, MQTT enabled without host/prefix, malformed stream URL, and valid `0.2.0` defaults. Assert errors name the option but never include its secret value.

- [ ] **Step 2: Verify the tests fail**

Run `go test ./internal/config -run TestValidate -v`.

Expected: failure because package/functions do not exist.

- [ ] **Step 3: Implement typed parsing and validation**

Read MQTT password from `VM65_MQTT_PASSWORD` and OTP from `VM65_OTP_CODE`; retain old flags as deprecated fallbacks for standalone users. Implement `Redacted()` for log-safe configuration summaries.

- [ ] **Step 4: Update add-on startup without secrets in argv**

Export secret options only as environment variables. Validate cross-field settings before starting any process. Keep all existing schema keys/defaults; add only optional timeout/log-level settings with defaults.

- [ ] **Step 5: Verify and commit**

Run config tests, `go test -race ./...`, ShellCheck, add-on validator, and `git diff --check`. Commit with `feat: validate runtime and add-on configuration`.

---

### Task 4: Build a multi-camera credential and session provider

**Files:**
- Create: `internal/fivegencare/provider.go`
- Create: `internal/fivegencare/provider_test.go`
- Create: `internal/fivegencare/store.go`
- Create: `internal/fivegencare/store_test.go`
- Modify: `internal/fivegencare/client.go`
- Modify: `cmd/vm65-setup/main.go`
- Modify: `internal/bridge/bridge.go`

**Interfaces:**
- Produces: `Provider.Restore(ctx context.Context) ([]CameraCredentials, error)`.
- Produces: `Provider.Refresh(ctx context.Context, reason RefreshReason) ([]CameraCredentials, error)`.
- Produces: `Store.Load() (State, error)` and `Store.Save(State) error` using atomic rename and mode `0600`.
- Produces: camera records keyed by UDID with device-reported name/model; no VM65 filter.

- [ ] **Step 1: Write failing provider/store tests**

Test candidate fallback after status `-9`, all returned devices retained, missing transport fields rejected per device, atomic save permissions, interrupted save preserving the old file, sanitized errors, and refresh retaining last-known-good credentials on failure.

- [ ] **Step 2: Verify the tests fail**

Run `go test ./internal/fivegencare -run 'TestProvider|TestStore' -v`.

- [ ] **Step 3: Implement the store and provider**

Move pairing/session/device-selection logic out of `cmd/vm65-setup`. Replace `strings.Contains(model, "VM65")` selection with eligibility based on required protocol fields. Return every eligible camera in stable UDID order.

- [ ] **Step 4: Make setup a compatibility wrapper**

`vm65-setup` calls the provider and writes the legacy first-camera credentials file plus the new registry state. Preserve pairing-required exit text used by the add-on.

- [ ] **Step 5: Verify and commit**

Run focused tests, `go test -race ./...`, `go vet ./...`, and a log scan for token/session fields. Commit with `feat: manage Nursery sessions and multiple cameras`.

---

### Task 5: Add supervised lifecycle, dynamic camera registry, and graceful shutdown

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`
- Create: `internal/app/registry.go`
- Create: `internal/app/registry_test.go`
- Modify: `cmd/vm65-bridge/main.go`
- Modify: `internal/bridge/bridge.go`
- Modify: `internal/bridge/reconnect_test.go`

**Interfaces:**
- Produces: `app.Run(ctx context.Context, cfg config.Config, deps Dependencies) error`.
- Produces: camera registry with deterministic stream names and historical `vm65` alias.
- Consumes: provider and configuration interfaces from Tasks 3-4.

- [ ] **Step 1: Write failing lifecycle tests**

Test startup ordering, cancellation, bounded shutdown, one failing camera not stopping others, authorization failure triggering one coalesced refresh, capped jittered backoff, deterministic name collisions, and `vm65` alias continuity.

- [ ] **Step 2: Verify the tests fail**

Run `go test ./internal/app -v`.

- [ ] **Step 3: Implement orchestration and registry**

Use one root context, `sync.WaitGroup`, injected clock/backoff for deterministic tests, and an error classifier separating transient network failures from authorization failures. New relay sessions read the latest atomic credential snapshot.

- [ ] **Step 4: Reduce command main to wiring**

Keep signal handling and exit-code mapping in `cmd/vm65-bridge`; delegate runtime work to `app.Run`. Ensure SIGTERM waits for the configured grace period.

- [ ] **Step 5: Verify and commit**

Run app/bridge tests with race detector, full tests, vet, and cross-compilation. Commit with `feat: supervise multi-camera bridge lifecycle`.

---

### Task 6: Separate liveness, readiness, and sanitized status

**Files:**
- Create: `internal/health/state.go`
- Create: `internal/health/handler.go`
- Create: `internal/health/handler_test.go`
- Modify: `internal/app/app.go`
- Remove after migration: `internal/bridge/health.go`
- Modify: `homeassistant/vm65-bridge/config.yaml`

**Interfaces:**
- Produces: `health.State` component transitions and HTTP handlers for `/healthz`, `/readyz`, `/status`.
- Consumes: lifecycle events from `internal/app`.

- [ ] **Step 1: Write failing endpoint/state tests**

Assert liveness 200 during runtime, readiness 503 before credentials/listeners, readiness 200 when required components are ready, optional MQTT failure not blocking readiness, bundled go2rtc blocking readiness, and status JSON containing no secret fields.

- [ ] **Step 2: Verify failure, implement state machine and handlers, then verify pass**

Run `go test ./internal/health -v` before and after implementation.

- [ ] **Step 3: Wire state transitions and watchdog**

Change Supervisor watchdog to `/healthz`; document `/readyz`. Keep the existing container and host health port mapping compatible.

- [ ] **Step 4: Verify and commit**

Run health/app tests with race detector, full tests, add-on validator, and `git diff --check`. Commit with `feat: expose health readiness and safe status`.

---

### Task 7: Replace MQTT wire code with a reconnecting discovery service

**Files:**
- Rewrite: `internal/mqttdiscovery/publisher.go`
- Rewrite: `internal/mqttdiscovery/publisher_test.go`
- Create: `internal/mqttdiscovery/service.go`
- Create: `internal/mqttdiscovery/service_test.go`
- Modify: `go.mod`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces: `Service.Start(ctx) error`, `Service.Upsert(Camera) error`, `Service.Remove(id string) error`, and `Service.Close(ctx) error`.
- Guarantees: reconnect, keepalive, LWT offline, retained config/availability, republish after reconnect, stable legacy unique IDs.

- [ ] **Step 1: Select and pin a maintained MQTT library**

Use Eclipse Paho Go unless its current Go 1.27 compatibility or reconnect/LWT API fails a focused spike. Record the exact version and license in `docs/security.md`.

- [ ] **Step 2: Write failing broker-fake tests**

Test CONNECT credentials, LWT, reconnect after broker drop, retained rediscovery, multi-camera topics, device-reported model, topic sanitization, legacy ID stability, and non-blocking streaming on MQTT outage.

- [ ] **Step 3: Implement the service and remove raw packet encoding**

Use library-managed keepalive/reconnect. Serialize discovery payloads in typed structs. Never log broker passwords or full payloads containing URLs with credentials.

- [ ] **Step 4: Verify and commit**

Run MQTT/app tests with race detector, full tests, vet, and dependency license/security checks. Commit with `feat: make MQTT discovery reconnect reliably`.

---

### Task 8: Supervise bundled go2rtc and make add-on builds deterministic

**Files:**
- Rewrite: `homeassistant/vm65-bridge/run.sh`
- Modify: `homeassistant/vm65-bridge/Dockerfile`
- Modify: `homeassistant/vm65-bridge/go2rtc.tmpl.yaml`
- Modify: `homeassistant/vm65-bridge/config.yaml`
- Remove: `homeassistant/vm65-bridge/build.yaml`
- Create: `homeassistant/vm65-bridge/tests/run.bats`

**Interfaces:**
- Produces: child supervision with readiness polling, signal forwarding, grace timeout, and non-zero exit on required child failure.
- Preserves: bundled/external modes, ports, options, slug, data paths, and local Supervisor builds.

- [ ] **Step 1: Write failing shell lifecycle tests**

Use command fakes to test bundled startup order, external mode skipping go2rtc, bridge failure terminating go2rtc, go2rtc failure terminating bridge, TERM propagation, timeout escalation, protected rendered files, and no secrets in argv/logs.

- [ ] **Step 2: Verify tests fail and implement supervision**

Run Bats, then implement explicit child PIDs, traps, readiness polling, and bounded cleanup. Do not `exec go2rtc`, because the entrypoint must supervise both children.

- [ ] **Step 3: Pin deterministic build inputs**

Pin the Go Alpine image, Home Assistant base image, and go2rtc image to explicit versions/digests. Build from the checked-out add-on context or an exact source tag/revision without a mutable cached `main` clone. Move supported build arguments into the Dockerfile and remove deprecated `build.yaml`.

- [ ] **Step 4: Verify both architectures and commit**

Run ShellCheck, Bats, add-on validator, and Buildx for `linux/amd64` and `linux/arm64`. Commit with `feat: harden add-on process and build lifecycle`.

---

### Task 9: Replace stale documentation with current production documentation

**Files:**
- Rewrite: `README.md`
- Create: `docs/architecture.md`
- Create: `docs/configuration.md`
- Create: `docs/operations.md`
- Create: `docs/security.md`
- Create: `docs/releases.md`
- Rewrite: `homeassistant/vm65-bridge/README.md`
- Rewrite: `homeassistant/vm65-bridge/DOCS.md`
- Modify: `repository.yaml`

**Interfaces:**
- Produces: one authoritative document per operational topic.
- Preserves: dated protocol research under `research/docs/`.

- [ ] **Step 1: Write current-state docs from verified behavior**

Document Motorola Nursery Bridge as model-independent, list VM65 as tested, explain multi-camera stream naming and the `vm65` alias, pairing/session refresh, bundled/external modes, MQTT discovery, endpoints, ports, backup/recovery, and troubleshooting.

- [ ] **Step 2: Document security and compatibility explicitly**

Include credential file modes, redacted logging, network exposure, research exclusions, dependency licenses, and preserved `0.2.0` interfaces.

- [ ] **Step 3: Check links and stale claims**

Run a Markdown link checker and:

```powershell
rg -n "not reconstructed|out of scope.*refresh|VM65 Bridge|go2rtc:latest" README.md docs homeassistant
```

Expected: only intentional compatibility/history references remain.

- [ ] **Step 4: Verify and commit**

Run repository policy, add-on validator, link checker, full tests, and `git diff --check`. Commit with `docs: describe production Nursery bridge`.

---

### Task 10: Add semantic-version release validation and tag workflow

**Files:**
- Create: `tools/release/validate-version.go`
- Create: `tools/release/validate-version_test.go`
- Create: `.github/workflows/release.yml`
- Modify: `Makefile`
- Modify: `docs/releases.md`
- Modify: `homeassistant/vm65-bridge/config.yaml`

**Interfaces:**
- Produces: `go run ./tools/release/validate-version.go -tag vX.Y.Z`.
- Produces: tag workflow that validates, builds binaries/add-on images without publishing containers, computes checksums, and creates a GitHub Release.

- [ ] **Step 1: Write failing version validation tests**

Test exact tag/manifest match, missing `v`, prerelease handling, malformed SemVer, dirty mismatch, and accepted `v0.3.0` with manifest `0.3.0`.

- [ ] **Step 2: Verify failure and implement validator**

Run `go test ./tools/release -v`, implement YAML version extraction without mutating files, then rerun to pass.

- [ ] **Step 3: Add the release workflow**

Trigger only on `v*.*.*` tags. Set minimal permissions (`contents: write` only for the release job). Reuse or duplicate mandatory CI commands, validate version, build both commands for both architectures, build both add-on architectures with `push: false`, generate SHA-256 checksums, and upload only binaries/checksums.

- [ ] **Step 4: Perform final production verification**

Run:

```powershell
gofmt -w cmd internal tools/release
go vet ./...
go test -race ./...
pwsh -File tools/ci/check-repository.ps1
python tools/ci/check-addon.py
```

Then run ShellCheck, Bats, both cross-builds, both Docker builds, and version validation for the manifest version. Expected: every command exits 0 and `git status --short` contains only intended release changes.

- [ ] **Step 5: Commit and prepare the first hardened release**

Commit with `release: automate validated local-build releases`. Do not push or create a tag without explicit user approval. Recommend `v0.3.0` if compatibility is preserved and all checks pass; use a higher major version if implementation discovers an unavoidable breaking change.

---

### Task 11: Replace Git history with one verified production root

**Files:**
- Preserve: the complete verified working tree from Tasks 1-10
- Rewrite: local and remote Git refs

**Interfaces:**
- Produces: exactly one remote branch, `main`, rooted at one production commit.
- Produces: no old local/remote branches and no old tags.
- Requires: explicit user authorization already recorded for destructive history removal, plus command-level escalation when the remote mutation runs.

- [ ] **Step 1: Re-run the complete release gate**

Run every Task 10 Step 4 command plus both Docker architecture builds. Confirm the worktree contains no untracked sensitive/generated files and every check exits 0. Stop if any check fails.

- [ ] **Step 2: Resolve and display the exact deletion scope**

Run:

```powershell
git remote -v
git branch --format='%(refname:short)'
git branch -r --format='%(refname:short)'
git tag --list
git status --short --branch
```

Verify `origin` is `https://github.com/Paradox101/motorola-vm65-bridge` (or its SSH equivalent) and no second remote is in scope. Record branch and tag names literally; do not use unresolved filesystem paths or broad deletion commands.

- [ ] **Step 3: Create the single-root production commit locally**

Create an orphan branch named `production-root`, stage the already verified tree, and commit it as `chore: establish production-ready Motorola Nursery Bridge`. Rename the current branch to `main`. Verify:

```powershell
git rev-list --count main
git status --short
```

Expected: commit count `1` and an empty worktree.

- [ ] **Step 4: Replace remote main and remove all other remote refs**

Fetch immediately before mutation, then push the new root using an explicit lease against the observed old `origin/main` object ID. Delete each enumerated remote branch other than `main` by literal name. Delete each enumerated remote tag by literal name. Do not use wildcard refspecs.

- [ ] **Step 5: Remove remaining local branches/tags and verify remote state**

Delete each enumerated local branch other than `main` and each local tag by literal name. Fetch with prune, then verify:

```powershell
git rev-list --count main
git branch --format='%(refname:short)'
git branch -r --format='%(refname:short)'
git tag --list
git ls-remote --heads origin
git ls-remote --tags origin
```

Expected: local `main` has exactly one commit; `origin/main` is the only remote head; both local and remote tag lists are empty.

- [ ] **Step 6: Create the first new release only after root verification**

Run the version validator, create the annotated release tag matching the add-on manifest, and push that one new tag only after the single-root checks pass. Report that forks, existing clones, and provider-side garbage-collection retention are outside repository control.
