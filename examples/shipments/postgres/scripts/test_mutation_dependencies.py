"""Dependency setup stays outside the fail-closed evidence classifier."""

import subprocess
from pathlib import Path
import unittest
from unittest.mock import patch

from mutation_dependencies import prepare


class DependencyTests(unittest.TestCase):
    @patch("mutation_dependencies.subprocess.run")
    def test_cold_downloads_stay_outside_evidence(self, run):
        environment = {"GOMODCACHE": "/empty/task-cache", "GOWORK": "off"}
        prepare(Path("consumer"), environment)
        run.assert_called_once_with(
            ["go", "mod", "download", "all"],
            cwd=Path("consumer"),
            env=environment,
            check=True,
            timeout=300,
        )

    @patch("mutation_dependencies.subprocess.run")
    def test_setup_failure_and_timeout_cannot_produce_sql_evidence(self, run):
        for error in (
            subprocess.CalledProcessError(1, "go mod download"),
            subprocess.TimeoutExpired("go mod download", 300),
        ):
            with self.subTest(error=error), self.assertRaises(SystemExit):
                run.side_effect = error
                prepare(Path("consumer"), {})


if __name__ == "__main__":
    unittest.main()
