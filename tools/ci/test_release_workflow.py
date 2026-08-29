import unittest
from pathlib import Path


class ReleaseWorkflowTests(unittest.TestCase):
    def test_checks_the_remote_peeled_tag_ref(self):
        workflow = Path(".github/workflows/release.yml").read_text(encoding="utf-8")

        self.assertIn(
            'git ls-remote --tags origin "refs/tags/${GITHUB_REF_NAME}^{}"',
            workflow,
        )


if __name__ == "__main__":
    unittest.main()
