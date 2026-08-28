#!/usr/bin/with-contenv bashio
# shellcheck shell=bash
#
# Add-on entrypoint: render config from the add-on options, start the Magic
# WEB2 bridge, then run go2rtc in the foreground so the supervisor watchdog
# tracks it.
set -euo pipefail

CREDS=/data/creds.json
GO2RTC_CFG=/data/go2rtc.yaml

DEVICE_ID=$(bashio::config 'device_id')
SID=$(bashio::config 'sid')
DEVICE_TOKEN=$(bashio::config 'device_token')
CONTROL_HOST=$(bashio::config 'control_host')
CONTROL_PORT=$(bashio::config 'control_port')
TARGET_PORT=$(bashio::config 'target_port')
RTSP_USER=$(bashio::config 'rtsp_user')
RTSP_PASSWORD=$(bashio::config 'rtsp_password')
ACCESS_TOKEN=$(bashio::config 'access_token')

if [[ -z "${SID}" || -z "${DEVICE_TOKEN}" || -z "${CONTROL_HOST}" ]]; then
  bashio::exit.nok "sid, device_token and control_host are required in the add-on options"
fi

# Credentials file consumed by vm65-bridge. Written to /data (add-on private).
cat > "${CREDS}" <<EOF
{
  "device_id": ${DEVICE_ID},
  "sid": "${SID}",
  "device_token": "${DEVICE_TOKEN}",
  "control_host": "${CONTROL_HOST}",
  "control_port": ${CONTROL_PORT},
  "target_port": ${TARGET_PORT}
}
EOF
chmod 600 "${CREDS}"

# Render go2rtc config from the template.
sed \
  -e "s|%RTSP_USER%|${RTSP_USER}|g" \
  -e "s|%RTSP_PASSWORD%|${RTSP_PASSWORD}|g" \
  -e "s|%ACCESS_TOKEN%|${ACCESS_TOKEN}|g" \
  /etc/go2rtc.tmpl.yaml > "${GO2RTC_CFG}"

bashio::log.info "Starting vm65-bridge on 127.0.0.1:8554 (control host ${CONTROL_HOST})"
vm65-bridge -listen 127.0.0.1:8554 -status 127.0.0.1:8557 -creds "${CREDS}" &
BRIDGE_PID=$!

# Stop the bridge when go2rtc exits.
trap 'kill "${BRIDGE_PID}" 2>/dev/null || true' EXIT

bashio::log.info "Starting go2rtc"
exec go2rtc -config "${GO2RTC_CFG}"
