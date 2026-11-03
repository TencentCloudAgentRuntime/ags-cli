#!/usr/bin/env bash
set -euo pipefail

shard="${1:?usage: ci-tests.sh unit|scenarios}"
case "$shard" in
  unit|scenarios) ;;
  *) echo "unknown test shard: $shard" >&2; exit 1 ;;
esac

# These packages build and exercise real CLI binaries. Run them on a separate
# machine so they do not compete with the main race/coverage suite for CPUs.
# Every other package, including newly added ones, belongs to the unit shard.
all_packages=$(go list ./...)
packages=()
while IFS= read -r package; do
  case "$package" in
    */tests/integ) continue ;;
    */internal/patchscenarios|*/tests/channels) package_shard=scenarios ;;
    *) package_shard=unit ;;
  esac
  if [[ "$package_shard" == "$shard" ]]; then
    packages+=("$package")
  fi
done <<<"$all_packages"

go test -v -race -coverprofile=coverage.out -count=1 "${packages[@]}"
if [[ "$shard" == unit ]]; then
  # Keep the credential-free integration suite's explicit 30-second deadline,
  # race detection and coverage, without running it again in a separate job.
  go test -v -race -coverprofile=coverage-integ.out -count=1 -timeout 30s ./tests/integ/
fi
