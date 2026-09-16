#!/usr/bin/env bash
# Output GitHub Actions fields for the two supported release channels.
set -euo pipefail
TAG="${1:-}"
CORE='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
if [[ "$TAG" =~ ^v${CORE}$ ]]; then
  CHANNEL=stable
  PRERELEASE=false
elif [[ "$TAG" =~ ^v${CORE}-preview\.[1-9][0-9]*$ ]]; then
  CHANNEL=preview
  PRERELEASE=true
else
  echo "Invalid tag '$TAG': expected vX.Y.Z or vX.Y.Z-preview.N (N >= 1)" >&2
  exit 1
fi
printf 'tag=%s\nversion=%s\nchannel=%s\nprerelease=%s\n' "$TAG" "${TAG#v}" "$CHANNEL" "$PRERELEASE"
