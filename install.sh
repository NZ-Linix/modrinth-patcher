#!/usr/bin/env bash
#
# modrinth-patcher installer (macOS)
#
# Installs the newest modrinth-patcher build, quits the Modrinth App if it is
# running, patches ads out, and relaunches the app.
#
# Run locally:   ./install.sh
# Run remotely:  curl -fsSL <url> | bash        (see README for the one-liner)
#
# Environment overrides:
#   DEST_DIR=~/bin        install destination        (default /usr/local/bin)
#   MP_REPO=owner/repo    GitHub repo for downloads  (default NZ-Linix/modrinth-patcher)
#   MP_REF=main           branch/tag/commit          (default main)
#   GH_TOKEN=...          token for downloads (only needed for private forks)
#   MP_BINARY=/path       patch a specific app binary instead of auto-detecting
#   DRY_RUN=1             print actions without quitting/patching/relaunching
#
set -euo pipefail

REPO="${MP_REPO:-NZ-Linix/modrinth-patcher}"
REF="${MP_REF:-main}"
DEST_DIR="${DEST_DIR:-/usr/local/bin}"
DRY_RUN="${DRY_RUN:-0}"

# ── tiny TUI ────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
	C_GREEN=$'\e[32m'; C_CYAN=$'\e[36m'; C_YELLOW=$'\e[33m'
	C_RED=$'\e[31m'; C_BOLD=$'\e[1m'; C_DIM=$'\e[2m'; C_RESET=$'\e[0m'
else
	C_GREEN=""; C_CYAN=""; C_YELLOW=""; C_RED=""; C_BOLD=""; C_DIM=""; C_RESET=""
fi

info() { printf '%s%s%s\n' "$C_CYAN" "$*" "$C_RESET"; }
ok()   { printf '%s  ✔  %s%s\n' "$C_GREEN" "$*" "$C_RESET"; }
warn() { printf '%s  ⚠  %s%s\n' "$C_YELLOW" "$*" "$C_RESET" >&2; }
die()  { printf '%s  ✖  %s%s\n' "$C_RED" "$*" "$C_RESET" >&2; exit 1; }

banner() {
	printf '%s\n' "${C_BOLD}${C_CYAN}┌─────────────────────────────────────────────────┐"
	printf '│  %-47s │\n' "modrinth-patcher — remove ads from Modrinth App"
	printf '│  %-47s │\n' "https://github.com/$REPO"
	printf '%s\n' "└─────────────────────────────────────────────────┘${C_RESET}"
}

dots() { # dots <label> <command...> — run a command with a little loader
	local label="$1"; shift
	local pid rc
	"$@" & pid=$!
	while kill -0 "$pid" 2>/dev/null; do
		printf '\r%s%s…%s' "$C_DIM" "$label" "$C_RESET"
		sleep 0.3
		printf '\r%s%s %s' "$C_DIM" "$label" "$C_RESET"
		sleep 0.3
	done
	wait "$pid"; rc=$?
	printf '\r\033[K'
	if [[ $rc -ne 0 ]]; then
		printf '%s%s failed (exit %d)%s\n' "$C_RED" "$label" "$rc" "$C_RESET"
	fi
	return "$rc"
}

# ── helpers ─────────────────────────────────────────────────────────────────
app_proc() { pgrep -f "Modrinth App.app/Contents/MacOS/Modrinth App" >/dev/null 2>&1; }

pick_binary() {
	local arch
	arch="$(uname -m)"
	case "$arch" in
		arm64 | aarch64) BIN_NAME="modrinth-patcher-macos-arm64" ;;
		x86_64 | amd64)  BIN_NAME="modrinth-patcher-macos-x64" ;;
		*) die "unsupported architecture: $arch" ;;
	esac
	info "Detected $(uname -s) $arch → $BIN_NAME"
}

download_binary() { # download_binary <out-path>
	local out="$1" tok="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
	local api raw
	api="https://api.github.com/repos/$REPO/contents/dist/${BIN_NAME}?ref=$REF"
	raw="https://raw.githubusercontent.com/$REPO/$REF/dist/$BIN_NAME"

	if [[ -n "$tok" ]]; then
		info "Downloading $BIN_NAME (authenticated)…"
		curl -fsSL --progress-bar -L -H "Authorization: Bearer $tok" \
			-H "Accept: application/vnd.github.raw" "$api" -o "$out" \
			|| die "download failed — check GH_TOKEN / network"
	elif curl -fsSL --progress-bar "$raw" -o "$out" 2>/dev/null; then
		info "Downloading ${BIN_NAME}…"
	else
		info "Downloading ${BIN_NAME}…"
		curl -fsSL --progress-bar -L -H "Authorization: Bearer $tok" \
			-H "Accept: application/vnd.github.raw" "$api" -o "$out" \
			|| die "download failed — if this is a private fork, export GH_TOKEN and retry"
	fi
}

resolve_source() {
	local script_dir
	script_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-.}")" 2>/dev/null && pwd)"
	if [[ "${MP_REMOTE:-0}" != "1" && -f "$script_dir/dist/$BIN_NAME" ]]; then
		SRC="$script_dir/dist/$BIN_NAME"
		ok "Using local build: $SRC"
	else
		SRC="$(mktemp -t mp-patcher.XXXXXX)"
		download_binary "$SRC"
		chmod +x "$SRC"
		ok "Downloaded to $SRC"
	fi
}

install_binary() {
	local dest="$DEST_DIR/modrinth-patcher"
	mkdir -p "$DEST_DIR"
	if [[ -w "$DEST_DIR" ]]; then
		install -m 755 "$SRC" "$dest"
	else
		info "Need admin to write $DEST_DIR — using sudo"
		sudo install -m 755 "$SRC" "$dest"
	fi
	ok "Installed $dest"
}

quit_app() {
	if ! app_proc; then
		ok "Modrinth App not running"
		return 0
	fi
	info "Closing Modrinth App…"
	osascript -e 'tell application "Modrinth App" to quit' >/dev/null 2>&1 || true
	local i
	for i in $(seq 1 20); do app_proc || break; sleep 0.5; done
	if app_proc; then
		pkill -f "Modrinth App.app/Contents/MacOS/Modrinth App" 2>/dev/null || true
		sleep 1
	fi
	if app_proc; then warn "could not close Modrinth App — close it manually"; else ok "Modrinth App closed"; fi
}

patch_app() {
	local args=()
	[[ -n "${MP_BINARY:-}" ]] && args+=(--binary "$MP_BINARY")
	info "Patching ads out…"
	dots "patching" "$DEST_DIR/modrinth-patcher" "${args[@]}"
	ok "Ads patched"
}

relaunch_app() {
	if [[ -d "/Applications/Modrinth App.app" ]]; then
		open "/Applications/Modrinth App.app"
		ok "Modrinth App relaunched"
	else
		warn "Modrinth App not found in /Applications — launch it manually"
	fi
}

# ── main ────────────────────────────────────────────────────────────────────
banner
pick_binary
resolve_source

"$SRC" --version >/dev/null 2>&1 \
	|| die "binary failed --version (wrong build for this platform?)"

install_binary

if [[ "$DRY_RUN" == "1" ]]; then
	echo "${C_DIM}[dry-run] would: close Modrinth App, run patcher, relaunch app${C_RESET}"
	rm -f "$SRC"
	exit 0
fi

quit_app
patch_app
relaunch_app
rm -f "$SRC"

echo
ok "Done — ads removed. The watcher re-patches after app updates automatically."
