# Temperature monitoring

## Goal

Expose the ambient temperature reported by compatible Motorola Nursery
cameras as a Home Assistant MQTT sensor without requiring the mobile app to
be open.

## User-facing configuration

Temperature monitoring is active whenever `mqtt_discovery` is enabled. The
Home Assistant add-on adds `temperature_poll_interval`, expressed in seconds,
with a default of `30` and an accepted range of `10` through `3600`.

No separate temperature enable switch is added. Disabling MQTT Discovery also
disables temperature control connections and polling.

## Device-control protocol

The setup process stores the account session domain alongside each camera's
existing device ID and device token. The long-running bridge connects to that
domain on TCP port `2288` using TLS with normal certificate and hostname
verification. This connection is separate from the Magic media relay.

For every camera, the bridge authenticates with:

```text
app <device-id> <device-token>\n
```

After a successful authentication response it sends `caplist\n`. Only cameras
whose capability list contains `temperature_reading` receive a Home Assistant
temperature entity. The reading request is:

```text
get 1 temperature_reading\n
```

The response value is an integer in tenths of a degree Celsius. For example,
`214` is published as `21.4` degrees Celsius. Values at or below zero and at
or above 500 are treated as out-of-range readings, matching the app's `LL` and
`HH` boundaries, and are not published as numeric states.

## Runtime design

A focused `internal/devicecontrol` package owns protocol formatting, response
parsing, TLS dialing, authentication, capability detection and polling. One
worker runs per registered camera. A worker keeps its connection open between
polls, reconnects with bounded backoff after failures, and also tolerates the
server closing an idle connection when a long poll interval is configured.

The temperature supervisor is independent of media sessions. Registry reloads
replace workers only when their device-control credentials or endpoint change,
add workers for new cameras and stop workers for removed cameras. Shutdown
cancels all workers cleanly.

## MQTT Discovery

Each supported camera gets one retained `sensor` discovery configuration with:

- a deterministic unique ID based on the existing camera ID;
- `device_class: temperature`;
- `state_class: measurement`;
- unit `degrees Celsius` (published to Home Assistant as its Celsius unit);
- a retained numeric state topic;
- a dedicated control-availability topic in addition to bridge availability.

The sensor is marked unavailable before authentication, after connection or
protocol failures, and during shutdown. An unsupported camera has no
temperature discovery entity. If a previously published sensor is later found
unsupported or its camera is removed, its retained discovery configuration is
cleared.

Neither MQTT payloads nor logs contain the device token, account tokens or MQTT
password.

## Error handling

Malformed responses, authentication rejection, timeouts and TLS failures mark
only the affected temperature sensor unavailable. They do not stop media
streaming or the rest of the add-on. Reconnect delays are bounded and reset
after a successful reading. A credential refresh delivered through `SIGHUP`
reconciles temperature workers with the refreshed registry.

## Verification

- Unit-test command construction and parsing with captured protocol shapes.
- Exercise authentication, capability discovery, temperature conversion,
  reconnect and cancellation against an in-process TLS test server.
- Unit-test MQTT discovery, state, availability and retained cleanup.
- Test registry reconciliation and command-line poll-interval validation.
- Test the add-on option, schema bounds and argument forwarding.
- Run the complete Go test suite, vet, shell tests and production builds.
