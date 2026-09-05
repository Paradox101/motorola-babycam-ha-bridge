#!/usr/bin/with-contenv bashio
# shellcheck shell=bash
#
# Add-on entrypoint: validate options, refresh the Nursery session and
# supervise the bridge plus the bundled go2rtc process.
set -euo pipefail

# The add-on's persistent directory. It is a variable so the shell tests can
# point it at a scratch directory; the Supervisor always mounts /data.
DATA_DIR="${VM65_DATA_DIR:-/data}"
CREDS="${DATA_DIR}/creds.json"
REGISTRY="${DATA_DIR}/cameras.json"
GO2RTC_CFG="${DATA_DIR}/go2rtc.yaml"
SNAPSHOT_TOKEN="${DATA_DIR}/snapshot-token"
# Must match ingress_port in config.yaml.
INGRESS_PORT=8099
BRIDGE_PID=""
GO2RTC_PID=""
REFRESH_PID=""
# Hash of the generated media server configuration, so a refresh that changed
# it can be told apart from one that did not.
GO2RTC_CFG_HASH=""
# Media server restart bookkeeping, mirroring what the bridge does per camera.
MEDIA_BACKOFF=1
MEDIA_FAILURES=0
MEDIA_STARTED=0
MEDIA_BACKOFF_MAX=30
MEDIA_FAILURES_MAX=5
# Set by the SIGUSR1 trap when the Web UI asks for a credential refresh.
REFRESH_REQUESTED=false

EMAIL=$(bashio::config 'email')
OTP_CODE=$(bashio::config 'otp_code')
CONTROL_HOST=$(bashio::config 'control_host')
STREAM_BACKEND=$(bashio::config 'stream_backend')
MQTT_DISCOVERY=$(bashio::config 'mqtt_discovery')
MQTT_HOST=$(bashio::config 'mqtt_host')
MQTT_PORT=$(bashio::config 'mqtt_port')
MQTT_TLS=$(bashio::config 'mqtt_tls')
MQTT_USERNAME=$(bashio::config 'mqtt_username')
MQTT_PASSWORD=$(bashio::config 'mqtt_password')
MQTT_PREFIX=$(bashio::config 'mqtt_discovery_prefix')
TEMPERATURE_POLL_INTERVAL=$(bashio::config 'temperature_poll_interval')
CAMERA_REFRESH_INTERVAL=$(bashio::config 'camera_refresh_interval')
STREAM_HOST=$(bashio::config 'stream_host')
RTSP_PORT=$(bashio::config 'rtsp_port')
WEBRTC_PORT=$(bashio::config 'webrtc_port')
EXTERNAL_STREAM_PORT=$(bashio::config 'external_stream_port')
SHUTDOWN_TIMEOUT=$(bashio::config 'shutdown_timeout')
CREDENTIAL_REFRESH_INTERVAL=$(bashio::config 'credential_refresh_interval')
STREAM_OVERLAY=$(bashio::config 'stream_overlay')

# The burnt-in overlay needs a font, and drawing it needs an ffmpeg built with
# libfreetype. Both ship in this image, but an option that silently produces no
# picture is worse than one that says why it stayed off, and a build without
# either must not cost anyone their video. Resolve the font here and let
# vm65-setup make the final call: it renders one frame through the filter it
# generated before putting it in front of a camera.
OVERLAY_FONT=""
overlay_font_path() {
  local candidate
  local -a candidates
  if [[ -n "${VM65_OVERLAY_FONT:-}" ]]; then
    # An explicit font is the only one tried: falling back to a system font
    # after being handed a path would hide the typo that made it unreadable.
    candidates=( "${VM65_OVERLAY_FONT}" )
  else
    candidates=(
      /usr/share/fonts/dejavu/DejaVuSans.ttf
      /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf
      /usr/share/fonts/TTF/DejaVuSans.ttf
    )
  fi
  for candidate in "${candidates[@]}"; do
    if [[ -r "${candidate}" ]]; then
      printf '%s' "${candidate}"
      return 0
    fi
  done
  return 1
}
if [[ "${STREAM_OVERLAY}" == "true" ]]; then
  if OVERLAY_FONT=$(overlay_font_path); then
    bashio::log.info "Burning the date, time and camera name into the picture; this re-encodes every frame"
  else
    OVERLAY_FONT=""
    bashio::log.warning "Overlay requested but no font is installed; leaving the picture untouched"
  fi
fi

