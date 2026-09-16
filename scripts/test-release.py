#!/usr/bin/env python3
"""Exercise release tag routing and the workflow's actual publish shell offline."""
import os
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
WORKFLOW = (ROOT / ".github/workflows/release.yml").read_text()


def step(name):
    marker = f"      - name: {name}\n"
    return WORKFLOW.split(marker, 1)[1].split("\n      - name:", 1)[0]


def classify(tag):
    return subprocess.run(["bash", str(ROOT / "scripts/release-channel.sh"), tag], capture_output=True, text=True)


class ReleaseRoutingTest(unittest.TestCase):
    def test_channels(self):
        for tag, channel, prerelease in [("v0.6.7", "stable", "false"), ("v0.7.0-preview.1", "preview", "true"), ("v0.7.0-preview.10", "preview", "true")]:
            with self.subTest(tag=tag):
                result = classify(tag)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(dict(line.split("=", 1) for line in result.stdout.splitlines()),
                                 dict(tag=tag, version=tag[1:], channel=channel, prerelease=prerelease))

    def test_invalid_tags(self):
        for tag in ["", "0.7.0", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-preview.0", "v1.2.3-preview.01", "v1.2.3-beta.1", "v1.2.3+meta", "v1.2.3-preview.1/extra"]:
            with self.subTest(tag=tag):
                result = classify(tag)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")

    def test_workflow_uses_classifier_and_stable_only_homebrew(self):
        self.assertIn('bash scripts/release-channel.sh "$TAG" >> "$GITHUB_OUTPUT"', step("Validate tag and select release channel"))
        for name in ["Create Homebrew Tap app token", "Trigger Homebrew Tap update"]:
            self.assertIn("if: ${{ steps.current.outputs.eligible == 'true' }}", step(name))
        draft = step("Create draft GitHub Release and upload artifacts")
        self.assertIn("draft: true", draft)
        self.assertIn("prerelease: ${{ steps.version.outputs.prerelease == 'true' }}", draft)
        self.assertIn('make_latest: "false"', draft)
        self.assertLess(WORKFLOW.index("Create draft GitHub Release"), WORKFLOW.index("Publish GitHub Release"))

    def test_published_release_cannot_be_redrafted(self):
        shell = step("Refuse to rewrite a published release").split("        run: |\n", 1)[1]
        shell = "\n".join(line[10:] for line in shell.splitlines() if line.startswith("          "))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            gh = root / "gh"
            gh.write_text('#!/bin/sh\nprintf "%s\\n" "$REMOTE_RELEASES"\nexit "$API_EXIT"\n')
            gh.chmod(0o755)
            for releases, api_exit, success in [("", "0", True), ("v0.7.0\ttrue", "0", True),
                                                ("v0.7.0\tfalse", "0", False), ("", "1", False)]:
                env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], TAG="v0.7.0",
                           GITHUB_REPOSITORY="owner/repo", REMOTE_RELEASES=releases, API_EXIT=api_exit)
                result = subprocess.run(["bash", "-euo", "pipefail", "-c", shell], env=env, capture_output=True)
                self.assertEqual(result.returncode == 0, success)

    def test_homebrew_retry_is_a_separate_job(self):
        self.assertIn("\n  homebrew:\n    needs: release", WORKFLOW)
        homebrew = WORKFLOW.split("\n  homebrew:\n", 1)[1]
        self.assertNotIn("Refuse to rewrite", homebrew)
        self.assertNotIn("Build release artifacts", homebrew)

    def test_publish_rechecks_newer_stable_before_marking_latest(self):
        publish = step("Publish GitHub Release")
        self.assertIn("scripts/release-latest.py", publish)
        self.assertNotIn('--draft=false --latest\n', publish)
        concurrency = WORKFLOW.split("\nconcurrency:\n", 1)[1].split("\njobs:", 1)[0]
        self.assertIn("group: agr-release-distribution", concurrency)
        self.assertIn("cancel-in-progress: false", concurrency)
        self.assertIn("queue: max", concurrency)
        self.assertNotIn("${{", concurrency)

    def run_step(self, name, env):
        shell = step(name).split("        run: |\n", 1)[1]
        shell = "\n".join(line[10:] for line in shell.splitlines() if line.startswith("          "))
        return subprocess.run(["bash", "-euo", "pipefail", "-c", shell], env=env, cwd=ROOT, capture_output=True, text=True)

    def test_actual_publish_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            gh = root / "gh"
            gh.write_text('#!/bin/sh\nif [ "$1" = api ]; then printf "%s\\n" "$REMOTE_RELEASES"; exit "$API_EXIT"; fi\nprintf "%s\\n" "$@" > "$CALLS"\n')
            gh.chmod(0o755)
            for tag, current, expected in [("v0.6.7", "v0.6.8", "false"), ("v0.6.10", "v0.6.9", "true"),
                                           ("v0.6.8", "v0.6.8", "true"), ("v0.7.0-preview.1", "v0.6.8", "false")]:
                fields = dict(line.split("=", 1) for line in classify(tag).stdout.splitlines())
                # A larger preview and draft must not block a stable release.
                releases = [[dict(tag_name=current, draft=False, prerelease=False)],
                            [dict(tag_name="v9.0.0-preview.1", draft=False, prerelease=True),
                             dict(tag_name="v9.0.0", draft=True, prerelease=False)]]
                env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], TAG=tag,
                           CHANNEL=fields["channel"], CALLS=str(root / "calls"), API_EXIT="0",
                           GITHUB_REPOSITORY="owner/repo", REMOTE_RELEASES=json.dumps(releases))
                result = self.run_step("Publish GitHub Release", env)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((root / "calls").read_text().splitlines(),
                                 ["release", "edit", tag, "--draft=false", "--latest=" + expected])
            (root / "calls").unlink()
            result = self.run_step("Publish GitHub Release", dict(env, CHANNEL="stable", TAG="v0.6.8", API_EXIT="1"))
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / "calls").exists())

    def test_homebrew_failure_retry_and_stale_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            gh = root / "gh"
            gh.write_text('#!/bin/sh\nif [ "$1" = api ]; then printf "%s\\n" "$REMOTE_RELEASES"; exit "$API_EXIT"; fi\nprintf "%s\\n" "$@" >> "$CALLS"\nexit "$DISPATCH_EXIT"\n')
            gh.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], TAG="v0.6.8",
                       GITHUB_REPOSITORY="owner/repo", API_EXIT="0", DISPATCH_EXIT="1",
                       GITHUB_OUTPUT=str(root / "output"), CALLS=str(root / "calls"),
                       REMOTE_RELEASES=json.dumps([[dict(tag_name="v0.6.8", draft=False, prerelease=False)]]))
            result = self.run_step("Check Homebrew target is still current", env)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "output").read_text(), "eligible=true\n")
            self.assertNotEqual(self.run_step("Trigger Homebrew Tap update", env).returncode, 0)
            self.assertEqual(self.run_step("Trigger Homebrew Tap update", dict(env, DISPATCH_EXIT="0")).returncode, 0)
            self.assertNotIn("release\nedit", (root / "calls").read_text())
            (root / "output").unlink()
            newer = json.dumps([[dict(tag_name="v0.6.9", draft=False, prerelease=False)]])
            result = self.run_step("Check Homebrew target is still current", dict(env, REMOTE_RELEASES=newer))
            self.assertEqual(result.returncode, 0)
            self.assertEqual((root / "output").read_text(), "eligible=false\n")
            (root / "output").unlink()
            self.assertNotEqual(self.run_step("Check Homebrew target is still current", dict(env, API_EXIT="1")).returncode, 0)
            self.assertFalse((root / "output").exists())


if __name__ == "__main__":
    unittest.main()
