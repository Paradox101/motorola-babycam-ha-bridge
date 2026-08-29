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
  export TEST_MQTT_TLS=false
  export TEST_MQTT_SERVICE_SSL=false
  export TEST_RTSP_PORT=8555
  export TEST_WEBRTC_PORT=8556
  export TEST_EXTERNAL_STREAM_PORT=0
  # run.sh writes nothing here itself, but the fakes do, and the real /data is
  # the Supervisor's mount.
  export VM65_DATA_DIR="${BATS_TEST_TMPDIR}/data"
  mkdir -p "${VM65_DATA_DIR}"

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
      mqtt_tls) printf '%s' "${TEST_MQTT_TLS}" ;;
      rtsp_port) printf '%s' "${TEST_RTSP_PORT}" ;;
      webrtc_port) printf '%s' "${TEST_WEBRTC_PORT}" ;;
      external_stream_port) printf '%s' "${TEST_EXTERNAL_STREAM_PORT}" ;;
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
      ssl) printf '%s' "${TEST_MQTT_SERVICE_SSL}" ;;
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
  grep -q -- "-go2rtc-config ${VM65_DATA_DIR}/go2rtc.yaml" "$CALL_LOG"
  grep -q "^go2rtc -config ${VM65_DATA_DIR}/go2rtc.yaml$" "$CALL_LOG"
}

@test "external mode starts RTSP republisher with WebRTC disabled" {
  export TEST_BACKEND=external
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- "-go2rtc-config ${VM65_DATA_DIR}/go2rtc.yaml" "$CALL_LOG"
  grep -q -- '-go2rtc-webrtc=false' "$CALL_LOG"
  grep -q "^go2rtc -config ${VM65_DATA_DIR}/go2rtc.yaml$" "$CALL_LOG"
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
  grep -q -- "-snapshot-token-file ${VM65_DATA_DIR}/snapshot-token" "$CALL_LOG"
}

# The Web UI shows a still on every camera card, so the endpoint that serves
# them cannot depend on whether a broker happens to be configured. Only the URL
# published over MQTT does.
@test "still images are served without MQTT discovery" {
  export TEST_BACKEND=bundled
  export TEST_MQTT_DISCOVERY=false
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- "-snapshot-token-file ${VM65_DATA_DIR}/snapshot-token" "$CALL_LOG"
  ! grep -q -- '-snapshot-url-base' "$CALL_LOG"
}

@test "external mode still serves the Web UI its own stills" {
  export TEST_BACKEND=external
  export TEST_MQTT_DISCOVERY=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- "-snapshot-token-file ${VM65_DATA_DIR}/snapshot-token" "$CALL_LOG"
}

@test "both Web UI repairs are offered" {
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-allow-media-restart' "$CALL_LOG"
  grep -q -- '-allow-credential-refresh' "$CALL_LOG"
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

# A bridge that stays up so the loop below it can be exercised. It exits 9 on
# SIGHUP, which is how a synchronous test ends a run at a known point.
stubborn_bridge() {
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-bridge" <<'EOF'
#!/bin/sh
printf "bridge %s\n" "$*" >> "$CALL_LOG"
sleep 30 &
SLEEP_PID=$!
trap 'printf "bridge-reloaded\n" >> "$CALL_LOG"; kill "$SLEEP_PID" 2>/dev/null; exit 9' HUP
wait "$SLEEP_PID"
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-bridge"
}

# A bridge that survives a reload, for the tests that watch what run.sh does
# after one instead of ending there.
patient_bridge() {
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-bridge" <<'EOF'
#!/bin/sh
printf "bridge %s\n" "$*" >> "$CALL_LOG"
trap 'printf "bridge-reloaded\n" >> "$CALL_LOG"' HUP
while :; do sleep 0.2; done
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-bridge"
}

# start_run runs the entrypoint in the background. It is deliberately not
# wrapped in timeout(1): these tests signal the entrypoint, and a wrapper would
# receive the signal instead.
start_run() {
  bash homeassistant/vm65-bridge/run.sh &
  RUN_PID=$!
}

# stop_run ends the entrypoint the way the Supervisor does.
stop_run() {
  kill "${RUN_PID}" 2>/dev/null || true
  wait "${RUN_PID}" 2>/dev/null || true
}

# wait_for blocks until the call log holds count lines matching pattern. The
# log may not exist yet, which is not a failure — it is the first thing being
# waited for.
wait_for() {
  local count="$1" pattern="$2" attempt seen
  for attempt in $(seq 1 200); do
    seen=$(grep -c -- "${pattern}" "$CALL_LOG" 2>/dev/null || true)
    if [ -n "${seen}" ] && [ "${seen}" -ge "${count}" ]; then
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for ${count}x '${pattern}'" >&2
  cat "$CALL_LOG" >&2 2>/dev/null || true
  return 1
}

# The camera access token in the generated configuration is derived from the
# device token, and go2rtc reads that file only when it starts. A refresh that
# rotates the session therefore has to take the media server with it, or go2rtc
# keeps presenting a token the camera no longer accepts.
@test "a refresh that rewrites the media configuration restarts go2rtc" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=1
  patient_bridge
  # Each run writes a different configuration, as a rotated token would.
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-setup" <<'EOF'
#!/bin/sh
printf "setup %s\n" "$*" >> "$CALL_LOG"
date +%s%N > "${VM65_DATA_DIR}/go2rtc.yaml"
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-setup"

  start_run
  wait_for 2 '^go2rtc '
  wait_for 1 'bridge-reloaded'
  stop_run
}

