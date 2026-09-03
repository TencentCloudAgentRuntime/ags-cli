#!/usr/bin/env bash

set -euo pipefail

CHANGELOG_FILE="${CHANGELOG_FILE:-CHANGELOG.md}"
RELEASE_NOTE_SECTIONS="${RELEASE_NOTE_SECTIONS:-Breaking Changes
Features
Bug Fixes
Docs}"
ZH_CHANGELOG_FILE="${ZH_CHANGELOG_FILE:-CHANGELOG-zh.md}"
ZH_RELEASE_NOTE_SECTIONS="${ZH_RELEASE_NOTE_SECTIONS:-破坏性变更
新功能
Bug 修复
文档}"
HEADING_RE='^## \[([0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z]+(\.[0-9]+)?)?)\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$'

usage() {
  cat <<'EOF'
Usage:
  scripts/changelog.sh validate [version]
  scripts/changelog.sh validate-pair [version]
  scripts/changelog.sh validate-release-notes <version>
  scripts/changelog.sh validate-release-notes-pair <version>
  scripts/changelog.sh validate-latest-release-notes
  scripts/changelog.sh validate-latest-release-notes-pair
  scripts/changelog.sh validate-optional-release-notes-pair
  scripts/changelog.sh extract <version> <output>

Commands:
  validate          Validate changelog heading format and duplicate versions.
                    If version is provided, also require that section to exist.
  validate-pair     Validate both CHANGELOG.md and CHANGELOG-zh.md.
  validate-release-notes
                    Validate the target version section uses the release-note
                    structure: Breaking Changes, Features, Bug Fixes, Docs.
  validate-release-notes-pair
                    Validate release-note sections in both changelogs.
  validate-latest-release-notes
                    Validate the latest changelog section uses the release-note
                    structure: Breaking Changes, Features, Bug Fixes, Docs.
  validate-latest-release-notes-pair
                    Validate the latest release-note section in both changelogs.
  validate-optional-release-notes-pair
                    Require both changelog files; if either has a version,
                    validate the pair and their latest release-note sections.
  extract           Write the requested version section body to the output file.

Notes:
  - Versions use X.Y.Z or X.Y.Z-preview.N format in changelog files.
  - Tags add a leading "v"; callers should strip it before invoking.
  - RELEASE_NOTE_SECTIONS can override the required section names, one per line.
  - ZH_CHANGELOG_FILE and ZH_RELEASE_NOTE_SECTIONS configure pair validation.
EOF
}

ensure_changelog_exists() {
  if [ ! -f "$CHANGELOG_FILE" ]; then
    echo "Missing changelog file: $CHANGELOG_FILE" >&2
    exit 1
  fi
}

latest_version() {
  ensure_changelog_exists

  awk '
    /^## \[/ {
      line = $0
      sub(/^## \[/, "", line)
      sub(/\] - .*/, "", line)
      print line
      exit
    }
  ' "$CHANGELOG_FILE"
}

validate() {
  local version="${1:-}"
  local headings versions duplicates

  ensure_changelog_exists

  headings="$(grep -n '^## \[' "$CHANGELOG_FILE" || true)"
  if [ -z "$headings" ]; then
    echo "No version headings found in $CHANGELOG_FILE" >&2
    exit 1
  fi

  if ! awk '
    /^## \[/ {
      if ($0 !~ /^## \[[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z]+(\.[0-9]+)?)?\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$/) {
        printf("Invalid changelog heading: %s\n", $0) > "/dev/stderr"
        bad = 1
      }
    }
    END { exit bad }
  ' "$CHANGELOG_FILE"; then
    exit 1
  fi

  versions="$(printf '%s\n' "$headings" | sed -E 's/^[0-9]+:## \[([^]]+)\].*/\1/')"
  duplicates="$(printf '%s\n' "$versions" | sort | uniq -d || true)"
  if [ -n "$duplicates" ]; then
    echo "Duplicate changelog versions found:" >&2
    printf '%s\n' "$duplicates" >&2
    exit 1
  fi

  if [ -n "$version" ] && ! grep -Eq "^## \[$version\] - " "$CHANGELOG_FILE"; then
    echo "Missing changelog section for version $version in $CHANGELOG_FILE" >&2
    exit 1
  fi
}

