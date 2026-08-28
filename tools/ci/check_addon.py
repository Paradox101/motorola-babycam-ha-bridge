#!/usr/bin/env python3
"""Validate Home Assistant add-on metadata and deterministic build inputs."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

import yaml


REQUIRED_ARCHITECTURES = {"amd64", "aarch64"}


def validate_addon(root: Path) -> list[str]:
    errors: list[str] = []
    config_path = root / "config.yaml"
    dockerfile_path = root / "Dockerfile"
    # Home Assistant now expects build arguments in the Dockerfile itself.
    if (root / "build.yaml").exists():
        errors.append("deprecated build.yaml must be removed")

    try:
        config = yaml.safe_load(config_path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as error:
        return [f"cannot parse {config_path}: {error}"]
    if not isinstance(config, dict):
        return ["config.yaml root must be a mapping"]

    architectures = set(config.get("arch") or [])
    if not REQUIRED_ARCHITECTURES.issubset(architectures):
        errors.append("arch must contain amd64 and aarch64")

    options = config.get("options") or {}
    schema = config.get("schema") or {}
    if not isinstance(options, dict) or not isinstance(schema, dict):
        errors.append("options and schema must be mappings")
    else:
        difference = sorted(set(options) ^ set(schema))
        if difference:
            errors.append(f"options/schema keys differ: {', '.join(difference)}")

    ports = config.get("ports") or {}
    declared_ports: set[str] = set()
    host_bindings: set[tuple[str, int]] = set()
    if not isinstance(ports, dict):
        errors.append("ports must be a mapping")
    else:
        for container_binding, host_port in ports.items():
            container_text = str(container_binding)
            container_port, separator, protocol = container_text.partition("/")
            if not separator or protocol not in {"tcp", "udp"} or not container_port.isdigit():
                errors.append(f"invalid container port declaration: {container_text}")
                continue
            declared_ports.add(container_port)
            if host_port is None:
                continue
            binding = (protocol, int(host_port))
            if binding in host_bindings:
                errors.append(f"duplicate host {protocol} port: {host_port}")
            host_bindings.add(binding)

    watchdog = str(config.get("watchdog") or "")
    watchdog_match = re.search(r"\[PORT:(\d+)\]", watchdog)
    if not watchdog_match:
        errors.append("watchdog must reference a declared container port")
    elif watchdog_match.group(1) not in declared_ports:
        errors.append(
            f"watchdog references undeclared container port {watchdog_match.group(1)}"
        )

    try:
        dockerfile = dockerfile_path.read_text(encoding="utf-8")
    except OSError as error:
        errors.append(f"cannot read {dockerfile_path}: {error}")
    else:
        for image in re.findall(r"(?im)^FROM(?:\s+--platform=\S+)?\s+(\S+)", dockerfile):
            if image.lower().endswith(":latest"):
                errors.append(f"Dockerfile uses mutable image tag: {image}")

    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "root",
        nargs="?",
        type=Path,
        default=Path("homeassistant/vm65-bridge"),
    )
    args = parser.parse_args()
    errors = validate_addon(args.root)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print(f"add-on validation passed: {args.root}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
