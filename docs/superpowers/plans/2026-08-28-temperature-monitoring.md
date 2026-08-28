# Temperature Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish each compatible camera's ambient temperature as a Home Assistant MQTT sensor at a user-configurable interval.

**Architecture:** Pairing persists the 5GenCare device-control endpoint next to each camera's existing ID and token. A new `internal/devicecontrol` package maintains one authenticated TLS connection per camera, discovers `temperature_reading`, polls it, and reports support, availability and readings through a small sink interface implemented by MQTT Discovery.

**Tech Stack:** Go 1.27 standard library TLS/networking, Eclipse Paho MQTT, Home Assistant MQTT Discovery, Bashio add-on configuration, Bats.

**Spec:** `docs/superpowers/specs/2026-08-28-temperature-monitoring-design.md`

## Global Constraints

- Temperature monitoring is active only when MQTT Discovery is enabled.
- `temperature_poll_interval` defaults to `30` seconds and accepts `10` through `3600` seconds.
- Device control uses strict TLS hostname verification on TCP port `2288`.
- Media streaming must continue when temperature monitoring fails.
- Device tokens, account tokens and MQTT passwords never appear in logs or MQTT payloads.
- Unsupported cameras do not retain a Home Assistant temperature entity.

---

### Task 1: Persist and validate the device-control endpoint

**Files:**
- Modify: `internal/fivegencare/provider.go`
- Modify: `internal/fivegencare/provider_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/vm65-bridge/main.go`
- Modify: `cmd/vm65-bridge/main_test.go`

**Interfaces:**
- Produces: `CameraCredentials.DeviceAPIHost string` and `CameraCredentials.DeviceAPIPort int`.
- Produces: `Config.TemperaturePollInterval time.Duration` from `-temperature-poll-interval`.
- Consumes: `State.Session.Domain` as the TLS hostname.

- [ ] **Step 1: Write failing provider and configuration tests**

```go
if cameras[0].DeviceAPIHost != "shard.example" || cameras[0].DeviceAPIPort != 2288 {
    t.Fatalf("device API endpoint = %s:%d", cameras[0].DeviceAPIHost, cameras[0].DeviceAPIPort)
}

cfg, err := Load([]string{"-temperature-poll-interval", "45s"}, nil)
if err != nil || cfg.TemperaturePollInterval != 45*time.Second {
    t.Fatalf("temperature interval = %s, error = %v", cfg.TemperaturePollInterval, err)
}
```

Add literal boundary cases for `9s`, `10s`, `1h` and `1h1s`; the two outside values must be rejected.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/fivegencare ./internal/config ./cmd/vm65-bridge`

Expected: compilation fails because the endpoint and interval fields do not exist.

- [ ] **Step 3: Add the endpoint and interval**

```go
const DeviceAPIPort = 2288

type CameraCredentials struct {
    // existing fields
    DeviceAPIHost string `json:"device_api_host"`
    DeviceAPIPort int    `json:"device_api_port"`
}
```

Set the fields from `state.Session.Domain` and `DeviceAPIPort`. Add a duration flag defaulting to `30*time.Second`, validate the inclusive `10*time.Second` through `time.Hour` range, include it in `Redacted`, and copy both fields through the bridge's credential JSON shape.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run: `go test ./internal/fivegencare ./internal/config ./cmd/vm65-bridge`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/fivegencare internal/config cmd/vm65-bridge
git commit -m "feat: persist temperature control endpoint"
```

### Task 2: Implement the captured device-control protocol

**Files:**
- Create: `internal/devicecontrol/protocol.go`
- Create: `internal/devicecontrol/protocol_test.go`
- Create: `internal/devicecontrol/client.go`
- Create: `internal/devicecontrol/client_test.go`

**Interfaces:**
- Produces: `type Camera struct { ID string; DeviceID uint32; Token, Host string; Port int }`.
- Produces: `type Client` with `Connect(context.Context, Camera) (*Connection, error)`.
- Produces: `(*Connection).SupportsTemperature(context.Context) (bool, error)`, `Temperature(context.Context) (float64, error)` and `Close() error`.

- [ ] **Step 1: Write failing protocol tests from the captured wire format**

