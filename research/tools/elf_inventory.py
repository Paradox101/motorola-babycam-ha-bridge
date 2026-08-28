#!/usr/bin/env python3
"""Print reproducible ELF metadata without requiring a native binutils install."""

import argparse
from pathlib import Path

from elftools.elf.elffile import ELFFile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("elf", type=Path)
    parser.add_argument("--symbols", default="magicp2p_")
    parser.add_argument("--relocations", action="store_true")
    args = parser.parse_args()

    with args.elf.open("rb") as stream:
        elf = ELFFile(stream)
        print(f"file={args.elf}")
        print(f"class={elf.elfclass}")
        print(f"little_endian={elf.little_endian}")
        print(f"machine={elf['e_machine']}")
        print(f"type={elf['e_type']}")

        dynamic = elf.get_section_by_name(".dynamic")
        if dynamic:
            for tag in dynamic.iter_tags():
                if tag.entry.d_tag == "DT_NEEDED":
                    print(f"needed={tag.needed}")

        dynsym = elf.get_section_by_name(".dynsym")
        if dynsym:
            for symbol in dynsym.iter_symbols():
                if args.symbols.lower() in symbol.name.lower():
                    info = symbol["st_info"]
                    print(
                        "symbol="
                        f"{symbol.name} value=0x{symbol['st_value']:x} "
                        f"size={symbol['st_size']} bind={info['bind']} "
                        f"type={info['type']} shndx={symbol['st_shndx']}"
                    )

        if args.relocations:
            for section in elf.iter_sections():
                if not hasattr(section, "iter_relocations"):
                    continue
                linked = elf.get_section(section["sh_link"])
                for relocation in section.iter_relocations():
                    index = relocation.entry.r_info_sym
                    name = linked.get_symbol(index).name if linked and index else ""
                    print(f"relocation={section.name} offset=0x{relocation['r_offset']:x} symbol={name}")


if __name__ == "__main__":
    main()
