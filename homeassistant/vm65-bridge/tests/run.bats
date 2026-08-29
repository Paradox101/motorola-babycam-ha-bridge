#!/usr/bin/env bats

setup() {
  export CALL_LOG="${BATS_TEST_TMPDIR}/calls.log"
  export PATH="${BATS_TEST_TMPDIR}/bin:${PATH}"
  mkdir -p "${BATS_TEST_TMPDIR}/bin"
  # run.sh runs with set -u, so every variable the fakes read must be exported
  # into the child shell. Set unconditionally: a ${VAR:-default} here would let
  # one test's value leak into the next.
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=false
  export TEST_REFRESH_INTERVAL=60
  export TEST_MQTT_SERVICE=false
  export TEST_TEMPERATURE_POLL_INTERVAL=30
  export TEST_CAMERA_REFRESH_INTERVAL=60
  export TEST_ADDON_HOSTNAME=local-vm65-bridge

  bashio::config() {
    case "$1" in
      email) printf 'owner@example.test' ;;
      otp_code|mqtt_username|mqtt_password) printf '' ;;
      control_host) printf 'relay.example.test' ;;
      stream_backend) printf '%s' "${TEST_BACKEND}" ;;
      mqtt_discovery) printf '%s' "${TEST_MQTT_DISCOVERY}" ;;
      mqtt_host) printf 'core-mosquitto' ;;
      mqtt_port) printf '1883' ;;
      mqtt_discovery_prefix) printf 'homeassistant' ;;
      temperature_poll_interval) printf '%s' "${TEST_TEMPERATURE_POLL_INTERVAL}" ;;
      camera_refresh_interval) printf '%s' "${TEST_CAMERA_REFRESH_INTERVAL}" ;;
      stream_host) printf 'homeassistant.local' ;;
      external_stream_port) printf '8556' ;;
      shutdown_timeout) printf '1' ;;
      credential_refresh_interval) printf '%s' "${TEST_REFRESH_INTERVAL}" ;;
    esac
  }
  bashio::services.available() { [ "${TEST_MQTT_SERVICE}" = "true" ]; }
  bashio::services() {
    case "$2" in
      host) printf 'supervised-broker' ;;
      port) printf '8883' ;;
      username) printf 'svc-user' ;;
      password) printf 'svc-pass' ;;
    esac
  }
  # The Supervisor names the add-on on its internal network; Home Assistant
  # fetches snapshots by that name. An empty value stands for a Supervisor that
  # could not be asked.
  bashio::addon.hostname() { [ -n "${TEST_ADDON_HOSTNAME}" ] && printf '%s' "${TEST_ADDON_HOSTNAME}"; }
  bashio::log.info() { :; }
  bashio::log.warning() { :; }
  bashio::log.fatal() { printf 'fatal %s\n' "$*" >> "$CALL_LOG"; }
  bashio::exit.nok() { return 1; }
  export -f bashio::config bashio::log.info bashio::log.warning bashio::log.fatal \
    bashio::exit.nok bashio::services.available bashio::services bashio::addon.hostname

  # The long-running fakes exec their sleep: killing the recorded PID then kills
  # the process that holds the output pipe, instead of orphaning a child that
  # keeps bats waiting long after run.sh has exited.
  printf '#!/bin/sh\nprintf "setup %%s\\n" "$*" >> "$CALL_LOG"\n' > "${BATS_TEST_TMPDIR}/bin/vm65-setup"
  printf '#!/bin/sh\nprintf "bridge %%s\\n" "$*" >> "$CALL_LOG"\nsleep 0.1\nexit 7\n' > "${BATS_TEST_TMPDIR}/bin/vm65-bridge"
  printf '#!/bin/sh\nprintf "go2rtc %%s\\n" "$*" >> "$CALL_LOG"\nexec sleep 30\n' > "${BATS_TEST_TMPDIR}/bin/go2rtc"
  chmod +x "${BATS_TEST_TMPDIR}/bin/"*
}

@test "bundled mode generates config and supervises go2rtc" {
  export TEST_BACKEND=bundled
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-go2rtc-config /data/go2rtc.yaml' "$CALL_LOG"
  grep -q '^go2rtc -config /data/go2rtc.yaml$' "$CALL_LOG"
}

@test "external mode starts RTSP republisher with WebRTC disabled" {
  export TEST_BACKEND=external
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-go2rtc-config /data/go2rtc.yaml' "$CALL_LOG"
  grep -q -- '-go2rtc-webrtc=false' "$CALL_LOG"
  grep -q '^go2rtc -config /data/go2rtc.yaml$' "$CALL_LOG"
}