```go
func TestParseCapabilitiesFindsTemperature(t *testing.T) {
    line := "caplist 2 temperature_reading r int 0 0 camera.volume rw int 5 0"
    supported, err := ParseTemperatureCapability(line)
    if err != nil || !supported { t.Fatalf("supported=%t err=%v", supported, err) }
}

func TestParseTemperatureConvertsTenths(t *testing.T) {
    got, err := ParseTemperature("get 1 temperature_reading 214")
    if err != nil || got != 21.4 { t.Fatalf("temperature=%v err=%v", got, err) }
}
```

Also reject a non-positive `app` status, a malformed caplist count/field count, a mismatched `get` response, and raw values `0` and `500`.

- [ ] **Step 2: Run protocol tests and verify RED**

Run: `go test ./internal/devicecontrol`

Expected: compilation fails because the package API does not exist.

- [ ] **Step 3: Implement the minimal parsers and command builders**

Implement newline-delimited commands `app <id> <token>`, `caplist`, and `get 1 temperature_reading`. Parse caplist as a count followed by exactly five fields per capability, and convert accepted raw values `1..499` to Celsius by dividing by ten.

- [ ] **Step 4: Run protocol tests and verify GREEN**

Run: `go test ./internal/devicecontrol -run 'Protocol|Parse|Command'`

Expected: PASS.

- [ ] **Step 5: Write a failing authenticated connection test**

Run an in-process TLS server with a test CA trusted through an injected
`*x509.CertPool`. Its certificate is valid for `camera.example`; the server
must observe the literal auth and query lines, reply with an accepted `app`
response, a valid capability list and `get 1 temperature_reading 214`, and the
client must return `21.4`. A second connection using `wrong.example` must fail
hostname verification, proving `InsecureSkipVerify` remains false.

- [ ] **Step 6: Run the client test and verify RED**

Run: `go test ./internal/devicecontrol -run 'Connection|TLS'`

Expected: compilation fails because `Client.Connect` is missing.

- [ ] **Step 7: Implement the connection**

Use `tls.Dialer` with an optional injected root pool and otherwise the system
roots. Apply context-derived deadlines around every write/read, require
successful auth before capability or temperature requests, buffer
newline-delimited responses, and never include the token in returned errors.

- [ ] **Step 8: Run the package tests and verify GREEN**

Run: `go test ./internal/devicecontrol`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/devicecontrol
git commit -m "feat: add temperature control protocol"
```

### Task 3: Publish temperature through MQTT Discovery

**Files:**
- Modify: `internal/mqttdiscovery/service.go`
- Modify: `internal/mqttdiscovery/service_test.go`

**Interfaces:**
- Produces: `SetTemperatureSupported(context.Context, string, bool) error`.
- Produces: `SetTemperatureAvailable(context.Context, string, bool) error`.
- Produces: `PublishTemperature(context.Context, string, float64) error`.

- [ ] **Step 1: Write failing MQTT behavior tests**

Register `bundledCamera()`, mark temperature supported, and assert the literal discovery topic `homeassistant/sensor/camera-a_temperature/config` carries `device_class=temperature`, `state_class=measurement`, `unit_of_measurement=°C`, state topic `motorola-nursery-bridge/camera/camera-a/temperature`, and two availability entries in `all` mode. Publish `21.4` and assert the retained state payload is exactly `21.4`.

Add tests proving unsupported and removed cameras clear the temperature discovery, state and control-availability topics, and MQTT reconnect republishes supported discovery and its last availability.

- [ ] **Step 2: Run MQTT tests and verify RED**

Run: `go test ./internal/mqttdiscovery`

Expected: compilation fails because the temperature methods do not exist.

- [ ] **Step 3: Implement temperature state in the service**

Add maps for support and control availability. Publish the sensor on the existing camera device, use bridge plus `temperature_availability`, retain numeric state, and extend `Remove` and `onConnect` cleanup/republish paths. Reject non-finite and out-of-range values before publishing.

- [ ] **Step 4: Run MQTT tests and verify GREEN**

Run: `go test ./internal/mqttdiscovery`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mqttdiscovery
git commit -m "feat: publish temperature MQTT sensors"
```

### Task 4: Supervise one temperature worker per camera

**Files:**
- Create: `internal/devicecontrol/supervisor.go`
- Create: `internal/devicecontrol/supervisor_test.go`
- Modify: `cmd/vm65-bridge/main.go`
- Modify: `cmd/vm65-bridge/main_test.go`

**Interfaces:**
- Consumes the MQTT methods through:

