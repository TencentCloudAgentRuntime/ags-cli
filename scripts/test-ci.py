#!/usr/bin/env python3
"""Exercise the actual CI gate and shard runner without cloud credentials."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
WORKFLOW = (ROOT / ".github/workflows/ci.yml").read_text()
GATE = WORKFLOW.split("      - name: Verify all required CI jobs succeeded\n", 1)[1]
GATE = GATE.split("        run: |\n", 1)[1]
GATE = "\n".join(line[10:] for line in GATE.splitlines() if line.startswith("          "))


class CIGateTest(unittest.TestCase):
    def test_gate_fails_closed(self):
        cases = [
            ("true", "success", "success", True),
            ("false", "success", "skipped", True),
            ("true", "success", "skipped", False),
            ("true", "success", "failure", False),
            ("true", "success", "cancelled", False),
            ("false", "failure", "skipped", False),
            ("false", "cancelled", "skipped", False),
            ("false", "success", "failure", False),
            ("", "success", "skipped", False),
        ]
        for run_ci, changes, test, success in cases:
            with self.subTest(run_ci=run_ci, changes=changes, test=test):
                needs = dict(changes=dict(result=changes), test=dict(result=test))
                env = dict(os.environ, RUN_CI=run_ci, NEEDS=json.dumps(needs))
                result = subprocess.run(["bash", "-euo", "pipefail", "-c", GATE],
                                        env=env, capture_output=True)
                self.assertEqual(result.returncode == 0, success)


class ShardTest(unittest.TestCase):
    def run_shard(self, directory, shard, list_status=0):
        root = Path(directory)
        go = root / "go"
        go.write_text('#!/usr/bin/env python3\n'
                      'import json, os, sys\n'
                      'if sys.argv[1] == "list":\n'
                      '    print(os.environ["PACKAGES"])\n'
                      '    sys.exit(int(os.environ["LIST_STATUS"]))\n'
                      'with open(os.environ["CALLS"], "a") as output:\n'
                      '    output.write(json.dumps(sys.argv[1:]) + "\\n")\n')
        go.chmod(0o755)
        calls = root / "calls"
        calls.unlink(missing_ok=True)
        # Include a future package: additions must not silently escape both shards.
        packages = ["example/cmd/agr", "example/internal/cli", "example/internal/commands",
                    "example/internal/commands/new/command", "example/internal/patchscenarios",
                    "example/tests/channels", "example/tests/integ", "example/new/package"]
        env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                   PACKAGES="\n".join(packages), CALLS=str(calls), LIST_STATUS=str(list_status))
        result = subprocess.run(["bash", str(ROOT / "scripts/ci-tests.sh"), shard],
                                env=env, capture_output=True)
        return result, [json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else [], packages

    def test_shards_cover_every_package_exactly_once(self):
        selected = []
        with tempfile.TemporaryDirectory() as directory:
            for shard in ("unit", "scenarios"):
                result, calls, packages = self.run_shard(directory, shard)
                self.assertEqual(result.returncode, 0, result.stderr)
                for call in calls:
                    self.assertEqual(call[0], "test")
                    self.assertIn("-race", call)
                    self.assertIn("-count=1", call)
                    self.assertTrue(any(arg.startswith("-coverprofile=") for arg in call))
                    selected.extend("example/tests/integ" if arg == "./tests/integ/" else arg
                                    for arg in call if arg.startswith(("example/", "./tests/integ/")))
                    if "./tests/integ/" in call:
                        self.assertIn("-timeout", call)
                        self.assertEqual(call[call.index("-timeout") + 1], "30s")
        self.assertCountEqual(selected, packages)

    def test_invalid_shard_and_failed_discovery_do_not_run_tests(self):
        with tempfile.TemporaryDirectory() as directory:
            for shard, status in (("typo", 0), ("unit", 1)):
                result, calls, _ = self.run_shard(directory, shard, status)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])


if __name__ == "__main__":
    unittest.main()
