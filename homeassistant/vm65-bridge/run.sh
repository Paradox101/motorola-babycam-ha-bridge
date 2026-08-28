#!/usr/bin/with-contenv bashio
# shellcheck shell=bash
#
# Add-on entrypoint: validate options, refresh the Nursery session and
# supervise the bridge plus the optional bundled go2rtc process.
set -euo pipefail

CREDS=/data/creds.json
REGISTRY=/data/cameras.json
GO2RTC_CFG=/data/go2rtc.yaml
SNAPSHOT_TOKEN=/data/snapshot-token
# Must match ingress_port in config.yaml.
INGRESS_PORT=8099
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
TEMPERATURE_POLL_INTERVAL=$(bashio::config 'temperature_poll_interval')
STREAM_HOST=$(bashio::config 'stream_host')
EXTERNAL_STREAM_PORT=$(bashio::config 'external_stream_port')
SHUTDOWN_TIMEOUT=$(bashio::config 'shutdown_timeout')
CREDENTIAL_REFRESH_INTERVAL=$(bashio::config 'credential_refresh_interval')

# Home Assistant can hand us the broker itself. Anything set explicitly in the
# add-on options still wins, so existing configurations keep working.
if [[ "${MQTT_DISCOVERY}" == "true" ]] && bashio::services.available "mqtt"; then
  [[ -z "${MQTT_HOST}" || "${MQTT_HOST}" == "core-mosquitto" ]] && MQTT_HOST=$(bashio::services mqtt "host")
  [[ -z "${MQTT_PORT}" || "${MQTT_PORT}" == "1883" ]] && MQTT_PORT=$(bashio::services mqtt "port")
  [[ -z "${MQTT_USERNAME}" ]] && MQTT_USERNAME=$(bashio::services mqtt "username")
  [[ -z "${MQTT_PASSWORD}" ]] && MQTT_PASSWORD=$(bashio::services mqtt "password")
  bashio::log.info "Using the MQTT broker provided by Home Assistant (${MQTT_HOST}:${MQTT_PORT})"
fi

# Home Assistant fetches camera snapshots by the add-on's hostname on the
# Supervisor network. That name is assigned by the Supervisor, always resolves
# from Home Assistant, and needs no published port — unlike stream_host, which
# only works if the name resolves on the LAN. Fall back to stream_host when the
# Supervisor cannot be asked, so a bare container still starts.
ADDON_HOSTNAME=$(bashio::addon.hostname 2>/dev/null || true)
if [[ -z "${ADDON_HOSTNAME}" || "${ADDON_HOSTNAME}" == "null" ]]; then
  ADDON_HOSTNAME="${STREAM_HOST}"
  bashio::log.warning "Could not read this add-on's hostname; snapshots will use ${STREAM_HOST}"
fi

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
  local status=0
  bashio::log.info "Refreshing compatible camera credentials from Motorola Nursery"
  setup_args=(-email "${EMAIL}" -control-host "${CONTROL_HOST}" -output "${CREDS}" -registry "${REGISTRY}" -go2rtc-config "${GO2RTC_CFG}")
  if [[ "${STREAM_BACKEND}" == "external" ]]; then
    setup_args+=( -go2rtc-webrtc=false )
  fi
  vm65-setup "${setup_args[@]}" || status=$?
  if (( status != 0 )); then
    # Pairing is a user action, not a crash: say what to do instead of letting
    # the Supervisor restart the add-on in a loop.
    bashio::log.fatal "Could not obtain camera credentials."
    bashio::log.fatal "If the log above says PAIRING_REQUIRED: set the 'email' option, start the"
    bashio::log.fatal "add-on once, then copy the code from your inbox into 'otp_code' and start again."
    return "${status}"
  fi
  return 0
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

load_credentials

bashio::log.info "Starting Motorola Nursery bridge (backend=${STREAM_BACKEND})"
BRIDGE_LISTEN=127.0.0.1:8554
STREAM_PORT=8555
if [[ "${STREAM_BACKEND}" == "external" ]]; then STREAM_PORT=${EXTERNAL_STREAM_PORT}; fi
BRIDGE_ARGS=(-listen "${BRIDGE_LISTEN}" -status 0.0.0.0:8557 -creds "${CREDS}" -registry "${REGISTRY}" -stream-url "rtsp://${STREAM_HOST}:${STREAM_PORT}/vm65" -shutdown-timeout "${SHUTDOWN_TIMEOUT}s")
BRIDGE_ARGS+=( -go2rtc-required -go2rtc-url "http://127.0.0.1:1984/" )
# The Web UI is proxied to go2rtc only after the Supervisor's ingress user
# header has been checked, so this listener is the one Ingress talks to.
BRIDGE_ARGS+=( -ingress "0.0.0.0:${INGRESS_PORT}" )
if [[ "${MQTT_DISCOVERY}" == "true" ]]; then
  BRIDGE_ARGS+=( -mqtt-host "${MQTT_HOST}" -mqtt-port "${MQTT_PORT}" -mqtt-username "${MQTT_USERNAME}" -mqtt-discovery-prefix "${MQTT_PREFIX}" -temperature-poll-interval "${TEMPERATURE_POLL_INTERVAL}s" )
  # Snapshots come from the bundled go2rtc, served and cached by the bridge on
  # the Supervisor network. Home Assistant reaches it by the add-on's internal
  # hostname, so no host port is involved and no name has to resolve on the
  # LAN. In external mode the media server owns snapshots, so none is
  # advertised.
  if [[ "${STREAM_BACKEND}" != "external" ]]; then
    BRIDGE_ARGS+=( -snapshot-url-base "http://${ADDON_HOSTNAME}:${INGRESS_PORT}" -snapshot-token-file "${SNAPSHOT_TOKEN}" )
  fi
fi
vm65-bridge "${BRIDGE_ARGS[@]}" &
BRIDGE_PID=$!

bashio::log.info "Starting go2rtc RTSP republisher (WebRTC backend=${STREAM_BACKEND})"
go2rtc -config "${GO2RTC_CFG}" &
GO2RTC_PID=$!

while true; do
  sleep "${CREDENTIAL_REFRESH_INTERVAL}" &
  REFRESH_PID=$!

  set +e
  wait -n "${BRIDGE_PID}" "${GO2RTC_PID}" "${REFRESH_PID}"
  STATUS=$?
  set -e

  if kill -0 "${REFRESH_PID}" 2>/dev/null; then
    # The bridge or go2rtc exited: that is a real failure, so surface it.
    exit "${STATUS}"
  fi
  REFRESH_PID=""

  # The refresh interval elapsed. Rewrite the credentials and signal the bridge
  # to pick them up. Cameras whose credentials did not change keep streaming, so
  # nobody loses their picture over a routine refresh.
  bashio::log.info "Credential refresh interval reached; refreshing in place"
  if load_credentials; then
    kill -HUP "${BRIDGE_PID}" 2>/dev/null || true
  else
    bashio::log.warning "Credential refresh failed; continuing with the current credentials"
  fi
done
