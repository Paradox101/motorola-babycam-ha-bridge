import tempfile
import unittest
from pathlib import Path

from check_addon import validate_addon


VALID_CONFIG = """
name: Motorola Nursery Homeassistant Bridge
version: "0.2.0"
slug: vm65_bridge
arch: [amd64, aarch64]
boot: auto
ingress: true
ingress_port: 8099
ingress_stream: true
ports:
  8099/tcp: null
  8557/tcp: null
watchdog: "http://[HOST]:8557/healthz"
options:
  mqtt_discovery: false
schema:
  mqtt_discovery: bool
"""

VALID_CHANGELOG = "# Changelog\n\n## 0.2.0\n\nThe first entry.\n"

VALID_TRANSLATIONS = """
configuration:
  mqtt_discovery:
    name: Publish to Home Assistant over MQTT
    description: Creates a device per camera.
"""

VALID_APPARMOR = """
#include <tunables/global>

profile vm65_bridge flags=(attach_disconnected,mediate_deleted) {
  #include <abstractions/base>
  file,
  network,
  /run.sh ix,
}
"""

VALID_DOCKERFILE = (
    "FROM example/image:1.2.3\n"
    "RUN apk add --no-cache ffmpeg\n"
    "COPY config.yaml /tmp/addon-config.yaml\n"
    "RUN git clone https://example.test/repo /src\n"
)


