#!/usr/bin/with-contenv bashio
# shellcheck shell=bash
#
# Add-on entrypoint: validate options, refresh the Nursery session and
# supervise the bridge plus the optional bundled go2rtc process.
set -euo pipefail

CREDS=/data/creds.json
REGISTRY=/data/cameras.json
GO2RTC_CFG=/data/go2rtc.yaml
BRIDGE_PID=""
GO2RTC_PID=""
REFRESH_PID=""

EMAIL=$(bashio::config 'email')
OTP_CODE=$(bashio::config 'otp_code')
CONTROL_HOST=$(bashio::config 'control_host')
STREAM_BACKEND=$(bashio::config 'stream_backend')
MQTT_DISCOVERY=$(bashio::config 'mqtt_discovery')
MQTT_HOST=$(bashio::config 'mqtt_host')
MQTT_PORT=$(bashio::config 'mqtt_port')
MQTT_USERNAME=$(bashio::config 'mqtt_username')
MQTT_PASSWORD=$(bashio::config 'mqtt_password')
MQTT_PREFIX=$(bashio::config 'mqtt_discovery_prefix')
STREAM_HOST=$(bashio::config 'stream_host')
EXTERNAL_STREAM_PORT=$(bashio::config 'external_stream_port')
SHUTDOWN_TIMEOUT=$(bashio::config 'shutdown_timeout')
CREDENTIAL_REFRESH_INTERVAL=$(bashio::config 'credential_refresh_interval')

if [[ -z "${CONTROL_HOST}" ]]; then
  bashio::exit.nok "control_host must not be empty"
fi
if [[ -z "${STREAM_HOST}" ]]; then
  bashio::exit.nok "stream_host must not be empty"
fi
if [[ "${MQTT_DISCOVERY}" == "true" && -z "${MQTT_HOST}" ]]; then
  bashio::exit.nok "mqtt_host is required when mqtt_discovery is enabled"
fi
if [[ "${MQTT_DISCOVERY}" == "true" && -z "${MQTT_PREFIX}" ]]; then
  bashio::exit.nok "mqtt_discovery_prefix is required when mqtt_discovery is enabled"
fi

export VM65_OTP_CODE="${OTP_CODE}"
export VM65_MQTT_PASSWORD="${MQTT_PASSWORD}"

load_credentials() {
  local -a setup_args
  bashio::log.info "Refreshing compatible camera credentials from Motorola Nursery"
  setup_args=(-email "${EMAIL}" -control-host "${CONTROL_HOST}" -output "${CREDS}" -registry "${REGISTRY}" -go2rtc-config "${GO2RTC_CFG}")
  if [[ "${STREAM_BACKEND}" == "external" ]]; then
    setup_args+=( -go2rtc-webrtc=false )
  fi
  vm65-setup "${setup_args[@]}"
}

shutdown_children() {
  local pid deadline still_running
  for pid in "${REFRESH_PID}" "${GO2RTC_PID}" "${BRIDGE_PID}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" 2>/dev/null || true
    fi
  done
  deadline=$((SECONDS + SHUTDOWN_TIMEOUT))
  while (( SECONDS < deadline )); do
    still_running=false
    for pid in "${REFRESH_PID}" "${GO2RTC_PID}" "${BRIDGE_PID}"; do
      if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
        still_running=true
      fi
    done
    [[ "${still_running}" == "false" ]] && break
    sleep 0.2
  done
  for pid in "${REFRESH_PID}" "${GO2RTC_PID}" "${BRIDGE_PID}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      bashio::log.warning "Process ${pid} did not stop in time; forcing shutdown"
      kill -KILL "${pid}" 2>/dev/null || true
    fi
    if [[ -n "${pid}" ]]; then
      wait "${pid}" 2>/dev/null || true
    fi
  done
  REFRESH_PID=""
  GO2RTC_PID=""
  BRIDGE_PID=""
}

handle_signal() {
  exit 143
}

trap shutdown_children EXIT
trap handle_signal TERM INT

while true; do
  load_credentials

  bashio::log.info "Starting Motorola Nursery bridge (backend=${STREAM_BACKEND})"
  BRIDGE_LISTEN=127.0.0.1:8554
  STREAM_PORT=8555
  if [[ "${STREAM_BACKEND}" == "external" ]]; then STREAM_PORT=${EXTERNAL_STREAM_PORT}; fi
  BRIDGE_ARGS=(-listen "${BRIDGE_LISTEN}" -status 0.0.0.0:8557 -creds "${CREDS}" -registry "${REGISTRY}" -stream-url "rtsp://${STREAM_HOST}:${STREAM_PORT}/vm65" -shutdown-timeout "${SHUTDOWN_TIMEOUT}s")
  BRIDGE_ARGS+=( -go2rtc-required -go2rtc-url "http://127.0.0.1:1984/" )
  if [[ "${MQTT_DISCOVERY}" == "true" ]]; then
    BRIDGE_ARGS+=( -mqtt-host "${MQTT_HOST}" -mqtt-port "${MQTT_PORT}" -mqtt-username "${MQTT_USERNAME}" -mqtt-discovery-prefix "${MQTT_PREFIX}" )
  fi
  vm65-bridge "${BRIDGE_ARGS[@]}" &
  BRIDGE_PID=$!

  WAIT_PIDS=("${BRIDGE_PID}")
  bashio::log.info "Starting go2rtc RTSP republisher (WebRTC backend=${STREAM_BACKEND})"
  go2rtc -config "${GO2RTC_CFG}" &
  GO2RTC_PID=$!
  WAIT_PIDS+=("${GO2RTC_PID}")

  sleep "${CREDENTIAL_REFRESH_INTERVAL}" &
  REFRESH_PID=$!
  WAIT_PIDS+=("${REFRESH_PID}")
  set +e
  wait -n "${WAIT_PIDS[@]}"
  STATUS=$?
  set -e

  if ! kill -0 "${REFRESH_PID}" 2>/dev/null; then
    bashio::log.info "Credential refresh interval reached; restarting media services"
    shutdown_children
    continue
  fi
  exit "${STATUS}"
done
