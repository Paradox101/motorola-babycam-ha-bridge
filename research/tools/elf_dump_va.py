#!/usr/bin/env python3
"""Dump bytes at an ELF virtual address for static-analysis verification."""

import argparse
from pathlib import Path

from elftools.elf.elffile import ELFFile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("elf", type=Path)
    parser.add_argument("address", type=lambda value: int(value, 0))
    parser.add_argument("--length", type=int, default=64)
    args = parser.parse_args()
    with args.elf.open("rb") as source:
        elf = ELFFile(source)
        for segment in elf.iter_segments():
            start = int(segment["p_vaddr"])
            end = start + int(segment["p_filesz"])
            if start <= args.address < end:
                offset = args.address - start
                data = segment.data()[offset:offset + args.length]
                printable = "".join(chr(b) if 32 <= b <= 126 else "." for b in data)
                print(f"va=0x{args.address:x} length={len(data)}")
                print(data.hex())
                print(printable)
                return
    raise SystemExit("virtual address is not file-backed")


if __name__ == "__main__":
    main()
