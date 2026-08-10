# modrinth-patcher

---

Heads up: This is vibe-coded

---

Removes ads from the **Modrinth App** launcher (the open-source desktop app for
managing Minecraft modpacks) on **macOS** and **Windows**.

The free tier of the Modrinth App displays:

1. **Third-party ads** — a 300×250 banner from Modrinth's ad network
   (Aditude), rendered in a hidden child webview over the app, refreshed every
   5 minutes.
2. **Self-promo banners** — an "Upgrade to Modrinth Plus" link and a
   "Modrinth Hosting" fallback image in the same corner.

This patcher removes both, and can keep them removed across app self-updates.

> **Legal note.** The Modrinth App is GPL-3.0 open source, but the ads are how
> the project funds itself. Removing them may violate the app's Terms of
> Service. This tool is provided for personal/educational use; use it at your
> own risk, and consider supporting Modrinth (Modrinth Plus) if you rely on
> it.

---

## How it works

The app ships as a single native binary with the entire frontend
**brotli-compressed and embedded inside it** (Tauri v2). The patcher:

1. **Native layer** — rewrites the ad webview's URL string
   (`https://modrinth.com/wrapper/app-ads-cookie` → `about:blank#…`) in place.
   This kills all ad-network traffic, ad cookies, and the consent flow.
   Same-length replacement, so no pointers or section tables change.
2. **Frontend layer** — finds the embedded main JS bundle by reading
   `/index.html` (which references `/assets/index-<hash>.js`), decompresses it,
   then:
   - forces `showAd = false` and `adConsentAvailable = false`
   - replaces the ad-showing watcher with a no-op
   - neuters the `init/show/hide_ads_window` helpers (defense in depth)
   - blanks the "Modrinth Plus" and "Modrinth Hosting" promo URLs
   - removes the `.app-sidebar::after` gradient fade-strip that blended the
     sidebar into the ad slot (patched in the embedded stylesheet)
3. **macOS** — ad-hoc re-signs the app bundle (patching invalidates the
   Developer ID signature).
4. **Auto-repatch** — installs a watcher that re-applies the patch whenever
   the app self-updates (macOS LaunchAgent, Windows scheduled task).

The discovery is **version-robust**: no hardcoded chunk hashes, so it keeps
working across app updates.

## Install

The installer scripts are the recommended way: they download the newest
binary, install it to your PATH, **quit the Modrinth App, patch it, and
relaunch it** — with a small terminal UI. The uninstallers reverse everything.

### One-liners

```sh
# macOS — install (patch ads out)
curl -fsSL "https://raw.githubusercontent.com/NZ-Linix/modrinth-patcher/main/install.sh" | bash

# macOS — uninstall (restore original)
curl -fsSL "https://raw.githubusercontent.com/NZ-Linix/modrinth-patcher/main/uninstall.sh" | bash
```

```powershell
# Windows — install (run in PowerShell; downloads the .bat to a temp file
# and runs it via cmd, since PowerShell can't execute batch syntax directly)
irm "https://raw.githubusercontent.com/NZ-Linix/modrinth-patcher/main/install.bat" -OutFile "$env:TEMP\mp-install.bat"
cmd /c "$env:TEMP\mp-install.bat"

# Windows — uninstall
irm "https://raw.githubusercontent.com/NZ-Linix/modrinth-patcher/main/uninstall.bat" -OutFile "$env:TEMP\mp-uninstall.bat"
cmd /c "$env:TEMP\mp-uninstall.bat"
```

### Local runs

```sh
# macOS
./install.sh                 # installs to /usr/local/bin, patches, relaunches
./uninstall.sh               # restores original, removes patcher + watcher

# Windows (cmd or double-click)
install.bat                  # installs to %LOCALAPPDATA%\Programs\modrinth-patcher
uninstall.bat
```

### Options

| Env var       | Meaning                                        | Default |
|---------------|------------------------------------------------|---------|
| `DEST_DIR`    | install destination                            | `/usr/local/bin` (mac) / `%LOCALAPPDATA%\Programs\modrinth-patcher` (win) |
| `MP_REPO`     | GitHub repo for downloads                      | `NZ-Linix/modrinth-patcher` |
| `MP_REF`      | branch/tag/commit                              | `main` |
| `GH_TOKEN`    | token for downloads (private forks only)       | — |
| `MP_BINARY`   | patch a specific app binary instead of auto    | auto-detect |
| `DRY_RUN`     | print actions without doing them               | `0` |

