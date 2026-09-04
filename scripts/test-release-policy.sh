#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
POLICY_SCRIPT="${SCRIPT_DIR}/release-policy.sh"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    echo "Expected command to fail: $*" >&2
    exit 1
  fi
}

bash "$POLICY_SCRIPT" validate-preview-core 0.7.0 \
  v0.6.6 v0.6.5 v0.7.0-preview.1
expect_failure bash "$POLICY_SCRIPT" validate-preview-core 0.6.6 \
  v0.6.6 v0.6.5
expect_failure bash "$POLICY_SCRIPT" validate-preview-core 0.5.2 \
  v0.6.6 v0.6.5
bash "$POLICY_SCRIPT" validate-preview-core 100000000000000000000.0.0 \
  v99999999999999999999.999.999
expect_failure bash "$POLICY_SCRIPT" validate-preview-core 99999999999999999999.999.999 \
  v100000000000000000000.0.0

release_commit="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
pulls_json="${TEST_ROOT}/pulls.json"
cat > "$pulls_json" <<EOF
[
  {
    "merged_at": "2026-09-03T00:00:00Z",
    "merge_commit_sha": "$release_commit",
    "base": {"ref": "main"},
    "head": {"ref": "release/v0.7.0"},
    "title": "chore(release): prepare v0.7.0"
  }
]
EOF

bash "$POLICY_SCRIPT" validate-release-pr \
  "$release_commit" main v0.7.0 "$pulls_json"
expect_failure bash "$POLICY_SCRIPT" validate-release-pr \
  bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb main v0.7.0 "$pulls_json"
expect_failure bash "$POLICY_SCRIPT" validate-release-pr \
  "$release_commit" preview v0.7.0 "$pulls_json"

echo "Release policy tests passed"
