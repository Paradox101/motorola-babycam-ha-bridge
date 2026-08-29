import unittest
from pathlib import Path


class AddonImageWorkflowTests(unittest.TestCase):
    def test_normalizes_the_ghcr_owner_before_tagging_images(self):
        workflow = Path(".github/workflows/addon-image.yml").read_text(encoding="utf-8")

        self.assertIn('echo "owner=${GITHUB_REPOSITORY_OWNER,,}" >> "$GITHUB_OUTPUT"', workflow)
        self.assertIn("ghcr.io/${{ steps.owner.outputs.owner }}/${{ matrix.arch }}-motorola-nursery-homeassistant-bridge:", workflow)
        self.assertNotIn("${{ matrix.arch }}-vm65-bridge:", workflow)


if __name__ == "__main__":
    unittest.main()