# The counterpart: an unchanged configuration must leave the streams alone.
@test "a refresh that changes nothing leaves go2rtc running" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=1
  patient_bridge
  cat > "${BATS_TEST_TMPDIR}/bin/vm65-setup" <<'EOF'
#!/bin/sh
printf "setup %s\n" "$*" >> "$CALL_LOG"
printf 'streams: {}\n' > "${VM65_DATA_DIR}/go2rtc.yaml"
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/vm65-setup"

  start_run
  # Two refreshes have happened, so a restart would have shown up by now.
  wait_for 3 '^setup '
  [ "$(grep -c '^go2rtc ' "$CALL_LOG")" -eq 1 ]
  stop_run
}

# One failed media server should not cost every camera its picture, plus a full
# credential round-trip on the way back up.
@test "a media server that dies is restarted instead of taking the add-on down" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=30
  patient_bridge
  # go2rtc exits straight away the first time and stays up afterwards.
  cat > "${BATS_TEST_TMPDIR}/bin/go2rtc" <<'EOF'
#!/bin/sh
printf "go2rtc %s\n" "$*" >> "$CALL_LOG"
if [ ! -f "${VM65_DATA_DIR}/go2rtc-crashed" ]; then
  : > "${VM65_DATA_DIR}/go2rtc-crashed"
  exit 3
fi
exec sleep 30
EOF
  chmod +x "${BATS_TEST_TMPDIR}/bin/go2rtc"

  start_run
  wait_for 2 '^go2rtc '
  # The add-on stayed up across the media server failure: the bridge was never
  # restarted and no credential round-trip was paid for it.
  [ "$(grep -c '^bridge ' "$CALL_LOG")" -eq 1 ]
  [ "$(grep -c '^setup ' "$CALL_LOG")" -eq 1 ]
  kill -0 "${RUN_PID}"
  stop_run
}

# The Web UI's refresh button reaches the bridge, which signals this process.
@test "SIGUSR1 refreshes credentials without waiting for the interval" {
  export TEST_BACKEND=bundled
  export TEST_REFRESH_INTERVAL=3000
  patient_bridge

  start_run
  wait_for 1 '^bridge '
  kill -USR1 "${RUN_PID}"
  wait_for 2 '^setup '
  wait_for 1 'bridge-reloaded'
  stop_run
}

@test "a TLS broker offered by Home Assistant is dialled over TLS" {
  export TEST_MQTT_DISCOVERY=true
  export TEST_MQTT_SERVICE=true
  export TEST_MQTT_SERVICE_SSL=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-mqtt-tls' "$CALL_LOG"
}

@test "a plain broker is not dialled over TLS" {
  export TEST_MQTT_DISCOVERY=true
  export TEST_MQTT_SERVICE=true
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  ! grep -q -- '-mqtt-tls' "$CALL_LOG"
}

# The advertised RTSP port used to be hard-coded to the container port, so
# remapping 8555 on the host produced a URL nothing answers on.
@test "the advertised RTSP port follows rtsp_port" {
  export TEST_BACKEND=bundled
  export TEST_RTSP_PORT=18555
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-stream-url rtsp://homeassistant.local:18555/vm65' "$CALL_LOG"
}

@test "the WebRTC candidate follows webrtc_port" {
  export TEST_BACKEND=bundled
  export TEST_WEBRTC_PORT=18556
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-webrtc-candidate homeassistant.local:18556' "$CALL_LOG"
}

# Existing configurations keep working: external_stream_port still means the
# WebRTC host port in bundled mode and the RTSP host port in external mode.
@test "a legacy external_stream_port still wins in bundled mode" {
  export TEST_BACKEND=bundled
  export TEST_EXTERNAL_STREAM_PORT=28556
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-webrtc-candidate homeassistant.local:28556' "$CALL_LOG"
  grep -q -- '-stream-url rtsp://homeassistant.local:8555/vm65' "$CALL_LOG"
}

@test "a legacy external_stream_port still wins in external mode" {
  export TEST_BACKEND=external
  export TEST_EXTERNAL_STREAM_PORT=28555
  run bash homeassistant/vm65-bridge/run.sh
  [ "$status" -eq 7 ]
  grep -q -- '-stream-url rtsp://homeassistant.local:28555/vm65' "$CALL_LOG"
}
