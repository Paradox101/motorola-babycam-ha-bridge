#!/usr/bin/env bats

setup() {
  export CALL_LOG="${BATS_TEST_TMPDIR}/calls.log"
  export PATH="${BATS_TEST_TMPDIR}/bin:${PATH}"
  mkdir -p "${BATS_TEST_TMPDIR}/bin"

  bashio::config() {
    case "$1" in
      email) printf 'owner@example.test' ;;
      otp_code|mqtt_username|mqtt_password) printf '' ;;
      control_host) printf 'relay.example.test' ;;
      stream_backend) printf '%s' "${TEST_BACKEND}" ;;
      mqtt_discovery) printf 'false' ;;
      mqtt_host) printf 'broker' ;;
      mqtt_port) printf '1883' ;;
      mqtt_discovery_prefix) printf 'homeassistant' ;;
      stream_host) printf 'homeassistant.local' ;;
      shutdown_timeout) printf '1' ;;
      credential_refresh_interval) printf '60' ;;
    esac
  }
  bashio::log.info() { :; }
  bashio::log.warning() { :; }
  bashio::exit.nok() { return 1; }
  export -f bashio::config bashio::log.info bashio::log.warning bashio::exit.nok

  printf '#!/bin/sh\nprintf "setup %%s\\n" "$*" >> "$CALL_LOG"\n' > "${BATS_TEST_TMPDIR}/bin/vm65-setup"
  printf '#!/bin/sh\nprintf "bridge %%s\\n" "$*" >> "$CALL_LOG"\nsleep 0.1\nexit 7\n' > "${BATS_TEST_TMPDIR}/bin/vm65-bridge"
  printf '#!/bin/sh\nprintf "go2rtc %%s\\n" "$*" >> "$CALL_LOG"\nsleep 30\n' > "${BATS_TEST_TMPDIR}/bin/go2rtc"
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
