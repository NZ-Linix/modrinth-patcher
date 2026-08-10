package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

	// also watch the .orig backup so a re-patch after update creates a fresh
	// backup of the *new* version only if none exists yet.
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
//	windows: schtasks /create ... (runs at logon, every 15 min)
func installWatcher() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own path: %w", err)
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
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	// load it (ignore failure: may already be loaded / not bootstrapped)
	_ = execCommand("launchctl", "unload", plistPath)
	return execCommand("launchctl", "load", plistPath)
}

func installScheduledTask(exe string) error {
	// Runs at logon and every 15 minutes; the watcher loop exits after a
	// successful check so the task re-runs it periodically.
	cmd := fmt.Sprintf(
		`schtasks /Create /F /TN "ModrinthPatcher" /TR "\"%s\" --watch --once" /SC ONLOGON /RL LIMITED`,
		exe,
	)
	return execCommand("cmd.exe", "/C", cmd)
}

func logPath() string {
	dir := filepath.Join(os.Getenv("HOME"), "Library", "Logs", "ModrinthPatcher")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "patcher.log")
}

// execCommand runs a command and returns an error including its output.
func execCommand(name string, args ...string) error {
	return runCommand(name, args...)
}