class ValidateAddonTests(unittest.TestCase):
    def validate(
        self,
        config=VALID_CONFIG,
        dockerfile=VALID_DOCKERFILE,
        build_yaml=None,
        apparmor=VALID_APPARMOR,
        changelog=VALID_CHANGELOG,
        translations=VALID_TRANSLATIONS,
        artwork_files=("icon.png", "logo.png"),
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "config.yaml").write_text(config, encoding="utf-8")
            (root / "Dockerfile").write_text(dockerfile, encoding="utf-8")
            if apparmor is not None:
                (root / "apparmor.txt").write_text(apparmor, encoding="utf-8")
            if build_yaml is not None:
                (root / "build.yaml").write_text(build_yaml, encoding="utf-8")
            if changelog is not None:
                (root / "CHANGELOG.md").write_text(changelog, encoding="utf-8")
            if translations is not None:
                (root / "translations").mkdir()
                (root / "translations" / "en.yaml").write_text(translations, encoding="utf-8")
            for artwork in artwork_files:
                (root / artwork).write_bytes(b"\x89PNG\r\n\x1a\n")
            return validate_addon(root)

    def test_accepts_valid_addon(self):
        self.assertEqual([], self.validate())

    def test_requires_model_neutral_public_name(self):
        errors = self.validate(
            VALID_CONFIG.replace(
                "name: Motorola Nursery Homeassistant Bridge",
                "name: Motorola VM65 Bridge",
            )
        )
        self.assertIn(
            "add-on name must be Motorola Nursery Homeassistant Bridge", errors
        )

    def test_requires_both_supported_architectures(self):
        errors = self.validate(VALID_CONFIG.replace("[amd64, aarch64]", "[amd64]"))
        self.assertIn("arch must contain amd64 and aarch64", errors)

    def test_requires_option_and_schema_parity(self):
        errors = self.validate(VALID_CONFIG.replace("  mqtt_discovery: bool\n", ""))
        self.assertIn("options/schema keys differ: mqtt_discovery", errors)

    def test_rejects_mutable_latest_image(self):
        errors = self.validate(dockerfile=VALID_DOCKERFILE.replace("1.2.3", "latest"))
        self.assertIn("Dockerfile uses mutable image tag: example/image:latest", errors)

    def test_requires_config_copy_before_remote_source_clone(self):
        errors = self.validate(
            dockerfile=VALID_DOCKERFILE.replace("COPY config.yaml /tmp/addon-config.yaml\n", "")
        )
        self.assertIn("Dockerfile must copy config.yaml before cloning remote source", errors)

    def test_watchdog_must_reference_declared_container_port(self):
        errors = self.validate(VALID_CONFIG.replace("[HOST]:8557", "[HOST]:9999"))
        self.assertIn("watchdog references undeclared container port 9999", errors)

    # [PORT:n] resolves to a host mapping, and the ports Home Assistant reaches
    # internally deliberately have none.
    def test_watchdog_must_not_depend_on_a_host_port_mapping(self):
        errors = self.validate(VALID_CONFIG.replace("[HOST]:8557", "[HOST]:[PORT:8557]"))
        self.assertIn(
            "watchdog must use a literal container port; [PORT:n] needs a host mapping",
            errors,
        )

    def test_rejects_a_published_go2rtc_api_port(self):
        errors = self.validate(
            VALID_CONFIG.replace("  8099/tcp: null", "  8099/tcp: null\n  1984/tcp: 1984")
        )
        self.assertIn(
            "go2rtc's API port must not be published; it is unauthenticated", errors
        )

    def test_rejects_privileges_the_addon_does_not_need(self):
        errors = self.validate(VALID_CONFIG + "\nhost_network: true\nhassio_role: admin\n")
        self.assertIn(
            "host_network is not needed by this add-on and lowers its security rating",
            errors,
        )
        self.assertIn(
            "hassio_role is not needed by this add-on and lowers its security rating",
            errors,
        )

    def test_requires_an_apparmor_profile_named_after_the_slug(self):
        self.assertIn(
            "apparmor.txt is required; the Supervisor loads it automatically",
            self.validate(apparmor=None),
        )
        errors = self.validate(apparmor=VALID_APPARMOR.replace("vm65_bridge", "other"))
        self.assertIn(
            "apparmor profile other must match the add-on slug vm65_bridge", errors
        )

    def test_requires_apparmor_to_execute_the_entrypoint(self):
        errors = self.validate(apparmor=VALID_APPARMOR.replace("/run.sh ix,", "/run.sh r,"))
        self.assertIn("apparmor.txt must grant /run.sh execute permission", errors)

    def test_requires_ffmpeg_for_snapshots(self):
        errors = self.validate(
            dockerfile=VALID_DOCKERFILE.replace("RUN apk add --no-cache ffmpeg\n", "")
        )
        self.assertIn(
            "Dockerfile must install ffmpeg; go2rtc needs it for snapshots", errors
        )

    def test_rejects_deprecated_build_yaml(self):
        errors = self.validate(build_yaml="build_from: {}\n")
        self.assertIn("deprecated build.yaml must be removed", errors)

    def test_rejects_a_port_based_webui_link(self):
        errors = self.validate(VALID_CONFIG + '\nwebui: "http://[HOST]:[PORT:1984]/"\n')
        self.assertIn(
            "webui must not be set; serve the Web UI through ingress instead", errors
        )

    def test_requires_ingress_for_the_web_ui(self):
        errors = self.validate(VALID_CONFIG.replace("ingress: true", "ingress: false"))
        self.assertIn(
            "ingress must be enabled so the Web UI works off the local network", errors
        )

    def test_ingress_port_must_be_a_declared_container_port(self):
        errors = self.validate(VALID_CONFIG.replace("ingress_port: 8099", "ingress_port: 9999"))
        self.assertIn("ingress_port 9999 is not a declared container port", errors)

    # The Supervisor buffers an ingress response unless the add-on opts out, and
    # the Web UI's MJPEG fallback is a response that never ends.
    def test_requires_streamed_ingress(self):
        errors = self.validate(VALID_CONFIG.replace("ingress_stream: true", "ingress_stream: false"))
        self.assertIn(
            "ingress_stream must be true; the Web UI streams MJPEG and fMP4 "
            "responses that a buffering proxy never delivers",
            errors,
        )

    def test_requires_a_changelog_covering_the_version(self):
        self.assertIn(
            "CHANGELOG.md is required; the add-on page shows it as the Changelog tab",
            self.validate(changelog=None),
        )
        errors = self.validate(changelog="# Changelog\n\n## 0.1.0\n")
        self.assertIn("CHANGELOG.md does not mention version 0.2.0", errors)

    def test_requires_a_translation_for_every_option(self):
        self.assertIn(
            "translations/en.yaml is required so options have names in the UI",
            self.validate(translations=None),
        )
        errors = self.validate(translations="configuration: {}\n")
        self.assertIn("translations/en.yaml is missing options: mqtt_discovery", errors)
        errors = self.validate(
            translations=VALID_TRANSLATIONS + "  gone:\n    name: Removed\n"
        )
        self.assertIn("translations/en.yaml describes unknown options: gone", errors)

    def test_requires_store_artwork(self):
        errors = self.validate(artwork_files=("icon.png",))
        self.assertIn("logo.png is required; the add-on store shows it", errors)

    def test_requires_starting_on_boot(self):
        errors = self.validate(VALID_CONFIG.replace("boot: auto", "boot: manual"))
        self.assertIn("boot must be auto so the add-on returns after a restart", errors)

    # A Home Assistant base tag without a date moves under the add-on, and a
    # bare Alpine series can sit on one that no longer gets security updates.
    def test_requires_a_dated_home_assistant_base_tag(self):
        errors = self.validate(
            dockerfile=VALID_DOCKERFILE.replace(
                "FROM example/image:1.2.3",
                "FROM ghcr.io/home-assistant/amd64-base:3.19",
            )
        )
        self.assertIn(
            "Home Assistant base image must be pinned to a dated tag: "
            "ghcr.io/home-assistant/amd64-base:3.19",
            errors,
        )
        self.assertEqual(
            [],
            self.validate(
                dockerfile=VALID_DOCKERFILE.replace(
                    "FROM example/image:1.2.3",
                    "FROM ghcr.io/home-assistant/amd64-base:3.24-2026.08.0",
                )
            ),
        )


if __name__ == "__main__":
    unittest.main()
