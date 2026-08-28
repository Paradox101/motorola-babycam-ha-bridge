#!/usr/bin/env python3
"""Offline decoder for captured Magic WEB2 relay streams.

Decoded data is shown only after credential/token redaction. Raw decoded bytes
can optionally be written under the already gitignored runtime-logs tree.
"""

import argparse
import collections
import ipaddress
import json
import re
import struct
from pathlib import Path


def packets(path: Path):
    with path.open("rb") as source:
        header = source.read(24)
        endian = "<" if header[:4] in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
        while packet_header := source.read(16):
            sec, usec, included, _original = struct.unpack(endian + "IIII", packet_header)
            packet = source.read(included)
            if len(packet) < 56:
                continue
            ip = packet[16:]
            if ip[0] >> 4 != 4 or ip[9] != 6:
                continue
            ihl = (ip[0] & 15) * 4
            total = struct.unpack("!H", ip[2:4])[0]
            tcp = ip[ihl:total]
            if len(tcp) < 20:
                continue
            sport, dport, seq = struct.unpack("!HHI", tcp[:8])
            offset = (tcp[12] >> 4) * 4
            payload = tcp[offset:]
            if payload and (sport == 9901 or dport == 9901):
                yield {
                    "timestamp": sec + usec / 1_000_000,
                    "src": str(ipaddress.ip_address(ip[12:16])), "sport": sport,
                    "dst": str(ipaddress.ip_address(ip[16:20])), "dport": dport,
                    "seq": seq, "payload": payload,
                }


def reassemble(segments, start_seq=None):
    segments = sorted(segments, key=lambda item: item["seq"])
    if not segments:
        return b""
    expected = segments[0]["seq"] if start_seq is None else start_seq
    output = bytearray()
    for segment in segments:
        seq, payload = segment["seq"], segment["payload"]
        if seq + len(payload) <= expected:
            continue
        if seq > expected:
            raise ValueError(f"TCP capture gap of {seq - expected} bytes")
        overlap = expected - seq
        output.extend(payload[overlap:])
        expected += len(payload) - overlap
    return bytes(output)


def device_token(path: Path):
    values = set()
    for line in path.read_text(encoding="utf-8").splitlines():
        try:
            text = json.loads(line).get("value", {}).get("text", "")
        except json.JSONDecodeError:
            continue
        if match := re.search(r"connectDevice .*? token:([^,]+), targetPort:", text):
            values.add(match.group(1))
    if len(values) != 1:
        raise ValueError(f"expected one device token, found {len(values)}")
    value = values.pop().encode("ascii")
    if not value:
        raise ValueError("empty device token")
    return value


def bootstrap(prefix: bytes, key: bytearray):
    if len(prefix) != len(key) + 1:
        raise ValueError("invalid crypto bootstrap length")
    rolling = 0xAA
    for index, random_byte in enumerate(prefix[:-1]):
        rolling ^= (key[index] >> 1) ^ ((random_byte & 0x7F) << 1)
        key[index] = rolling & 0xFF
    return prefix[-1] % len(key)


def decode(data: bytes, token: bytes):
    overhead = len(token) + 1
    if len(data) < overhead:
        raise ValueError("encrypted direction lacks bootstrap")
    key = bytearray(token)
    state = bootstrap(data[:overhead], key)
    output = bytearray()
    for cipher_byte in data[overhead:]:
        plain_byte = cipher_byte ^ key[state]
        output.append(plain_byte)
        state = ((key[state] + plain_byte) | 1) + state
        state %= len(key)
    return bytes(output), overhead


def safe_preview(data: bytes, limit=4096):
    text = data[:limit].decode("latin-1", errors="replace")
    text = re.sub(r"rtsp://[^@\s/]+@", "rtsp://<REDACTED_CREDENTIALS>@", text, flags=re.I)
    text = re.sub(r"(?i)(accessToken=)[^&\s]+", r"\1<REDACTED>", text)
    text = re.sub(r"(?im)^(Authorization:\s*).+$", r"\1<REDACTED>", text)
    text = "".join(char if char in "\r\n\t" or 32 <= ord(char) <= 126 else "." for char in text)
    return text


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("pcap", type=Path)
    parser.add_argument("flog", type=Path)
    parser.add_argument("--output-dir", type=Path)
    args = parser.parse_args()
    token = device_token(args.flog)
    all_packets = list(packets(args.pcap))
    sessions = []
    for opening in (item for item in all_packets if item["dport"] == 9901 and item["payload"].startswith(b"v002 ")):
        client_key = (opening["src"], opening["sport"], opening["dst"], opening["dport"])
        server_key = (opening["dst"], opening["dport"], opening["src"], opening["sport"])
        outbound = [item for item in all_packets if (item["src"], item["sport"], item["dst"], item["dport"]) == client_key]
        inbound = [item for item in all_packets if (item["src"], item["sport"], item["dst"], item["dport"]) == server_key]
        encrypted_out = reassemble(outbound, opening["seq"] + len(opening["payload"]))
        encrypted_in = reassemble(inbound)
        plain_out, overhead_out = decode(encrypted_out, token)
        plain_in, overhead_in = decode(encrypted_in, token)
        result = {
            "relay": f"{opening['dst']}:9901",
            "crypto_key_length": len(token),
            "outbound_bootstrap_length": overhead_out,
            "inbound_bootstrap_length": overhead_in,
            "decoded_outbound_bytes": len(plain_out),
            "decoded_inbound_bytes": len(plain_in),
            "outbound_rtsp_markers": [m for m in ("OPTIONS", "DESCRIBE", "SETUP", "PLAY", "TEARDOWN") if m.encode() in plain_out],
            "inbound_rtsp_response": b"RTSP/1.0" in plain_in,
            "outbound_preview_redacted": safe_preview(plain_out),
            "inbound_preview_redacted": safe_preview(plain_in),
            "secrets_printed": False,
        }
        sessions.append(result)
        if args.output_dir:
            args.output_dir.mkdir(parents=True, exist_ok=True)
            # These files can contain a live access token and are intentionally
            # only emitted on explicit request into a gitignored location.
            (args.output_dir / "magic-outbound.decoded.bin").write_bytes(plain_out)
            (args.output_dir / "magic-inbound.decoded.bin").write_bytes(plain_in)
    print(json.dumps({"sessions": sessions}, indent=2))


if __name__ == "__main__":
    main()
