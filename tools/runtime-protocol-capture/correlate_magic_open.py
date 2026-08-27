#!/usr/bin/env python3
"""Correlate redacted WEB2 opening fields with app log labels.

The tool deliberately prints only lengths and equality booleans, never values.
"""

import argparse
import ipaddress
import json
import re
import struct
from pathlib import Path


def relay_payloads(path: Path):
    with path.open("rb") as source:
        header = source.read(24)
        endian = "<" if header[:4] in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
        while packet_header := source.read(16):
            _sec, _usec, included, _original = struct.unpack(endian + "IIII", packet_header)
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
            _sport, dport = struct.unpack("!HH", tcp[:4])
            offset = (tcp[12] >> 4) * 4
            payload = tcp[offset:]
            if dport == 9901 and payload.startswith(b"v"):
                yield payload


def log_values(path: Path):
    magic_uuids, session_names = set(), set()
    for line in path.read_text(encoding="utf-8").splitlines():
        try:
            text = json.loads(line).get("value", {}).get("text", "")
        except json.JSONDecodeError:
            continue
        if match := re.search(r"magicUuid: ?([^, ]+)", text):
            magic_uuids.add(match.group(1))
        if match := re.search(r"connectDevice sessionName:([^, ]+)", text):
            session_names.add(match.group(1))
    return magic_uuids, session_names


def parse(payload: bytes):
    match = re.fullmatch(rb"v(\d{3}) (\d{3}) (\d{5}) (\d{3}) (\S+) (\d{4}) (\S+)", payload)
    if not match:
        raise ValueError("opening payload does not match the native format string")
    first, second = match.group(5).decode(), match.group(7).decode()
    if len(first) != int(match.group(4)) or len(second) != int(match.group(6)):
        raise ValueError("encoded identifier length mismatch")
    return first, second


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("pcap", type=Path)
    parser.add_argument("flog", type=Path)
    args = parser.parse_args()
    magic_uuids, session_names = log_values(args.flog)
    for index, payload in enumerate(relay_payloads(args.pcap), 1):
        first, second = parse(payload)
        print(json.dumps({
            "frame": index,
            "first_length": len(first),
            "first_matches_magic_uuid": first in magic_uuids,
            "first_matches_session_name": first in session_names,
            "second_length": len(second),
            "second_matches_magic_uuid": second in magic_uuids,
            "second_matches_session_name": second in session_names,
        }))


if __name__ == "__main__":
    main()