# Home Assistant can hand us the broker itself. Anything set explicitly in the
# add-on options still wins, so existing configurations keep working.
if [[ "${MQTT_DISCOVERY}" == "true" ]] && bashio::services.available "mqtt"; then
  [[ -z "${MQTT_HOST}" || "${MQTT_HOST}" == "core-mosquitto" ]] && MQTT_HOST=$(bashio::services mqtt "host")
  [[ -z "${MQTT_PORT}" || "${MQTT_PORT}" == "1883" ]] && MQTT_PORT=$(bashio::services mqtt "port")
  [[ -z "${MQTT_USERNAME}" ]] && MQTT_USERNAME=$(bashio::services mqtt "username")
  [[ -z "${MQTT_PASSWORD}" ]] && MQTT_PASSWORD=$(bashio::services mqtt "password")
  # A broker that speaks TLS refuses a plain TCP client by simply never
  # completing a handshake, which reaches the user as entities that never
  # appear and a log line that only says discovery is unavailable.
  if [[ "${MQTT_TLS}" != "true" ]]; then
    MQTT_SERVICE_SSL=$(bashio::services mqtt "ssl" 2>/dev/null || true)
    [[ "${MQTT_SERVICE_SSL}" == "true" ]] && MQTT_TLS=true
  fi
  bashio::log.info "Using the MQTT broker provided by Home Assistant (${MQTT_HOST}:${MQTT_PORT}, tls=${MQTT_TLS})"
fi

# external_stream_port used to mean two different things: the RTSP host port in
# external mode and the WebRTC host port in bundled mode. It is superseded by
# rtsp_port and webrtc_port, which mean the same thing in both modes — but a
# configuration that still sets it keeps working, and keeps winning.
if [[ -n "${EXTERNAL_STREAM_PORT}" ]] && (( EXTERNAL_STREAM_PORT > 0 )); then
  if [[ "${STREAM_BACKEND}" == "external" ]]; then
    RTSP_PORT="${EXTERNAL_STREAM_PORT}"
  else
    WEBRTC_PORT="${EXTERNAL_STREAM_PORT}"
  fi
  bashio::log.warning "external_stream_port is deprecated; use rtsp_port and webrtc_port instead"
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
if [[ -z "${RTSP_PORT}" ]] || (( RTSP_PORT < 1 )); then
  bashio::exit.nok "rtsp_port must be the host port container 8555/tcp is mapped to"
fi
if [[ -z "${WEBRTC_PORT}" ]] || (( WEBRTC_PORT < 1 )); then
  bashio::exit.nok "webrtc_port must be the host port container 8556 is mapped to"
fi
if [[ "${MQTT_DISCOVERY}" == "true" && -z "${MQTT_HOST}" ]]; then
  bashio::exit.nok "mqtt_host is required when mqtt_discovery is enabled"
fi
if [[ "${MQTT_DISCOVERY}" == "true" && -z "${MQTT_PREFIX}" ]]; then
  bashio::exit.nok "mqtt_discovery_prefix is required when mqtt_discovery is enabled"
fi

export VM65_OTP_CODE="${OTP_CODE}"
export VM65_MQTT_PASSWORD="${MQTT_PASSWORD}"

# go2rtc_config_hash fingerprints the generated media server configuration. The
# camera access token in it is derived from the device token, so a refresh that
# rotates the session rewrites this file — and go2rtc reads it only at start.
go2rtc_config_hash() {
  if [[ -f "${GO2RTC_CFG}" ]]; then
    sha256sum "${GO2RTC_CFG}" | cut -d' ' -f1
  else
    printf ''
  fi
}

# load_credentials refreshes the camera credentials. With -pair-ui it also
# serves the pairing page and waits there when the account is not paired yet,
# so first-time setup is a form in the Web UI rather than two restarts and a
# trip through the log. Called without arguments during the periodic refresh,
# where an unattended wait would be wrong.
load_credentials() {
  local -a setup_args
  local status=0
  local interactive="${1:-false}"
  bashio::log.info "Refreshing compatible camera credentials from Motorola Nursery"
  setup_args=(-email "${EMAIL}" -control-host "${CONTROL_HOST}" -output "${CREDS}" -registry "${REGISTRY}" -go2rtc-config "${GO2RTC_CFG}")
  # Without this go2rtc offers only its container address for WebRTC media, so
  # the browser negotiates a connection that can never carry a packet.
  setup_args+=( -webrtc-candidate "${STREAM_HOST}:${WEBRTC_PORT}" )
  # A code is sent because someone pressed the button in the Web UI, not
  # because the add-on restarted.
  setup_args+=( -request-code=false )
  if [[ "${STREAM_BACKEND}" == "external" ]]; then
    setup_args+=( -go2rtc-webrtc=false )
  fi
  if [[ -n "${OVERLAY_FONT}" ]]; then
    setup_args+=( -overlay-font "${OVERLAY_FONT}" )
  fi
  if [[ "${interactive}" == "true" ]]; then
    setup_args+=( -pair-ui "0.0.0.0:${INGRESS_PORT}" -status 0.0.0.0:8557 )
  fi
  vm65-setup "${setup_args[@]}" || status=$?
  if (( status != 0 )); then
    # Pairing is a user action, not a crash: say what to do instead of letting
    # the Supervisor restart the add-on in a loop.
    bashio::log.fatal "Could not obtain camera credentials."
    bashio::log.fatal "If the log above says PAIRING_REQUIRED: open this add-on in the Home"
    bashio::log.fatal "Assistant sidebar and complete pairing there."
    return "${status}"
  fi
  return 0
}

