import tempfile
import unittest
from pathlib import Path

from check_addon import validate_addon


VALID_CONFIG = """
name: Motorola Nursery Bridge
version: "0.2.0"
slug: vm65_bridge
arch: [amd64, aarch64]
ports:
  8557/tcp: 8558
watchdog: "http://[HOST]:[PORT:8557]/healthz"
options:
  mqtt_discovery: false
schema:
  mqtt_discovery: bool
"""


class ValidateAddonTests(unittest.TestCase):
    def validate(
        self,
        config=VALID_CONFIG,
        dockerfile="FROM example/image:1.2.3\n",
        build_yaml=None,
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "config.yaml").write_text(config, encoding="utf-8")
            (root / "Dockerfile").write_text(dockerfile, encoding="utf-8")
            if build_yaml is not None:
                (root / "build.yaml").write_text(build_yaml, encoding="utf-8")
            return validate_addon(root)

    def test_accepts_valid_addon(self):
        self.assertEqual([], self.validate())

    def test_requires_both_supported_architectures(self):
        errors = self.validate(VALID_CONFIG.replace("[amd64, aarch64]", "[amd64]"))
        self.assertIn("arch must contain amd64 and aarch64", errors)

    def test_requires_option_and_schema_parity(self):
        errors = self.validate(VALID_CONFIG.replace("  mqtt_discovery: bool\n", ""))
        self.assertIn("options/schema keys differ: mqtt_discovery", errors)

    def test_rejects_mutable_latest_image(self):
        errors = self.validate(dockerfile="FROM example/image:latest\n")
        self.assertIn("Dockerfile uses mutable image tag: example/image:latest", errors)

    def test_watchdog_must_reference_declared_container_port(self):
        errors = self.validate(VALID_CONFIG.replace("[PORT:8557]", "[PORT:9999]"))
        self.assertIn("watchdog references undeclared container port 9999", errors)

    def test_rejects_deprecated_build_yaml(self):
        errors = self.validate(build_yaml="build_from: {}\n")
        self.assertIn("deprecated build.yaml must be removed", errors)


if __name__ == "__main__":
    unittest.main()
