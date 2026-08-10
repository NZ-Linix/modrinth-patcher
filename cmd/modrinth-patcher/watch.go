package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nzxt/modrinth-patcher/internal/patch"
)

// runWatcher is the long-lived auto-repatch loop. It watches the app binary
// and re-applies the patch whenever an update replaces it (the Modrinth App
// self-updates by downloading a new binary into the same location).
//
// Triggered by: macOS LaunchAgent / Windows scheduled task (see installWatcher).
func runWatcher() error {
	bin, err := findAppBinary()
	if err != nil {
		return err
	}
	fmt.Printf("watcher: monitoring %s\n", bin)

	for {
		time.Sleep(30 * time.Second)

		b, err := patch.Open(bin)
		if err != nil {
			continue // binary temporarily locked / being replaced
		}
		if patch.IsPatched(b) {
			continue
		}
		fmt.Println("watcher: app updated, re-patching...")
		if err := runPatch(bin, false); err != nil {
			fmt.Fprintln(os.Stderr, "watcher: re-patch failed:", err)
			continue
		}
	}
}

// installWatcher registers a persistent auto-repatch job:
//
//	macOS:  ~/Library/LaunchAgents/com.modrinth-patcher.plist (LaunchAgent)
//	windows: schtasks /create ... (runs at logon)
//
// The job must point at a STABLE binary path — the installed patcher
// (e.g. /usr/local/bin/modrinth-patcher), not the current executable, which
// may be a temp file (install.sh downloads to a temp dir and deletes it) or
// a dev build. We prefer the installed path and fall back to os.Executable().
func installWatcher() error {
	exe := installedPatcherPath()
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve own path: %w", err)
		}
	}
	exe, _ = filepath.Abs(exe)

	switch runtime.GOOS {
	case "darwin":
		return installLaunchAgent(exe)
	case "windows":
		return installScheduledTask(exe)
	default:
		return fmt.Errorf("no watcher support on %s", runtime.GOOS)
	}
}

// installedPatcherPath returns the canonical installed location of the
// patcher binary, or "" if it isn't installed there. It honors DEST_DIR
// (set by install.sh/install.bat) and falls back to the standard locations.
func installedPatcherPath() string {
	candidates := []string{}
	if d := os.Getenv("DEST_DIR"); d != "" {
		switch runtime.GOOS {
		case "darwin":
			candidates = append(candidates, filepath.Join(d, "modrinth-patcher"))
		case "windows":
			candidates = append(candidates, filepath.Join(d, "modrinth-patcher.exe"))
		}
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/usr/local/bin/modrinth-patcher",
			"/opt/homebrew/bin/modrinth-patcher",
			filepath.Join(os.Getenv("HOME"), ".local", "bin", "modrinth-patcher"),
			filepath.Join(os.Getenv("HOME"), "bin", "modrinth-patcher"),
		)
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates,
				filepath.Join(local, "Programs", "modrinth-patcher", "modrinth-patcher.exe"),
				filepath.Join(local, "modrinth-patcher", "modrinth-patcher.exe"),
			)
		}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// uninstallWatcher removes the auto-repatch job (used by --unpatch so the
// restored binary stays unpatched).
func uninstallWatcher() error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchAgent()
	case "windows":
		return uninstallScheduledTask()
	default:
		return nil
	}
}

func installLaunchAgent(exe string) error {
	agentDir := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	plistPath := filepath.Join(agentDir, "com.modrinth-patcher.plist")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>com.modrinth-patcher</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>--watch</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Background</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, exe, logPath(), logPath())
	if err := writeFileAtomic(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	// load it (ignore failure: may already be loaded / not bootstrapped)
	_ = runCommand("launchctl", "unload", plistPath)
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchAgent() error {
	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.modrinth-patcher.plist")
	_ = runCommand("launchctl", "unload", plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func installScheduledTask(exe string) error {
	// Prefer a scheduled task; on many systems (or with restricted
	// policies) schtasks /Create returns "Access is denied", so fall back
	// to a per-user HKCU Run entry that needs no admin rights.
	err := createScheduledTask(exe)
	if err == nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, "watcher: schtasks failed (%v) — falling back to HKCU Run entry\n", err)
	return createRunEntry(exe)
}

func uninstallScheduledTask() error {
	// remove both the task (if present) and the Run entry (if present)
	_ = runCommand("schtasks", "/Delete", "/F", "/TN", "ModrinthPatcher")
	return removeRunEntry()
}

// createScheduledTask registers a logon scheduled task that runs the watcher.
func createScheduledTask(exe string) error {
	// Runs at logon; the task starts the watcher loop which stays alive.
	// Call schtasks.exe directly (not via cmd.exe /C) so Go's Windows
	// argument quoting handles the embedded quotes in /TR correctly.
	tr := fmt.Sprintf(`"%s" --watch`, exe)
	return runCommand("schtasks",
		"/Create", "/F",
		"/TN", "ModrinthPatcher",
		"/TR", tr,
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
	)
}

// createRunEntry adds a HKCU\...\Run value that launches the watcher at
// logon via a hidden VBS wrapper (no console window, no admin required).
func createRunEntry(exe string) error {
	// VBS launcher: start the watcher hidden.
	vbs := filepath.Join(os.Getenv("TEMP"), "mp-patcher-watch.vbs")
	vbsContent := fmt.Sprintf(
		`CreateObject("Wscript.Shell").Run "%s" --watch, 0, False`+"\r\n",
		strings.ReplaceAll(exe, `"`, `""`),
	)
	if err := os.WriteFile(vbs, []byte(vbsContent), 0o644); err != nil {
		return fmt.Errorf("write vbs launcher: %w", err)
	}
	// HKCU Run — per-user, no admin needed. Call reg.exe directly so Go's
	// Windows argument quoting handles the backslashes and quotes.
	runKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	value := `wscript.exe "` + vbs + `"`
	return runCommand("reg",
		"add", runKey,
		"/v", "ModrinthPatcher",
		"/t", "REG_SZ",
		"/d", value,
		"/f",
	)
}

// removeRunEntry deletes the HKCU Run value and the VBS launcher.
func removeRunEntry() error {
	vbs := filepath.Join(os.Getenv("TEMP"), "mp-patcher-watch.vbs")
	_ = os.Remove(vbs)
	runKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	return runCommand("reg", "delete", runKey, "/v", "ModrinthPatcher", "/f")
}

func logPath() string {
	dir := filepath.Join(os.Getenv("HOME"), "Library", "Logs", "ModrinthPatcher")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "patcher.log")
}
