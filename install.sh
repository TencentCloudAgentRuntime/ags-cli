#!/usr/bin/env sh
# install.sh - one-line installer for AGR CLI.
# Default source: official (https://dl.tencentags.com/agr-cli)
# GitHub fallback:
#   curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | AGR_DOWNLOAD_MIRROR=github sh
set -eu

REPO="TencentCloudAgentRuntime/ags-cli"
BINARY="agr"
AGR_DOWNLOAD_MIRROR="${AGR_DOWNLOAD_MIRROR:-official}"
OFFICIAL_BASE_URL="${AGR_DOWNLOAD_BASE_URL:-https://dl.tencentags.com/agr-cli}"

if [ -n "${INSTALL_DIR:-}" ]; then
    : # Honor the caller's explicit install directory.
elif [ -w /usr/local/bin ]; then
    INSTALL_DIR="/usr/local/bin"
elif [ -n "${HOME:-}" ]; then
    INSTALL_DIR="${HOME}/.local/bin"
else
    echo "Error: no writable install directory found and HOME is not set." >&2
    echo "Set INSTALL_DIR to a writable directory and retry." >&2
    exit 1
fi

download() {
    url="$1"
    output="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fSL -o "$output" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$output" "$url"
    else
        echo "Error: curl or wget is required." >&2
        exit 1
    fi
}

download_quiet() {
    url="$1"
    output="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$output" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$output" "$url"
    else
        echo "Error: curl or wget is required." >&2
        exit 1
    fi
}

sha256_file() {
    path="$1"
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$path" | awk '{print $1}'
    elif command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$path" | awk '{print $1}'
    else
        echo "Error: shasum or sha256sum is required for checksum verification." >&2
        exit 1
    fi
}

verify_checksum() {
    checksums="$1"
    expected_path="$2"
    actual_file="$3"
    expected_sha="$(awk -v path="$expected_path" '$2 == path {print $1}' "$checksums")"
    if [ -z "$expected_sha" ]; then
        echo "Error: checksum for ${expected_path} not found." >&2
        exit 1
    fi
    actual_sha="$(sha256_file "$actual_file")"
    if [ "$expected_sha" != "$actual_sha" ]; then
        echo "Error: checksum mismatch for ${expected_path}." >&2
        echo "Expected: $expected_sha" >&2
        echo "Actual:   $actual_sha" >&2
        exit 1
    fi
}

# Detect platform.
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      echo "Error: unsupported OS '$OS'" >&2; exit 1 ;;
esac

case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             echo "Error: unsupported architecture '$ARCH'" >&2; exit 1 ;;
esac

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

case "$AGR_DOWNLOAD_MIRROR" in
    official|"")
        if [ -z "${VERSION:-}" ]; then
            download_quiet "${OFFICIAL_BASE_URL}/latest/VERSION" "$TMPDIR/VERSION"
            VERSION="$(tr -d '[:space:]' < "$TMPDIR/VERSION")"
        fi
        case "$VERSION" in
            v*) TAG="$VERSION"; VERSION_NUMBER="${VERSION#v}" ;;
            *)  TAG="v$VERSION"; VERSION_NUMBER="$VERSION" ;;
        esac
        FILENAME="${BINARY}-${VERSION_NUMBER}-${OS}-${ARCH}.tar.gz"
        DOWNLOAD_URL="${OFFICIAL_BASE_URL}/${TAG}/${FILENAME}"
        CHECKSUM_URL="${OFFICIAL_BASE_URL}/${TAG}/checksums.txt"

        echo "AGR CLI ${TAG} (${OS}/${ARCH})"
        echo "Source: official (${OFFICIAL_BASE_URL})"
        echo "Downloading ${DOWNLOAD_URL} ..."
        download "$DOWNLOAD_URL" "$TMPDIR/$FILENAME"
        download_quiet "$CHECKSUM_URL" "$TMPDIR/checksums.txt"
        verify_checksum "$TMPDIR/checksums.txt" "$FILENAME" "$TMPDIR/$FILENAME"
        tar xzf "$TMPDIR/$FILENAME" -C "$TMPDIR"
        BIN_NAME="$BINARY"
        ;;

    github|gh)
        if [ -z "${VERSION:-}" ]; then
            if command -v curl >/dev/null 2>&1; then
                VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null | sed 's|.*/tag/||')"
            elif command -v wget >/dev/null 2>&1; then
                VERSION="$(wget -qS -O /dev/null "https://github.com/${REPO}/releases/latest" 2>&1 | grep -i 'Location:' | tail -1 | sed 's|.*/tag/||' | tr -d '\r\n')"
            fi
        fi
        if [ -z "${VERSION:-}" ]; then
            echo "Error: could not determine the latest GitHub release version." >&2
            echo "Please retry with VERSION=vX.Y.Z." >&2
            exit 1
        fi
        case "$VERSION" in
            v*) TAG="$VERSION"; VERSION_NUMBER="${VERSION#v}" ;;
            *)  TAG="v$VERSION"; VERSION_NUMBER="$VERSION" ;;
        esac
        FILENAME="${BINARY}-${VERSION_NUMBER}-${OS}-${ARCH}.tar.gz"
        DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"
        CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

        echo "AGR CLI ${TAG} (${OS}/${ARCH})"
        echo "Source: GitHub Releases"
        echo "Downloading ${DOWNLOAD_URL} ..."
        download "$DOWNLOAD_URL" "$TMPDIR/$FILENAME"
        download_quiet "$CHECKSUM_URL" "$TMPDIR/checksums.txt"
        verify_checksum "$TMPDIR/checksums.txt" "$FILENAME" "$TMPDIR/$FILENAME"
        tar xzf "$TMPDIR/$FILENAME" -C "$TMPDIR"
        BIN_NAME="$BINARY"
        ;;

    *)
        echo "Error: unsupported AGR_DOWNLOAD_MIRROR '$AGR_DOWNLOAD_MIRROR'." >&2
        echo "Supported values: official, github." >&2
        exit 1
        ;;
esac

if [ ! -f "$TMPDIR/$BIN_NAME" ]; then
    echo "Error: binary '$BIN_NAME' not found." >&2
    exit 1
fi

chmod +x "$TMPDIR/$BIN_NAME"

mkdir -p "$INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ]; then
    echo "Error: install directory '$INSTALL_DIR' is not writable." >&2
    echo "Set INSTALL_DIR to a writable directory and retry." >&2
    exit 1
fi
mv "$TMPDIR/$BIN_NAME" "${INSTALL_DIR}/${BINARY}"

echo ""
echo "AGR CLI installed successfully!"
echo "  Command:  ${INSTALL_DIR}/${BINARY}"
echo "  Version:  $("${INSTALL_DIR}/${BINARY}" version 2>/dev/null | head -1 || echo "${TAG}")"
echo ""
case ":${PATH:-}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        echo "Note: ${INSTALL_DIR} is not in PATH."
        echo "Add it for the current shell with:"
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
        echo ""
        ;;
esac
echo "Next step: run 'agr init' to configure your credentials."
