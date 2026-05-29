#!/usr/bin/env sh
# install.sh — one-line installer for AGR CLI
# Usage: curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | sh
set -eu

REPO="TencentCloudAgentRuntime/ags-cli"
BINARY="agr"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# ── Detect platform ──────────────────────────────────────────────────
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

# ── Determine latest version ─────────────────────────────────────────
# Method 1: GitHub release redirect (no API quota needed)
if [ -z "${LATEST:-}" ] && command -v curl >/dev/null 2>&1; then
    LATEST="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null | sed 's|.*/tag/||')"
fi
if [ -z "${LATEST:-}" ] && command -v wget >/dev/null 2>&1; then
    LATEST="$(wget -qS -O /dev/null "https://github.com/${REPO}/releases/latest" 2>&1 | grep -i 'Location:' | tail -1 | sed 's|.*/tag/||' | tr -d '\r\n')"
fi

# Method 2: GitHub API (fallback, subject to rate limits)
if [ -z "${LATEST:-}" ] && command -v curl >/dev/null 2>&1; then
    LATEST="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
fi
if [ -z "${LATEST:-}" ] && command -v wget >/dev/null 2>&1; then
    LATEST="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
fi

if [ -z "${LATEST:-}" ]; then
    echo "Error: could not determine the latest version." >&2
    echo "Please specify the version manually, e.g.:" >&2
    echo "  curl -fLO https://github.com/${REPO}/releases/download/v0.5.0/agr-0.5.0-${OS}-${ARCH}.tar.gz" >&2
    exit 1
fi

VERSION="${LATEST#v}"
FILENAME="${BINARY}-${VERSION}-${OS}-${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${FILENAME}"

# ── Download and install ─────────────────────────────────────────────
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "AGR CLI ${LATEST} (${OS}/${ARCH})"
echo "Downloading ${DOWNLOAD_URL} ..."

if command -v curl >/dev/null 2>&1; then
    curl -fSL -o "$TMPDIR/$FILENAME" "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMPDIR/$FILENAME" "$DOWNLOAD_URL"
else
    echo "Error: curl or wget is required." >&2
    exit 1
fi

tar xzf "$TMPDIR/$FILENAME" -C "$TMPDIR"

if [ ! -f "$TMPDIR/$BINARY" ]; then
    echo "Error: binary '$BINARY' not found in archive." >&2
    exit 1
fi

chmod +x "$TMPDIR/$BINARY"

if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPDIR/$BINARY" "${INSTALL_DIR}/${BINARY}"
else
    echo "Installing to ${INSTALL_DIR}/${BINARY} (requires sudo) ..."
    sudo mv "$TMPDIR/$BINARY" "${INSTALL_DIR}/${BINARY}"
fi

echo ""
echo "AGR CLI installed successfully!"
echo "  Command:  ${INSTALL_DIR}/${BINARY}"
echo "  Version:  $(${INSTALL_DIR}/${BINARY} version 2>/dev/null | head -1 || echo "${LATEST}")"
echo ""
echo "Next step: run 'agr init' to configure your credentials."