- `install.sh` picks `modrinth-patcher-macos-arm64` on Apple Silicon and
  `modrinth-patcher-macos-x64` on Intel; uses the local `dist/` build when
  present, otherwise downloads from GitHub.
- `install.bat` installs `modrinth-patcher-windows-amd64.exe`, adds the
  folder to your **user** PATH (no admin needed), and is idempotent
  (re-running won't duplicate the PATH entry). Open a new terminal after
  installing for PATH changes to take effect.
- Both quit the running Modrinth App before patching and relaunch it after.
- Both always overwrite with the newest build, so re-run after rebuilding.

## Manual usage

```sh
# macOS — auto-detects /Applications/Modrinth App.app
./modrinth-patcher

# Windows — auto-detects %LOCALAPPDATA%\Modrinth App\Modrinth App.exe
modrinth-patcher.exe

# explicit path / skip watcher
./modrinth-patcher --binary "/path/to/Modrinth App" --no-watch
```

First run backs up the original binary next to it (`Modrinth App.orig`).

### After an app update

With the watcher installed, nothing to do — it re-patches automatically within
~30 seconds of the update.

Without the watcher, just run the patcher again.

> **Troubleshooting**: if auto-repatch stopped working after an update (e.g.
> the app updated but ads came back), the watcher job may point at an old
> binary path. Re-run the installer once — it re-registers the watcher at the
> installed location:
> `curl -fsSL https://raw.githubusercontent.com/NZ-Linix/modrinth-patcher/main/install.sh | bash`

### Unpatch / restore

```sh
./modrinth-patcher --unpatch
```

restores the original binary from the backup and disables the auto-repatch
watcher (LaunchAgent / scheduled task) so the restore sticks.

### A note on backups and updates

`--unpatch` restores the `.orig` backup of the *current* version. If the app
self-updated since you last ran the patcher, re-run the patcher once (it
refreshes the backup from the new version) before `--unpatch`.

## macOS notes

- Patching invalidates the Developer ID signature; the patcher re-signs the
  bundle **ad-hoc** (`codesign --force --deep -s -`), preserving entitlements.
  Gatekeeper may still complain on first launch (app downloaded from the
  internet) — right-click → **Open** once.
- If you re-sign with your own certificate instead:
  `codesign --force --deep --sign "Your Cert" "/Applications/Modrinth App.app"`

## Windows notes

- The EXE is Authenticode-signed by Modrinth; patching invalidates that
  signature, so SmartScreen may warn. The patched binary runs fine — you may
  need to click "More info → Run anyway".
- Windows Defender may flag the modified binary. Add an exclusion if needed.
- The installer (NSIS) installs to `%LOCALAPPDATA%\Modrinth App\`. Re-running
  the installer or an app update replaces the EXE — the watcher re-patches it.

## Building

```sh
go build -o modrinth-patcher ./cmd/modrinth-patcher          # current OS
GOOS=windows GOARCH=amd64 go build -o modrinth-patcher.exe ./cmd/modrinth-patcher
GOOS=darwin GOARCH=arm64 go build -o modrinth-patcher-macos-arm64 ./cmd/modrinth-patcher
GOOS=darwin GOARCH=amd64 go build -o modrinth-patcher-macos-x64 ./cmd/modrinth-patcher
```

The only dependency is `github.com/andybalholm/brotli` (pure Go, no CGO).

## Layout

```
cmd/modrinth-patcher/   CLI: patch / unpatch / watch, watcher install
internal/patch/         core: binary handling, asset extraction, JS/CSS markers
install.sh|bat          installer (TUI, download, patch, relaunch)
uninstall.sh|bat        uninstaller (restore original, remove watcher)
dist/                   prebuilt binaries for macOS arm64/x64 and Windows amd64
```

## How to verify it worked

- The ad corner is gone (no 300×250 box, no "Upgrade to Modrinth Plus" link).
- No network requests to `modrinth.com/wrapper/app-ads-cookie` (check with a
  proxy, or just trust the URL rewrite).
- `strings <binary> | grep wrapper/app-ads-cookie` returns nothing.
