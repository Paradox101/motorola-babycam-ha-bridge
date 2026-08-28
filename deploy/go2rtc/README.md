# Standalone go2rtc example

This Compose example fronts one Motorola Nursery Magic WEB2 bridge with go2rtc.
The Home Assistant add-on is recommended for automatic pairing, multi-camera
configuration and credential refresh; this directory is a manual standalone
example.

1. Run `vm65-setup` to produce a current private credentials file, or provide
   the documented single-camera JSON shape as `creds.json`.
2. Set the RTSP user, password and access token in `go2rtc.yaml`.
3. Run `docker compose up --build`.
4. Open `http://localhost:1984`; the compatibility stream is `vm65`.

Keep `creds.json` and the populated go2rtc configuration out of source control.
For automatic account/session handling use the add-on in
`../../homeassistant/vm65-bridge`.
