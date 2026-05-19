#!/usr/bin/env bash

# tmux-process-monitor install script
# Detects OS + architecture, downloads the pre-built binary from GitHub Releases.
# Falls back with instructions to build from source.

set -euo pipefail

REPO="den-tanui/tmux-process-monitor"
PLUGIN_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$PLUGIN_DIR/bin"

mkdir -p "$BIN_DIR"

# ── Detect OS ──────────────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
	linux)  OS="linux" ;;
	darwin) OS="darwin" ;;
	*)
		echo "[tmux-process-monitor] Unsupported OS: $OS"
		echo "  Build from source:  cd \"$PLUGIN_DIR\" && make install"
		exit 1
		;;
esac

# ── Detect architecture ────────────────────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
	x86_64)          ARCH="amd64" ;;
	aarch64 | arm64) ARCH="arm64" ;;
	*)
		echo "[tmux-process-monitor] Unsupported architecture: $ARCH"
		echo "  Build from source:  cd \"$PLUGIN_DIR\" && make install"
		exit 1
		;;
esac

BINARY_NAME="tmux-process-monitor-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}"
DEST="$BIN_DIR/tmux-process-monitor"

echo "[tmux-process-monitor] Downloading ${BINARY_NAME} ..."

if command -v curl >/dev/null 2>&1; then
	curl -fsSL "$URL" -o "$DEST"
elif command -v wget >/dev/null 2>&1; then
	wget -qO "$DEST" "$URL"
else
	echo "[tmux-process-monitor] Error: curl or wget is required."
	echo "  Build from source:  cd \"$PLUGIN_DIR\" && make install"
	exit 1
fi

chmod +x "$DEST"
echo "[tmux-process-monitor] Installed to $DEST"
