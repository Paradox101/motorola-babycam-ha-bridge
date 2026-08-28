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
  bashio::log.info() { :; }
  bashio::log.warning() { :; }
  bashio::log.fatal() { printf 'fatal %s\n' "$*" >> "$CALL_LOG"; }
  bashio::exit.nok() { return 1; }
  export -f bashio::config bashio::log.info bashio::log.warning bashio::log.fatal \
    bashio::exit.nok bashio::services.available bashio::services

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

@test "bundled mode advertises a snapshot URL for Home Assistant" {
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-snapshot-url-base http://homeassistant.local:1984' "$CALL_LOG"
}

@test "external mode leaves snapshots to the existing media server" {
  export TEST_BACKEND=external
  export TEST_MQTT_DISCOVERY=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  ! grep -q -- '-snapshot-url-base' "$CALL_LOG"
}