start_media_server() {
  bashio::log.info "Starting go2rtc RTSP republisher (WebRTC backend=${STREAM_BACKEND})"
  go2rtc -config "${GO2RTC_CFG}" &
  GO2RTC_PID=$!
  MEDIA_STARTED=${SECONDS}
}

stop_media_server() {
  if [[ -n "${GO2RTC_PID}" ]] && kill -0 "${GO2RTC_PID}" 2>/dev/null; then
    kill "${GO2RTC_PID}" 2>/dev/null || true
    wait "${GO2RTC_PID}" 2>/dev/null || true
  fi
  GO2RTC_PID=""
}

# restart_media_server brings go2rtc back with a backoff instead of taking the
# whole add-on down with it. The bridge already supervises its cameras one by
# one for the same reason: one failed process should not cost every camera its
# picture, plus a full credential round-trip on the way back up.
restart_media_server() {
  if (( SECONDS - MEDIA_STARTED >= 60 )); then
    MEDIA_BACKOFF=1
    MEDIA_FAILURES=0
  fi
  MEDIA_FAILURES=$(( MEDIA_FAILURES + 1 ))
  if (( MEDIA_FAILURES > MEDIA_FAILURES_MAX )); then
    bashio::log.fatal "The media server failed ${MEDIA_FAILURES} times in a row; letting the Supervisor restart the add-on"
    return 1
  fi
  bashio::log.warning "Restarting the media server in ${MEDIA_BACKOFF}s (attempt ${MEDIA_FAILURES})"
  sleep "${MEDIA_BACKOFF}"
  MEDIA_BACKOFF=$(( MEDIA_BACKOFF * 2 ))
  if (( MEDIA_BACKOFF > MEDIA_BACKOFF_MAX )); then
    MEDIA_BACKOFF=${MEDIA_BACKOFF_MAX}
  fi
  start_media_server
}

