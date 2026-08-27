"""mitmproxy addon for the reFlutter capture of the 5GenCare control flow.

The reFlutter-patched app connects everything to the proxy at 10.0.2.2:8083
(host loopback) with TLS validation disabled, so mitmproxy in transparent mode
upstreams by SNI and can read the plaintext of both HTTPS and raw-TLS sockets.

This addon prints a redacted, direction-tagged log of anything touching the
5GenCare/Magic infrastructure and leaves everything else untouched. Secrets are
never written in the clear. Full flows are still saved via mitmdump -w.

Run:
  mitmdump --mode transparent -p 8083 \
    -s scripts/arm/mitm_5gencare.py \
    -w scratchpad/reflutter/flows.mitm
"""

import re
import logging
from mitmproxy import http, tcp

VENDOR = re.compile(r"(5gen|5gencare|moto|orbweb|vrelay)", re.I)

_SECRET = re.compile(
    r"(password|passwd|accessToken|sessionToken|masterToken|deviceToken|token|pwd|sid)"
    r"([=:\"\s]+)([^\s&\"\\]+)",
    re.I,
)
_OPAQUE = re.compile(r"\b[A-Za-z0-9_-]{28,}\b")
_EMAIL = re.compile(r"[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}", re.I)


def _redact(b: bytes) -> str:
    s = b.decode("latin-1", "replace")
    s = s.replace("\r", "\\r").replace("\n", "\\n").replace("\t", "\\t")
    s = _EMAIL.sub("<email>", s)
    s = _SECRET.sub(lambda m: m.group(1) + m.group(2) + "<redacted>", s)
    s = _OPAQUE.sub(lambda m: "<opaque:%d>" % len(m.group(0)), s)
    return s[:2048]


def _host_of(flow) -> str:
    try:
        if flow.server_conn and flow.server_conn.address:
            return "%s:%d" % (flow.server_conn.address[0], flow.server_conn.address[1])
    except Exception:
        pass
    return "?"


def _sni_of(flow) -> str:
    try:
        return flow.server_conn.sni or ""
    except Exception:
        return ""


class Capture:
    def http_connect(self, flow: http.HTTPFlow):
        logging.info("[5GC] CONNECT %s sni=%s" % (_host_of(flow), _sni_of(flow)))

    def request(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host or ""
        if not VENDOR.search(host):
            return
        logging.info("[5GC] >>> %s %s%s" % (flow.request.method, host, flow.request.path))
        if flow.request.content:
            logging.info("[5GC]     body: " + _redact(flow.request.content))

    def response(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host or ""
        if not VENDOR.search(host):
            return
        logging.info("[5GC] <<< %d %s%s" % (flow.response.status_code, host, flow.request.path))
        if flow.response.content:
            logging.info("[5GC]     body: " + _redact(flow.response.content))

    # Raw-TLS sockets (e.g. 5GenCare control on 3388) surface as tcp messages.
    def tcp_message(self, flow: tcp.TCPFlow):
        sni = _sni_of(flow)
        host = _host_of(flow)
        if not (VENDOR.search(sni or "") or VENDOR.search(host)):
            return
        msg = flow.messages[-1]
        direction = ">>>" if msg.from_client else "<<<"
        logging.info(
            "[5GC-TCP] %s %s sni=%s len=%d : %s"
            % (direction, host, sni, len(msg.content), _redact(msg.content))
        )


addons = [Capture()]
