#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

CORE_VERSION_RE='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
STABLE_TAG_RE='^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$'

usage() {
  cat <<'EOF'
Usage:
  scripts/release-policy.sh validate-preview-core <core-version> [stable-tag ...]
  scripts/release-policy.sh validate-release-pr <commit> <base-branch> <tag> <pulls-json>

Commands:
  validate-preview-core
                    Require the preview core version to be newer than every
                    stable tag. Without tag arguments, read tags from Git.
  validate-release-pr
                    Require the release commit to be the exact merge commit of
                    the expected release pull request.
EOF
}

version_gt() {
  local left="$1"
  local right="$2"
  local index left_part right_part
  local -a left_parts right_parts

  IFS=. read -r -a left_parts <<< "$left"
  IFS=. read -r -a right_parts <<< "$right"
  for index in 0 1 2; do
    left_part="${left_parts[$index]}"
    right_part="${right_parts[$index]}"
    if [ "$left_part" = "$right_part" ]; then
      continue
    fi
    if [ "${#left_part}" -gt "${#right_part}" ] ||
       { [ "${#left_part}" -eq "${#right_part}" ] &&
         [[ "$left_part" > "$right_part" ]]; }; then
      return 0
    fi
    return 1
  done
  return 1
}

validate_preview_core() {
  local core_version="$1"
  shift
  local latest_stable=""
  local tag candidate
  local -a stable_tags=()

  if [[ ! "$core_version" =~ $CORE_VERSION_RE ]]; then
    echo "Invalid preview core version: $core_version" >&2
    exit 1
  fi

  if [ "$#" -eq 0 ]; then
    while IFS= read -r tag; do
      stable_tags+=("$tag")
    done < <(git tag --list 'v*')
  else
    stable_tags=("$@")
  fi

  for tag in "${stable_tags[@]}"; do
    if [[ ! "$tag" =~ $STABLE_TAG_RE ]]; then
      continue
    fi
    candidate="${BASH_REMATCH[1]}"
    if [ -z "$latest_stable" ] || version_gt "$candidate" "$latest_stable"; then
      latest_stable="$candidate"
    fi
  done

  if [ -n "$latest_stable" ] && ! version_gt "$core_version" "$latest_stable"; then
    echo "Preview core version $core_version must be newer than latest stable version $latest_stable" >&2
    exit 1
  fi

  if [ -n "$latest_stable" ]; then
    echo "Preview core version $core_version is newer than latest stable version $latest_stable"
  else
    echo "Preview core version $core_version accepted; no stable tags found"
  fi
}

validate_release_pr() {
  local commit="$1"
  local base_branch="$2"
  local tag="$3"
  local pulls_json="$4"
  local expected_head="release/${tag}"
  local expected_title="chore(release): prepare ${tag}"

  if ! jq -e \
    --arg commit "$commit" \
    --arg base "$base_branch" \
    --arg head "$expected_head" \
    --arg title "$expected_title" \
    '[.[] | select(
      .merged_at != null and
      .merge_commit_sha == $commit and
      .base.ref == $base and
      .head.ref == $head and
      .title == $title
    )] | length == 1' \
    "$pulls_json" >/dev/null; then
    echo "Release commit $commit is not the exact merge commit of $expected_head into $base_branch with title '$expected_title'" >&2
    exit 1
  fi

  echo "Release commit $commit matches $expected_head into $base_branch"
}

main() {
  if [ "$#" -lt 1 ]; then
    usage >&2
    exit 1
  fi

  case "$1" in
    validate-preview-core)
      shift
      if [ "$#" -lt 1 ]; then
        usage >&2
        exit 1
      fi
      validate_preview_core "$@"
      ;;
    validate-release-pr)
      shift
      if [ "$#" -ne 4 ]; then
        usage >&2
        exit 1
      fi
      validate_release_pr "$@"
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