# apply_credentials adopts a freshly written credential set. The bridge swaps
# its relay sessions in place on SIGHUP, but go2rtc holds the camera-side access
# token in its generated configuration and reads that only once, so it has to be
# restarted whenever that file actually changed.
apply_credentials() {
  local new_hash
  kill -HUP "${BRIDGE_PID}" 2>/dev/null || true
  new_hash=$(go2rtc_config_hash)
  if [[ "${new_hash}" == "${GO2RTC_CFG_HASH}" ]]; then
    return 0
  fi
  GO2RTC_CFG_HASH="${new_hash}"
  bashio::log.info "The generated media server configuration changed; restarting go2rtc so the new camera credentials take effect"
  stop_media_server
  start_media_server
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

# The Web UI's "refresh credentials" button reaches the bridge, which signals
# this process: the account session and the generated media configuration are
# owned here, so this is the only place a refresh can happen.
handle_refresh_request() {
  REFRESH_REQUESTED=true
  if [[ -n "${REFRESH_PID}" ]]; then
    kill "${REFRESH_PID}" 2>/dev/null || true
  fi
}

trap shutdown_children EXIT
trap handle_signal TERM INT
trap handle_refresh_request USR1

# First start: serve the pairing page and wait there if the account is not
# paired, instead of exiting and letting the Supervisor show a failed add-on.
load_credentials true
GO2RTC_CFG_HASH=$(go2rtc_config_hash)

bashio::log.info "Starting Motorola Nursery bridge (backend=${STREAM_BACKEND})"
BRIDGE_LISTEN=127.0.0.1:8554
BRIDGE_ARGS=(-listen "${BRIDGE_LISTEN}" -status 0.0.0.0:8557 -creds "${CREDS}" -registry "${REGISTRY}" -stream-url "rtsp://${STREAM_HOST}:${RTSP_PORT}/vm65" -shutdown-timeout "${SHUTDOWN_TIMEOUT}s")
BRIDGE_ARGS+=( -go2rtc-required -go2rtc-url "http://127.0.0.1:1984/" )
# The Web UI is proxied to go2rtc only after the Supervisor's ingress user
# header has been checked, so this listener is the one Ingress talks to.
BRIDGE_ARGS+=( -ingress "0.0.0.0:${INGRESS_PORT}" )
# Still images are served to the Web UI whether or not a broker is configured,
# so the token is always needed. Persisting it keeps a URL Home Assistant
# already holds working across a restart.
BRIDGE_ARGS+=( -snapshot-token-file "${SNAPSHOT_TOKEN}" )
# Both repairs the Web UI offers are handled here: go2rtc is restarted through
# its loopback API, and a credential refresh arrives as SIGUSR1.
BRIDGE_ARGS+=( -allow-media-restart -allow-credential-refresh )
if [[ "${MQTT_DISCOVERY}" == "true" ]]; then
  BRIDGE_ARGS+=( -mqtt-host "${MQTT_HOST}" -mqtt-port "${MQTT_PORT}" -mqtt-username "${MQTT_USERNAME}" -mqtt-discovery-prefix "${MQTT_PREFIX}" -temperature-poll-interval "${TEMPERATURE_POLL_INTERVAL}s" )
  if [[ "${MQTT_TLS}" == "true" ]]; then
    BRIDGE_ARGS+=( -mqtt-tls )
  fi
  # Feeding a camera entity is the only way Home Assistant discovers a camera
  # over MQTT, so this is what saves adding one by hand. Zero turns it off.
  BRIDGE_ARGS+=( -camera-refresh-interval "${CAMERA_REFRESH_INTERVAL}s" )
  # Snapshots come from the bundled go2rtc, served and cached by the bridge on
  # the Supervisor network. Home Assistant reaches it by the add-on's internal
  # hostname, so no host port is involved and no name has to resolve on the
  # LAN. In external mode the media server owns snapshots, so no URL is
  # advertised — the Web UI still shows its own stills either way.
  if [[ "${STREAM_BACKEND}" != "external" ]]; then
    BRIDGE_ARGS+=( -snapshot-url-base "http://${ADDON_HOSTNAME}:${INGRESS_PORT}" )
  fi
fi
vm65-bridge "${BRIDGE_ARGS[@]}" &
BRIDGE_PID=$!

start_media_server

while true; do
  sleep "${CREDENTIAL_REFRESH_INTERVAL}" &
  REFRESH_PID=$!

  # -p records which child exited. Inferring it afterwards is unreliable: a
  # bridge that stops at the very moment the refresh timer elapses would be
  # read as a routine tick, and the add-on would carry on with no bridge.
  EXITED_PID=""
  set +e
  wait -n -p EXITED_PID "${BRIDGE_PID}" "${GO2RTC_PID}" "${REFRESH_PID}"
  STATUS=$?
  set -e
  # wait unsets the variable before setting it, and leaves it unset when a
  # trapped signal — the Web UI's refresh request — cut the wait short.
  EXITED_PID="${EXITED_PID:-}"

  if [[ -n "${REFRESH_PID}" ]]; then
    kill "${REFRESH_PID}" 2>/dev/null || true
    wait "${REFRESH_PID}" 2>/dev/null || true
    REFRESH_PID=""
  fi

  if [[ "${EXITED_PID}" == "${BRIDGE_PID}" ]]; then
    # The bridge is the add-on. Nothing below it is worth keeping alive.
    bashio::log.fatal "The bridge exited with status ${STATUS}"
    exit "${STATUS}"
  fi

  if [[ -n "${EXITED_PID}" && "${EXITED_PID}" == "${GO2RTC_PID}" ]]; then
    bashio::log.warning "The media server exited with status ${STATUS}"
    restart_media_server || exit "${STATUS}"
    continue
  fi

  # Either the refresh interval elapsed or the Web UI asked for one, which
  # interrupts the wait above. Rewrite the credentials and adopt them: cameras
  # whose credentials did not change keep streaming, so nobody loses their
  # picture over a routine refresh.
  if [[ "${REFRESH_REQUESTED}" == "true" ]]; then
    bashio::log.info "Credential refresh requested from the Web UI"
    REFRESH_REQUESTED=false
  else
    bashio::log.info "Credential refresh interval reached; refreshing in place"
  fi
  if load_credentials; then
    apply_credentials
  else
    bashio::log.warning "Credential refresh failed; continuing with the current credentials"
  fi
done
