# Bridge command reference

`cmd/vm65-bridge` is the compatibility command for the Motorola Nursery Bridge
daemon. It exposes one local RTSP-over-TCP listener per camera and carries each
connection through an independent Magic WEB2 relay tunnel.

The command normally reads `/data/cameras.json`, produced by `vm65-setup`:

```sh
vm65-setup -email owner@example.test -registry /data/cameras.json
vm65-bridge \
  -listen 127.0.0.1:8554 \
  -registry /data/cameras.json \
  -status 127.0.0.1:8557
```

For standalone backward compatibility, `-creds` accepts a single-camera JSON
file. `-registry` takes precedence when set.

Useful flags include `-mqtt-host`, `-mqtt-port`, `-mqtt-username`,
`-mqtt-discovery-prefix`, `-stream-url`, `-shutdown-timeout`,
`-go2rtc-required` and `-go2rtc-url`. MQTT passwords are preferably supplied as
`VM65_MQTT_PASSWORD`; setup OTP is preferably supplied as `VM65_OTP_CODE`.

Bind raw bridge listeners to loopback unless a trusted external media server
must connect. The raw endpoint adds no authentication beyond the opaque camera
RTSP URL. See [architecture](architecture.md), [operations](operations.md) and
[security](security.md).
