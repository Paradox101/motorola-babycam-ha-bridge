import tempfile
import unittest
from pathlib import Path

from check_release import validate_release


class ValidateReleaseTests(unittest.TestCase):
    def validate(self, tag="v1.2.3", version="1.2.3", source_ref="v1.2.3"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            addon = root / "homeassistant" / "vm65-bridge"
            addon.mkdir(parents=True)
            (addon / "config.yaml").write_text(
                f'version: "{version}"\n', encoding="utf-8"
            )
            (addon / "Dockerfile").write_text(
                f"ARG SOURCE_REF={source_ref}\n", encoding="utf-8"
            )
            return validate_release(root, tag)

    def test_accepts_matching_semver_release(self):
        self.assertEqual([], self.validate())

    def test_rejects_non_semver_tag(self):
        self.assertEqual(
            ["release tag must use vMAJOR.MINOR.PATCH"], self.validate(tag="release-1")
        )

    def test_rejects_version_mismatch(self):
        self.assertIn("add-on version must equal 1.2.3", self.validate(version="1.2.4"))

    def test_rejects_unpinned_source(self):
        self.assertIn(
            "Dockerfile SOURCE_REF must equal v1.2.3",
            self.validate(source_ref="main"),
        )


if __name__ == "__main__":
    unittest.main()
