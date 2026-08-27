#!/usr/bin/env python3
"""Summarize classic Linux-cooked IPv4/TCP captures without exposing payloads."""

import argparse
import collections
import math
import re
import ipaddress
import json
import struct
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("pcap", type=Path)
    parser.add_argument("--json", type=Path)
    args = parser.parse_args()
    flows = collections.defaultdict(lambda: {
        "packets": 0, "payload_packets": 0, "payload_bytes": 0,
        "payload_lengths": collections.Counter(), "timestamps": [], "sample": bytearray(),
        "first_payloads": []
    })
    with args.pcap.open("rb") as source:
        header = source.read(24)
        if len(header) != 24:
            raise SystemExit("truncated pcap")
        magic = header[:4]
        endian = "<" if magic in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
        while True:
            packet_header = source.read(16)
            if not packet_header:
                break
            sec, usec, included, _original = struct.unpack(endian + "IIII", packet_header)
            packet = source.read(included)
            if len(packet) < 36:  # 16 SLL + minimum IPv4
                continue
            ip = packet[16:]
            if ip[0] >> 4 != 4 or ip[9] != 6:
                continue
            ihl = (ip[0] & 0x0F) * 4
            total = struct.unpack("!H", ip[2:4])[0]
            src = str(ipaddress.ip_address(ip[12:16]))
            dst = str(ipaddress.ip_address(ip[16:20]))
            tcp = ip[ihl:total]
            if len(tcp) < 20:
                continue
            sport, dport = struct.unpack("!HH", tcp[:4])
            offset = (tcp[12] >> 4) * 4
            payload_len = max(0, len(tcp) - offset)
            key = f"{src}:{sport} -> {dst}:{dport}"
            flow = flows[key]
            flow["packets"] += 1
            if payload_len:
                payload = tcp[offset:]
                flow["payload_packets"] += 1
                flow["payload_bytes"] += payload_len
                flow["payload_lengths"][payload_len] += 1
                flow["timestamps"].append(sec + usec / 1_000_000)
                if len(flow["sample"]) < 65536:
                    flow["sample"].extend(payload[:65536 - len(flow["sample"])])
                if len(flow["first_payloads"]) < 12:
                    rendered = ''.join(chr(b) if 32 <= b <= 126 else '.' for b in payload[:512])
                    rendered = re.sub(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}", "<email>", rendered)
                    rendered = re.sub(r"[A-Za-z0-9_-]{20,}", lambda m: f"<opaque:{len(m.group(0))}>", rendered)
                    flow["first_payloads"].append({
                        "timestamp": round(sec + usec / 1_000_000, 6),
                        "length": payload_len,
                        "redacted_printable_preview": rendered,
                    })

    output = []
    for key, value in sorted(flows.items(), key=lambda item: -item[1]["payload_bytes"]):
        times = value.pop("timestamps")
        sample = bytes(value.pop("sample"))
        gaps = [round(times[i] - times[i - 1], 3) for i in range(1, len(times))]
        counts = collections.Counter(sample)
        entropy = -sum((n / len(sample)) * math.log2(n / len(sample)) for n in counts.values()) if sample else 0
        value["sample_entropy_bits_per_byte"] = round(entropy, 3)
        value["plaintext_markers"] = {
            marker.decode("ascii"): marker.lower() in sample.lower()
            for marker in (b"RTSP/1.0", b"OPTIONS", b"DESCRIBE", b"accessToken", b"owner/streaming")
        }
        value["common_payload_lengths"] = value.pop("payload_lengths").most_common(12)
        value["common_inter_payload_gaps_seconds"] = collections.Counter(gaps).most_common(12)
        output.append({"flow": key, **value})
    rendered = json.dumps({"source": str(args.pcap), "payload_redacted": True, "flows": output}, indent=2)
    if args.json:
        args.json.write_text(rendered + "\n", encoding="utf-8")
    else:
        print(rendered)


if __name__ == "__main__":
    main()
