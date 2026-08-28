#!/usr/bin/env python3
"""Validate native generate_sid_v1 reconstruction without printing secrets."""

import argparse
import hashlib
import hmac
import json
import re
from pathlib import Path


def derive(device_id: int, sid: str, token: str) -> str:
    # libdevconn.so generate_sid_v1 first builds this buffer. The format widths
    # are minimum widths (not truncation); its first 32 bytes form the HMAC key.
    seed = f"{device_id:08x}{sid:<20}{token:<27}{sid:<20}".encode("ascii")
    key = seed[:32]
    digest = hmac.new(key, sid.encode("ascii"), hashlib.sha256).hexdigest()
    return (f"{device_id:08x}{sid[:3]}{token[:3]}{digest}").lower()


def observations(path: Path):
    inputs = []
    outputs = []
    for line in path.read_text(encoding="utf-8").splitlines():
        try:
            text = json.loads(line).get("value", {}).get("text", "")
        except json.JSONDecodeError:
            continue
        match = re.search(
            r"connectDevice sessionName:([^,]+), deviceId:(\d+), sid:([^,]+), "
            r"token:([^,]+), targetPort:(\d+)", text
        )
        if match:
            inputs.append((int(match.group(2)), match.group(3), match.group(4)))
        match = re.search(r"(?:magicUuid|magicUser): ?([^, )]+)", text)
        if match:
            outputs.append(match.group(1))
    return inputs, set(outputs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("flog", type=Path)
    args = parser.parse_args()
    inputs, outputs = observations(args.flog)
    if not inputs or not outputs:
        raise SystemExit("required redacted correlation labels not found")
    unique_inputs = set(inputs)
    matches = sum(derive(*item) in outputs for item in unique_inputs)
    print(json.dumps({
        "unique_input_sets": len(unique_inputs),
        "derived_values_matching_runtime_magic_uuid": matches,
        "all_match": matches == len(unique_inputs),
        "secrets_printed": False,
    }))


if __name__ == "__main__":
    main()
