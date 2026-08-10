# modrinth-patcher

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
   
   The patched JS is recompressed (brotli q9) and written back zero-padded to
   the original size, so every other embedded asset stays put.
3. **macOS** — ad-hoc re-signs the app bundle (patching invalidates the
   Developer ID signature).
4. **Auto-repatch** — installs a watcher that re-applies the patch whenever
   the app self-updates (macOS LaunchAgent, Windows scheduled task).

The discovery is **version-robust**: no hardcoded chunk hashes, so it keeps
working across app updates.

## Usage

### Patch

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
GOOS=darwin GOARCH=arm64 go build -o modrinth-patcher-mac-arm64 ./cmd/modrinth-patcher
GOOS=darwin GOARCH=amd64 go build -o modrinth-patcher-mac-x64 ./cmd/modrinth-patcher
```

The only dependency is `github.com/andybalholm/brotli` (pure Go, no CGO).

## Layout

```
cmd/modrinth-patcher/   CLI: patch / unpatch / watch, watcher install
internal/patch/         core: binary handling, asset extraction, JS markers
```

## How to verify it worked

- The ad corner is gone (no 300×250 box, no "Upgrade to Modrinth Plus" link).
- No network requests to `modrinth.com/wrapper/app-ads-cookie` (check with a
  proxy, or just trust the URL rewrite).
- `strings <binary> | grep wrapper/app-ads-cookie` returns nothing.
