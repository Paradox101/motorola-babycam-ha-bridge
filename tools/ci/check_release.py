#!/usr/bin/env python3
"""Validate the tag, add-on version and pinned add-on source revision."""

from __future__ import annotations

import argparse
import re
from pathlib import Path

import yaml


SEMVER_TAG = re.compile(r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")

# The add-on builds from this branch rather than from the version tag: a tag
# cannot exist before the commit that names it, so pinning to one broke every
# add-on build between the release commit and the tag push.
RELEASE_BRANCH = "release"


def validate_release(root: Path, tag: str) -> list[str]:
    match = SEMVER_TAG.fullmatch(tag)
    if not match:
        return ["release tag must use vMAJOR.MINOR.PATCH"]

    errors: list[str] = []
    addon = root / "homeassistant" / "vm65-bridge"
    config = yaml.safe_load((addon / "config.yaml").read_text(encoding="utf-8"))
    version = ".".join(match.groups())
    if str(config.get("version")) != version:
        errors.append(f"add-on version must equal {version}")

    dockerfile = (addon / "Dockerfile").read_text(encoding="utf-8")
    source_match = re.search(r"(?m)^ARG SOURCE_REF=(\S+)$", dockerfile)
    if source_match is None or source_match.group(1) != RELEASE_BRANCH:
        errors.append(f"Dockerfile SOURCE_REF must equal {RELEASE_BRANCH}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("tag")
    parser.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args()
    errors = validate_release(args.root, args.tag)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print(f"release validation passed: {args.tag}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
