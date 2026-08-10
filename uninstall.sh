#!/usr/bin/env bash
#
# modrinth-patcher uninstaller (macOS)
#
# Reverses install.sh: closes Modrinth App, restores the original binary
# (--unpatch), removes the installed patcher + LaunchAgent watcher + logs.
#
# Run locally:   ./uninstall.sh
# Run remotely:  curl -fsSL <url> | bash        (see README for the one-liner)
#
# Environment overrides:
#   DEST_DIR=~/bin        where the patcher was installed  (default /usr/local/bin)
#   MP_BINARY=/path       the app binary to unpatch         (default auto-detect)
#   DRY_RUN=1             print actions without running them
#
set -euo pipefail

DEST_DIR="${DEST_DIR:-/usr/local/bin}"
DRY_RUN="${DRY_RUN:-0}"
PATCHER="$DEST_DIR/modrinth-patcher"
APP_BIN="/Applications/Modrinth App.app/Contents/MacOS/Modrinth App"
LA_AGENT="$HOME/Library/LaunchAgents/com.modrinth-patcher.plist"
LOG_DIR="$HOME/Library/Logs/ModrinthPatcher"

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
	printf '│  %-47s │\n' "modrinth-patcher — uninstaller"
	printf '│  %-47s │\n' "restores the original Modrinth App"
	printf '%s\n' "└─────────────────────────────────────────────────┘${C_RESET}"
}

app_proc() { pgrep -f "Modrinth App.app/Contents/MacOS/Modrinth App" >/dev/null 2>&1; }

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
	app_proc && warn "could not close Modrinth App — close it manually" || ok "Modrinth App closed"
}

unpatch_app() {
	[[ -x "$PATCHER" ]] || { warn "patcher not installed at $PATCHER — skipping unpatch"; return 0; }
	info "Restoring original binary…"
	if [[ -n "${MP_BINARY:-}" ]]; then
		"$PATCHER" --unpatch --binary "$MP_BINARY" || warn "unpatch reported a problem"
	else
		"$PATCHER" --unpatch || warn "unpatch reported a problem"
	fi
	ok "Original binary restored"
}

remove_files() {
	if [[ -f "$LA_AGENT" ]]; then
		launchctl unload "$LA_AGENT" >/dev/null 2>&1 || true
		rm -f "$LA_AGENT" 2>/dev/null && ok "Removed LaunchAgent $LA_AGENT" \
			|| warn "could not remove LaunchAgent $LA_AGENT (permissions?)"
	fi
	[[ -d "$LOG_DIR" ]] && { rm -rf "$LOG_DIR" 2>/dev/null; ok "Removed logs $LOG_DIR"; }
	if [[ -f "$PATCHER" ]]; then
		rm -f "$PATCHER" 2>/dev/null && ok "Removed $PATCHER" \
			|| warn "could not remove $PATCHER (permissions?)"
	fi
	# .orig backup stays so the user can still restore manually
}

# ── main ────────────────────────────────────────────────────────────────────
banner

quit_app
unpatch_app
if [[ "$DRY_RUN" == "1" ]]; then
	echo "${C_DIM}[dry-run] would: remove LaunchAgent, logs, $PATCHER${C_RESET}"
	exit 0
fi
remove_files

echo
ok "Uninstalled. (The .orig backup next to the app binary is kept — delete it if unwanted.)"
