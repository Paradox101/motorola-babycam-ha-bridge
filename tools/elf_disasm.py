#!/usr/bin/env python3
"""Disassemble one named ARM ELF symbol, preserving its published boundaries."""

import argparse
from pathlib import Path

from capstone import Cs, CS_ARCH_ARM, CS_MODE_ARM, CS_MODE_LITTLE_ENDIAN, CS_MODE_THUMB
from elftools.elf.elffile import ELFFile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("elf", type=Path)
    parser.add_argument("symbol")
    args = parser.parse_args()
    with args.elf.open("rb") as stream:
        elf = ELFFile(stream)
        dynsym = elf.get_section_by_name(".dynsym")
        matches = dynsym.get_symbol_by_name(args.symbol) if dynsym else None
        if not matches:
            raise SystemExit(f"symbol not found: {args.symbol}")
        symbol = matches[0]
        raw_address = int(symbol["st_value"])
        address = raw_address & ~1
        size = int(symbol["st_size"])
        section = elf.get_section(symbol["st_shndx"])
        offset = address - int(section["sh_addr"])
        code = section.data()[offset:offset + size]
        mode = CS_MODE_LITTLE_ENDIAN | (CS_MODE_THUMB if raw_address & 1 else CS_MODE_ARM)
        engine = Cs(CS_ARCH_ARM, mode)
        print(f"{args.symbol}: address=0x{raw_address:x} size={size} mode={'thumb' if raw_address & 1 else 'arm'}")
        for instruction in engine.disasm(code, address):
            print(f"0x{instruction.address:08x}:\t{instruction.mnemonic:<8}\t{instruction.op_str}")


if __name__ == "__main__":
    main()
