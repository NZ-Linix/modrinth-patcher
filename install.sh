#!/usr/bin/env bash
#
# install.sh — installs the newest modrinth-patcher build from ./dist
# onto the system PATH as `modrinth-patcher`.
#
# Default destination: /usr/local/bin (sudo is used when needed).
# Override with: DEST_DIR=~/bin ./install.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$SCRIPT_DIR/dist"
DEST_DIR="${DEST_DIR:-/usr/local/bin}"

# --- 1. pick the binary for this architecture ------------------------------
ARCH="$(uname -m)"
case "$ARCH" in
	arm64 | aarch64)
		CANDIDATES=("modrinth-patcher-macos-arm64" "modrinth-patcher-macos-x64")
		;;
	x86_64 | amd64)
		CANDIDATES=("modrinth-patcher-macos-x64" "modrinth-patcher-macos-arm64")
		;;
	*)
		echo "error: unsupported architecture: $ARCH" >&2
		exit 1
		;;
esac

SRC=""
for c in "${CANDIDATES[@]}"; do
	f="$DIST_DIR/$c"
	if [[ -f "$f" ]]; then
		SRC="$f"
		break
	fi
done
if [[ -z "$SRC" ]]; then
	echo "error: no macOS binary found in $DIST_DIR (build one first: go build)" >&2
	exit 1
fi

# --- 2. install (always overwrites with the current dist build) ------------
DEST="$DEST_DIR/modrinth-patcher"

mkdir -p "$DEST_DIR"
if [[ -w "$DEST_DIR" ]]; then
	install -m 755 "$SRC" "$DEST"
else
	echo "need admin to write $DEST_DIR — using sudo"
	sudo install -m 755 "$SRC" "$DEST"
fi

# --- 3. verify --------------------------------------------------------------
if "$DEST" --version >/dev/null 2>&1; then
	echo "installed: $SRC -> $DEST"
	"$DEST" --version
else
	echo "warning: installed but '--version' failed" >&2
fi
