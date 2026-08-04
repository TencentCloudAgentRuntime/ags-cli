#!/usr/bin/env sh
set -eu

REPO_ROOT="$(unset CDPATH; cd -- "$(dirname "$0")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

case "${1:-}" in
    "")                 require_fallback=false ;;
    --require-fallback) require_fallback=true ;;
    *)                  fail "unknown argument: $1" ;;
esac

assert_contains() {
    file="$1"
    expected="$2"
    grep -F "$expected" "$file" >/dev/null 2>&1 || {
        echo "Expected output to contain: $expected" >&2
        cat "$file" >&2
        fail "missing expected output"
    }
}

assert_not_contains() {
    file="$1"
    unexpected="$2"
    if grep -F "$unexpected" "$file" >/dev/null 2>&1; then
        echo "Expected output not to contain: $unexpected" >&2
        cat "$file" >&2
        fail "found unexpected output"
    fi
}

case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    *)      fail "unsupported test OS" ;;
esac

case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)             fail "unsupported test architecture" ;;
esac

version="0.0.0-test"
tag="v${version}"
filename="agr-${version}-${os}-${arch}.tar.gz"
mirror_root="${TEST_ROOT}/mirror"
release_dir="${mirror_root}/${tag}"
payload_dir="${TEST_ROOT}/payload"
fake_bin_dir="${TEST_ROOT}/fake-bin"

mkdir -p "$release_dir" "$payload_dir" "$fake_bin_dir"

cat >"${payload_dir}/agr" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "version" ]; then
    echo "agr version v0.0.0-test"
fi
EOF
chmod +x "${payload_dir}/agr"
tar -czf "${release_dir}/${filename}" -C "$payload_dir" agr

if command -v shasum >/dev/null 2>&1; then
    checksum="$(shasum -a 256 "${release_dir}/${filename}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
    checksum="$(sha256sum "${release_dir}/${filename}" | awk '{print $1}')"
else
    fail "shasum or sha256sum is required"
fi
printf '%s  %s\n' "$checksum" "$filename" >"${release_dir}/checksums.txt"

cat >"${fake_bin_dir}/sudo" <<'EOF'
#!/usr/bin/env sh
echo "unexpected sudo invocation" >&2
exit 99
EOF
chmod +x "${fake_bin_dir}/sudo"

run_installer() {
    test_home="$1"
    output_file="$2"
    install_dir="${3:-}"
    install_path="${4:-${fake_bin_dir}:${PATH}}"

    if [ -n "$install_dir" ]; then
        env \
            HOME="$test_home" \
            PATH="$install_path" \
            VERSION="$tag" \
            AGR_DOWNLOAD_BASE_URL="file://${mirror_root}" \
            INSTALL_DIR="$install_dir" \
            sh "${REPO_ROOT}/install.sh" >"$output_file" 2>&1
    else
        env -u INSTALL_DIR \
            HOME="$test_home" \
            PATH="$install_path" \
            VERSION="$tag" \
            AGR_DOWNLOAD_BASE_URL="file://${mirror_root}" \
            sh "${REPO_ROOT}/install.sh" >"$output_file" 2>&1
    fi
}

explicit_home="${TEST_ROOT}/explicit-home"
explicit_dir="${explicit_home}/custom/bin"
explicit_output="${TEST_ROOT}/explicit.out"
mkdir -p "$explicit_home"

if ! run_installer "$explicit_home" "$explicit_output" "$explicit_dir"; then
    cat "$explicit_output" >&2
    fail "explicit INSTALL_DIR installation failed"
fi
[ -x "${explicit_dir}/agr" ] || fail "explicit INSTALL_DIR binary is missing"
assert_contains "$explicit_output" "Command:  ${explicit_dir}/agr"
assert_contains "$explicit_output" "not in PATH"
echo "PASS: explicit INSTALL_DIR is created without sudo"

if [ -w /usr/local/bin ]; then
    if [ "$require_fallback" = true ]; then
        fail "fallback coverage required, but /usr/local/bin is writable"
    fi
    echo "SKIP: default fallback test (/usr/local/bin is writable)"
else
    fallback_home="${TEST_ROOT}/fallback-home"
    fallback_dir="${fallback_home}/.local/bin"
    fallback_output="${TEST_ROOT}/fallback.out"
    fallback_path="${fallback_dir}:${fake_bin_dir}:${PATH}"
    mkdir -p "$fallback_home"

    if ! run_installer "$fallback_home" "$fallback_output" "" "$fallback_path"; then
        cat "$fallback_output" >&2
        fail "default user-local fallback installation failed"
    fi
    [ -x "${fallback_dir}/agr" ] || fail "fallback binary is missing"
    assert_contains "$fallback_output" "Command:  ${fallback_dir}/agr"
    assert_not_contains "$fallback_output" "not in PATH"
    fallback_version="$(env PATH="$fallback_path" agr version)"
    [ "$fallback_version" = "agr version v0.0.0-test" ] || fail "fallback binary is not executable through PATH"
    echo "PASS: unwritable system directory falls back without sudo and is executable through PATH"
fi