@test "a failing credential run reports the pairing instruction instead of crash-looping" {
  export TEST_BACKEND=bundled
  printf '#!/bin/sh\nprintf "setup %%s\\n" "$*" >> "$CALL_LOG"\nexit 1\n' > "${BATS_TEST_TMPDIR}/bin/vm65-setup"
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-setup"
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -ne 0 ]
  grep -q 'PAIRING_REQUIRED' "$CALL_LOG"
  # The bridge must never start without credentials.
  ! grep -q '^bridge ' "$CALL_LOG"
}

@test "the refresh interval reloads credentials without restarting go2rtc" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=1
  # A bridge that stays up long enough for the refresh interval to elapse and
  # records the SIGHUP it receives.
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-bridge" <<'EOF'
#!/bin/sh
printf "bridge %s\n" "$*" >> "$CALL_LOG"
sleep 30 &
SLEEP_PID=$!
trap 'printf "bridge-reloaded\n" >> "$CALL_LOG"; kill "$SLEEP_PID" 2>/dev/null; exit 9' HUP
wait "$SLEEP_PID"
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-bridge"

  run timeout 20 bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 9 ]
  grep -q 'bridge-reloaded' "$CALL_LOG"
  # Credentials were rewritten twice: once at startup, once on refresh.
  [ "$(grep -c '^setup ' "$CALL_LOG")" -ge 2 ]
  # go2rtc was started exactly once; a refresh must not drop the streams.
  [ "$(grep -c '^go2rtc ' "$CALL_LOG")" -eq 1 ]
}

@test "MQTT settings come from Home Assistant when the service is offered" {
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=true
  export TEST_MQTT_SERVICE=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-mqtt-host supervised-broker' "$CALL_LOG"
  grep -q -- '-mqtt-port 8883' "$CALL_LOG"
  grep -q -- '-mqtt-username svc-user' "$CALL_LOG"
  # The password travels in the environment, never on the command line.
  ! grep -q 'svc-pass' "$CALL_LOG"
}

@test "first start serves the pairing page instead of failing" {
  export TEST_BACKEND=bundled
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  # The startup run can wait on the pairing page; the periodic refresh must not.
  grep -q -- '-pair-ui 0.0.0.0:8099' "$CALL_LOG"
  # The watchdog has to find something listening while pairing is pending.
  grep -q -- '-status 0.0.0.0:8557' "$CALL_LOG"
  # No code goes out until someone asks for one in the Web UI.
  grep -q -- '-request-code=false' "$CALL_LOG"
}

@test "the periodic refresh never waits on the pairing page" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=1
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-bridge" <<'EOF'
#!/bin/sh
printf "bridge %s\n" "$*" >> "$CALL_LOG"
sleep 30 &
SLEEP_PID=$!
trap 'kill "$SLEEP_PID" 2>/dev/null; exit 9' HUP
wait "$SLEEP_PID"
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-bridge"

  run timeout 20 bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 9 ]
  # Exactly one of the two setup runs may serve the page: the first.
  [ "$(grep -c -- '-pair-ui' "$CALL_LOG")" -eq 1 ]
  [ "$(grep -c '^setup ' "$CALL_LOG")" -ge 2 ]
}

@test "go2rtc is given a WebRTC candidate browsers can reach" {
  export TEST_BACKEND=bundled
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-webrtc-candidate homeassistant.local:8556' "$CALL_LOG"
}

@test "the Web UI listens on the ingress port rather than go2rtc" {
  export TEST_BACKEND=bundled
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-ingress 0.0.0.0:8099' "$CALL_LOG"
}

@test "bundled mode advertises a snapshot URL on the Supervisor network" {
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  # The add-on's own hostname, not stream_host: it always resolves from Home
  # Assistant and needs no published port.
  grep -q -- '-snapshot-url-base http://local-vm65-bridge:8099' "$CALL_LOG"
  grep -q -- '-snapshot-token-file /data/snapshot-token' "$CALL_LOG"
}

@test "snapshots fall back to stream_host when the Supervisor cannot be asked" {
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=true
  export TEST_ADDON_HOSTNAME=
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-snapshot-url-base http://homeassistant.local:8099' "$CALL_LOG"
}

@test "external mode leaves snapshots to the existing media server" {
  export TEST_BACKEND=external
  export TEST_MQTT_DISCOVERY=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  ! grep -q -- '-snapshot-url-base' "$CALL_LOG"
}

@test "MQTT discovery forwards the camera refresh interval" {
  export TEST_MQTT_DISCOVERY=true
  export TEST_CAMERA_REFRESH_INTERVAL=120
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-camera-refresh-interval 120s' "$CALL_LOG"
}

@test "MQTT discovery forwards the configurable temperature interval" {
  export TEST_MQTT_DISCOVERY=true
  export TEST_TEMPERATURE_POLL_INTERVAL=45
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-temperature-poll-interval 45s' "$CALL_LOG"
}