validate_release_notes() {
  local version sections

  if [ "$#" -ne 1 ]; then
    usage >&2
    exit 1
  fi

  version="$1"
  validate "$version"

  sections="$(awk -v ver="$version" '
    $0 ~ ("^## \\[" ver "\\] - ") { in_section = 1; next }
    /^## \[/ && in_section { exit }
    in_section && /^### / {
      sub(/^### /, "", $0)
      print
    }
  ' "$CHANGELOG_FILE")"

  if [ "$sections" != "$RELEASE_NOTE_SECTIONS" ]; then
    echo "Release notes for version $version must use exactly these sections in order:" >&2
    printf '%s\n' "$RELEASE_NOTE_SECTIONS" | sed 's/^/  ### /' >&2
    echo "Found:" >&2
    if [ -n "$sections" ]; then
      printf '  %s\n' "$sections" >&2
    else
      echo "  <none>" >&2
    fi
    exit 1
  fi
}

with_changelog() {
  local changelog_file="$1"
  local release_note_sections="$2"
  shift 2

  CHANGELOG_FILE="$changelog_file" RELEASE_NOTE_SECTIONS="$release_note_sections" "$@"
}

validate_pair() {
  local version="${1:-}"

  validate "$version"
  with_changelog "$ZH_CHANGELOG_FILE" "$ZH_RELEASE_NOTE_SECTIONS" validate "$version"
}

validate_release_notes_pair() {
  if [ "$#" -ne 1 ]; then
    usage >&2
    exit 1
  fi

  validate_release_notes "$1"
  with_changelog "$ZH_CHANGELOG_FILE" "$ZH_RELEASE_NOTE_SECTIONS" validate_release_notes "$1"
}

validate_latest_release_notes_pair() {
  local version

  version="$(latest_version)"
  validate_release_notes "$version"
  with_changelog "$ZH_CHANGELOG_FILE" "$ZH_RELEASE_NOTE_SECTIONS" validate_release_notes "$version"
}

validate_optional_release_notes_pair() {
  ensure_changelog_exists
  with_changelog "$ZH_CHANGELOG_FILE" "$ZH_RELEASE_NOTE_SECTIONS" ensure_changelog_exists

  if ! grep -q '^## \[' "$CHANGELOG_FILE" &&
     ! grep -q '^## \[' "$ZH_CHANGELOG_FILE"; then
    return
  fi

  validate_pair
  validate_latest_release_notes_pair
}

extract() {
  local version output

  if [ "$#" -ne 2 ]; then
    usage >&2
    exit 1
  fi

  version="$1"
  output="$2"

  validate "$version"

  awk -v ver="$version" '
    $0 ~ ("^## \\[" ver "\\] - ") { in_section = 1; next }
    /^## \[/ && in_section { exit }
    in_section { print }
  ' "$CHANGELOG_FILE" > "$output"

  if ! grep -q '[^[:space:]]' "$output"; then
    echo "Changelog section for version $version is empty" >&2
    exit 1
  fi
}

main() {
  if [ "$#" -lt 1 ]; then
    usage >&2
    exit 1
  fi

  case "$1" in
    validate)
      shift
      if [ "$#" -gt 1 ]; then
        usage >&2
        exit 1
      fi
      validate "${1:-}"
      ;;
    validate-pair)
      shift
      if [ "$#" -gt 1 ]; then
        usage >&2
        exit 1
      fi
      validate_pair "${1:-}"
      ;;
    validate-release-notes)
      shift
      validate_release_notes "$@"
      ;;
    validate-release-notes-pair)
      shift
      validate_release_notes_pair "$@"
      ;;
    validate-latest-release-notes)
      shift
      if [ "$#" -ne 0 ]; then
        usage >&2
        exit 1
      fi
      validate_release_notes "$(latest_version)"
      ;;
    validate-latest-release-notes-pair)
      shift
      if [ "$#" -ne 0 ]; then
        usage >&2
        exit 1
      fi
      validate_latest_release_notes_pair
      ;;
    validate-optional-release-notes-pair)
      shift
      if [ "$#" -ne 0 ]; then
        usage >&2
        exit 1
      fi
      validate_optional_release_notes_pair
      ;;
    extract)
      shift
      extract "$@"
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
