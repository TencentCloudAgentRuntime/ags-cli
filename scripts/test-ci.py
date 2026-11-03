#!/usr/bin/env python3
"""Exercise CI routing, gates and shard completeness without cloud credentials."""
import json
import os
from fnmatch import fnmatchcase
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
WORKFLOW = (ROOT / ".github/workflows/ci.yml").read_text()
GATE = WORKFLOW.split("      - name: Verify all required CI jobs succeeded\n", 1)[1]
GATE = GATE.split("        run: |\n", 1)[1]
GATE = "\n".join(line[10:] for line in GATE.splitlines() if line.startswith("          "))
# Ruby and Node are preinstalled on the Ubuntu runner; no test dependencies need downloading.
CONFIG = json.loads(subprocess.check_output(
    ["ruby", "-ryaml", "-rjson", "-e", "puts JSON.generate(YAML.load(STDIN.read))"],
    input=WORKFLOW, text=True))
# Psych uses YAML 1.1, where the unquoted `on` key is a boolean.
TRIGGERS = CONFIG.get("on", CONFIG.get("true"))


def evaluate(expression, github, steps=None):
    expression = expression.removeprefix("${{").removesuffix("}}")
    script = ('const [expression, github, steps] = JSON.parse(process.argv[1]);'
              'console.log(JSON.stringify(Function("github", "steps", "return (" + expression + ");")(github, steps)));')
    return json.loads(subprocess.check_output(
        ["node", "-e", script, json.dumps([expression, github, steps or {}])], text=True))


def context(event="pull_request", ref="refs/pull/164/merge", changed_files=2, run_id=1):
    return dict(workflow="CI", event_name=event, ref=ref,
                ref_type="tag" if ref.startswith("refs/tags/") else "branch",
                ref_name=ref.split("/", 2)[-1], run_id=run_id,
                event=dict(pull_request=dict(number=164, changed_files=changed_files))
                if event == "pull_request" else dict(pull_request={}))


class CIRoutingTest(unittest.TestCase):
    def test_only_one_automatic_ci_per_pr_update(self):
        push = TRIGGERS["push"] or {}
        for ref, expected in [("refs/heads/codex/ci-speedup", False),
                              ("refs/heads/feature/example", False),
                              ("refs/heads/main", True), ("refs/heads/master", True),
                              ("refs/tags/v0.7.1", True), ("refs/tags/test/nested", True)]:
            with self.subTest(ref=ref):
                kind = "tags" if ref.startswith("refs/tags/") else "branches"
                patterns = push.get(kind, [] if "branches" in push or "tags" in push else ["**"])
                self.assertEqual(any(fnmatchcase(ref.split("/", 2)[-1], p) for p in patterns), expected)
        self.assertIn("synchronize", TRIGGERS["pull_request"]["types"])
        self.assertIn("workflow_dispatch", TRIGGERS)

    def test_large_pr_cannot_skip_required_tests(self):
        changes = CONFIG["jobs"]["changes"]
        filter_step = next(step for step in changes["steps"] if step.get("id") == "filter")
        docs_only = dict(filter=dict(outputs=dict(code="false", changelog="false")))
        for count in (2999, 3000, 3001):
            with self.subTest(changed_files=count):
                github = context(changed_files=count)
                # The filter is skipped at the cap, so it has no outputs.
                run_ci = evaluate(changes["outputs"]["ci"], github, docs_only if count < 3000 else {})
                self.assertEqual(run_ci, count >= 3000)
                self.assertEqual(evaluate(filter_step.get("if", "true"), github), count < 3000)
                needs = dict(changes=dict(result="success"), test=dict(result="skipped"))
                result = subprocess.run(["bash", "-euo", "pipefail", "-c", GATE], capture_output=True,
                                        env=dict(os.environ, RUN_CI=str(run_ci).lower(), NEEDS=json.dumps(needs)))
                self.assertEqual(result.returncode == 0, count < 3000)

    def test_small_pr_preserves_code_changelog_and_docs_routing(self):
        for code, changelog, expected in [("false", "false", False),
                                          ("true", "false", True), ("false", "true", True)]:
            with self.subTest(code=code, changelog=changelog):
                steps = dict(filter=dict(outputs=dict(code=code, changelog=changelog)))
                self.assertEqual(evaluate(CONFIG["jobs"]["changes"]["outputs"]["ci"], context(), steps), expected)

    def test_main_tag_and_manual_runs_always_validate(self):
        docs_only = dict(filter=dict(outputs=dict(code="false", changelog="false")))
        for event, ref in [("push", "refs/heads/main"), ("push", "refs/heads/master"),
                           ("push", "refs/tags/v0.7.1"), ("workflow_dispatch", "refs/heads/feature/example")]:
            with self.subTest(event=event, ref=ref):
                self.assertTrue(evaluate(CONFIG["jobs"]["changes"]["outputs"]["ci"], context(event, ref), docs_only))

    def test_main_commits_have_independent_non_cancelling_groups(self):
        for ref in ("refs/heads/main", "refs/heads/master"):
            groups = []
            for run_id in (1, 2):
                github = context("push", ref, run_id=run_id)
                self.assertFalse(evaluate(CONFIG["concurrency"]["cancel-in-progress"], github))
                groups.append(re.sub(r"\$\{\{(.*?)\}\}",
                                     lambda m: str(evaluate(m[1], github)), CONFIG["concurrency"]["group"]))
            self.assertNotEqual(*groups)

    def test_new_pr_run_cancels_older_run_of_the_same_pr(self):
        groups = []
        for run_id in (1, 2):
            github = context(run_id=run_id)
            self.assertTrue(evaluate(CONFIG["concurrency"]["cancel-in-progress"], github))
            groups.append(re.sub(r"\$\{\{(.*?)\}\}",
                                 lambda m: str(evaluate(m[1], github)), CONFIG["concurrency"]["group"]))
        self.assertEqual(*groups)


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
