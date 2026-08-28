# Optional WebRTC backend and MQTT Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional external WebRTC mode and MQTT Discovery while preserving the current bundled go2rtc default.

**Architecture:** `run.sh` selects bundled or external startup from add-on options. The bridge health endpoint becomes the watchdog target in both modes. Pairing stores camera metadata, while a small Go MQTT publisher inside the long-running bridge emits retained discovery and availability messages without logging secrets.

**Tech Stack:** Go standard library, Bashio, Home Assistant add-on schema, MQTT 3.1.1.

**Spec:** `docs/superpowers/specs/2026-08-28-optional-webrtc-mqtt-discovery-design.md`

## Global Constraints

- `bundled` remains the default backend.
- MQTT Discovery remains disabled unless explicitly enabled.
- MQTT credentials and Motorola tokens never appear in logs or discovery payloads.
- Existing `/data` pairing state remains compatible.

---

### Task 1: Define add-on options and health ports

**Files:**
- Modify: `homeassistant/vm65-bridge/config.yaml`
- Modify: `homeassistant/vm65-bridge/run.sh`
- Modify: `homeassistant/vm65-bridge/Dockerfile`

- [ ] Add schema options `stream_backend`, `mqtt_discovery`, `mqtt_host`, `mqtt_port`, `mqtt_username`, `mqtt_password`, and `mqtt_discovery_prefix`, with defaults from the spec.
- [ ] Expose bridge health port `8557`, point `watchdog` to `http://[HOST]:[PORT:8557]/`, and keep bundled ports available for compatibility.
- [ ] Branch startup on `stream_backend`; start go2rtc only for `bundled`, otherwise run the bridge in the foreground.
- [ ] Pass MQTT options to `vm65-setup` through environment variables without echoing secrets.
- [ ] Validate shell syntax with `bash -n homeassistant/vm65-bridge/run.sh` and inspect the rendered schema.
- [ ] Commit: `feat: add optional external stream backend options`.

### Task 2: Persist camera metadata for the bridge

**Files:**
- Modify: `cmd/vm65-setup/main.go`
- Modify: `cmd/vm65-bridge/main.go`
- Test: `internal/bridge/*_test.go`

- [ ] Extend credentials with stable device identifier and display name.
- [ ] Carry the selected VM65 device metadata from setup into `creds.json`.
- [ ] Load and validate the metadata in the bridge without breaking old credential files.
- [ ] Run the existing bridge tests and commit: `feat: persist VM65 discovery metadata`.

### Task 3: Implement MQTT discovery publisher

**Files:**
- Create: `internal/mqttdiscovery/publisher.go`
- Create: `internal/mqttdiscovery/publisher_test.go`

- [ ] Write tests for deterministic topic/payload generation, retained discovery, availability online/offline, and omission of credentials/tokens.
- [ ] Implement a minimal MQTT 3.1.1 publisher using TLS-optional TCP, CONNECT/CONNACK, PUBLISH, and DISCONNECT packets from the standard library.
- [ ] Expose `Publish(ctx, Config, DeviceInfo, StreamURL)` and `Close(ctx)` interfaces; use QoS 0 retained messages.
- [ ] Return actionable errors for DNS, authentication, and broker refusal without including passwords.
- [ ] Run `go test ./internal/mqttdiscovery` and `go vet ./internal/mqttdiscovery`.
- [ ] Commit: `feat: publish VM65 MQTT discovery`.

### Task 4: Integrate discovery with bridge lifecycle

**Files:**
- Modify: `cmd/vm65-bridge/main.go`
- Modify: `internal/mqttdiscovery/publisher.go`

- [ ] Add bridge arguments/environment parsing for MQTT settings.
- [ ] Publish discovery and availability online after the bridge starts; derive a stable unique ID from persisted device metadata and use the configured host/RTSP port.
- [ ] Publish retained availability offline during bridge shutdown.
- [ ] Keep pairing successful when MQTT is disabled or temporarily unavailable, logging only a warning.
- [ ] Run `gofmt -w internal cmd`, `go test ./...`, and `go vet ./...`.
- [ ] Commit: `feat: connect pairing to MQTT discovery`.

### Task 5: Build and integration verification

**Files:**
- Modify: `README.md`
- Modify: `homeassistant/vm65-bridge/config.yaml`

- [ ] Document bundled/external configuration, MQTT credentials, discovery behavior, and RTSP URL `rtsp://<HA-IP>:8556/vm65`.
- [ ] Bump add-on version and verify both schema modes plus add-on image build for amd64/aarch64.
- [ ] Confirm Home Assistant discovers the camera and that external WebRTC can open the RTSP source.
- [ ] Review logs for absence of session tokens, MQTT passwords, and device tokens.
- [ ] Commit: `docs: document external WebRTC and MQTT discovery`.
