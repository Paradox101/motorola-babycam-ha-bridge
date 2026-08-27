#!/usr/bin/env python3
"""Extract VM65CAP JSON lines and add stable per-socket sequence numbers."""

import argparse
import json
from collections import defaultdict
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    sequences: dict[str, int] = defaultdict(int)
    with args.input.open("r", encoding="utf-8", errors="replace") as source, \
            args.output.open("w", encoding="utf-8") as target:
        for line in source:
            marker = "[VM65CAP] "
            if marker not in line:
                continue
            try:
                row = json.loads(line.split(marker, 1)[1])
            except json.JSONDecodeError:
                continue
            key = f"{row.get('module', '?')}:{row.get('fd', '?')}:{row.get('peer', '?')}"
            sequences[key] += 1
            row["flow"] = key
            row["sequence"] = sequences[key]
            target.write(json.dumps(row, ensure_ascii=False, separators=(",", ":")) + "\n")


if __name__ == "__main__":
    main()