```go
type Sink interface {
    SetTemperatureSupported(context.Context, string, bool) error
    SetTemperatureAvailable(context.Context, string, bool) error
    PublishTemperature(context.Context, string, float64) error
}
```

- Produces: `NewSupervisor(Config)`, `(*Supervisor).Reconcile([]Camera)` and `(*Supervisor).Close()`.

- [ ] **Step 1: Write failing supervisor tests**

With an in-memory scripted connector, reconcile one camera and verify observable sink state progresses from unavailable to supported/available with `21.4`. Reconcile the identical camera and prove no second worker starts; change its token and prove the old worker is cancelled and replaced; reconcile an empty list and prove support is retired.

Add a test where the first read fails and the next connection succeeds, proving reconnect without terminating the worker. Use short injected retry delays and poll intervals rather than sleeping production durations.

- [ ] **Step 2: Run supervisor tests and verify RED**

Run: `go test ./internal/devicecontrol -run Supervisor`

Expected: compilation fails because `Supervisor` does not exist.

- [ ] **Step 3: Implement worker lifecycle and retry**

Maintain workers by stable camera ID. Each worker marks control unavailable, authenticates, discovers support, polls immediately and then on the configured ticker, and reconnects after an error using a bounded delay. Unsupported workers retire discovery and stop reconnecting until credentials change. Cancellation closes the active connection promptly.

- [ ] **Step 4: Run device-control tests and verify GREEN**

Run: `go test ./internal/devicecontrol`

Expected: PASS without goroutine leaks or race-prone shared state.

- [ ] **Step 5: Write the failing bridge integration test**

Exercise the registry-to-control-camera conversion with a complete registry entry and assert ID, numeric device ID, token, host and port survive. Assert entries missing a host, port or token are skipped without affecting media registry entries.

- [ ] **Step 6: Run bridge tests and verify RED**

Run: `go test ./cmd/vm65-bridge`

Expected: compilation fails because the conversion helper is absent.

- [ ] **Step 7: Wire the supervisor into bridge lifecycle**

Start it only after MQTT Discovery starts, call `Reconcile` initially and after successful `SIGHUP` reloads, and close it on context cancellation. Log camera ID/name and sanitized errors only; never log the token.

- [ ] **Step 8: Run bridge tests and verify GREEN**

Run: `go test ./cmd/vm65-bridge ./internal/devicecontrol`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/devicecontrol cmd/vm65-bridge
git commit -m "feat: supervise camera temperature polling"
```

### Task 5: Expose the add-on option and document the sensor

**Files:**
- Modify: `homeassistant/vm65-bridge/config.yaml`
- Modify: `homeassistant/vm65-bridge/run.sh`
- Modify: `homeassistant/vm65-bridge/tests/run.bats`
- Modify: `homeassistant/vm65-bridge/DOCS.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: bridge flag `-temperature-poll-interval <duration>`.
- Produces: add-on option `temperature_poll_interval: int(10,3600)` defaulting to `30`.

- [ ] **Step 1: Write the failing add-on behavior test**

Extend the Bashio fake with `temperature_poll_interval` returning `45`, enable MQTT Discovery, execute `run.sh`, and assert the recorded bridge command contains `-temperature-poll-interval 45s`. Add a disabled-MQTT case proving the flag is not forwarded.

- [ ] **Step 2: Run Bats and verify RED**

Run: `bats homeassistant/vm65-bridge/tests/run.bats`

Expected: the configurable interval test fails because the flag is absent.

- [ ] **Step 3: Add option, forwarding and documentation**

Add the default/schema entry, read it in `run.sh`, and append the flag only in the MQTT branch. Add Temperature to the Home Assistant entity table, explain the polling interval and availability, update the README, and bump the add-on patch version.

- [ ] **Step 4: Run focused verification and verify GREEN**

Run: `bats homeassistant/vm65-bridge/tests/run.bats`

Run: `bash -n homeassistant/vm65-bridge/run.sh`

Run: `go test ./...`

Expected: all PASS.

- [ ] **Step 5: Run final quality gates**

Run: `gofmt -w internal/devicecontrol internal/fivegencare internal/config internal/mqttdiscovery cmd/vm65-bridge`

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `go build ./cmd/...`

Run: `git diff --check`

Expected: every command exits zero and no secret value appears in test output or generated MQTT fixtures.

- [ ] **Step 6: Commit**

```bash
git add homeassistant/vm65-bridge README.md
git commit -m "feat: configure add-on temperature polling"
```
