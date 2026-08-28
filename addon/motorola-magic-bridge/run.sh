#!/usr/bin/env bashio
# shellcheck shell=bash
set -euo pipefail

# --- Read add-on options ----------------------------------------------------
DEVICE_ID="$(bashio::config 'device_id')"
SID="$(bashio::config 'sid')"
DEVICE_TOKEN="$(bashio::config 'device_token')"
CONTROL_HOST="$(bashio::config 'control_host')"
TARGET_PORT="$(bashio::config 'target_port')"
RTSP_USER="$(bashio::config 'rtsp_user')"
RTSP_PASSWORD="$(bashio::config 'rtsp_password')"
RTSP_PATH="$(bashio::config 'rtsp_path')"
ACCESS_TOKEN="$(bashio::config 'access_token')"

BRIDGE_ADDR="127.0.0.1:8555"

if bashio::var.is_empty "${CONTROL_HOST}" || bashio::var.is_empty "${DEVICE_TOKEN}"; then
  bashio::exit.nok "control_host and device_token are required; set them in the add-on configuration."
fi

# --- Start the Magic transport bridge --------------------------------------
bashio::log.info "Starting Magic bridge on ${BRIDGE_ADDR} -> ${CONTROL_HOST} (target ${TARGET_PORT})"
MAGIC_DEVICE_ID="${DEVICE_ID}" \
MAGIC_SID="${SID}" \
MAGIC_DEVICE_TOKEN="${DEVICE_TOKEN}" \
MAGIC_CONTROL_HOST="${CONTROL_HOST}" \
MAGIC_TARGET_PORT="${TARGET_PORT}" \
  magicbridge --listen "${BRIDGE_ADDR}" &
BRIDGE_PID=$!

# Stop the bridge when go2rtc exits.
trap 'kill "${BRIDGE_PID}" 2>/dev/null || true' EXIT

# --- Compose the camera RTSP URL the bridge proxies -------------------------
# The tunnel is byte-transparent, so go2rtc speaks ordinary RTSP to the bridge
# and the bytes reach the camera. The credentials and access token below all
# originate from an authorized 5GenCare app session.
userinfo=""
if ! bashio::var.is_empty "${RTSP_USER}"; then
  userinfo="${RTSP_USER}:${RTSP_PASSWORD}@"
fi
query=""
if ! bashio::var.is_empty "${ACCESS_TOKEN}"; then
  query="?accessToken=${ACCESS_TOKEN}"
fi
CAMERA_URL="rtsp://${userinfo}${BRIDGE_ADDR}${RTSP_PATH}${query}"

# --- Render go2rtc config ---------------------------------------------------
cat > /etc/go2rtc.yaml <<EOF
api:
  listen: ":1984"
rtsp:
  listen: ":8554"
streams:
  motorola: ${CAMERA_URL}
EOF

bashio::log.info "go2rtc serving stream 'motorola' on rtsp://<host>:8554/motorola (web UI on :1984)"
bashio::log.notice "This add-on reconstructs the Magic transport only. If the camera does not attach, the 5GenCare-side authorization for this session is missing or expired; refresh the credentials from an authorized app session."

exec go2rtc -config /etc/go2rtc.yaml
