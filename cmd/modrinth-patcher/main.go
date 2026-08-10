// Command modrinth-patcher removes ads from the Modrinth App launcher
// (macOS and Windows). It patches the installed app binary in place:
//
//  1. Native layer: rewrites the ad-webview URL (modrinth.com/wrapper/
//     app-ads-cookie → about:blank) — kills all ad-network traffic.
//  2. Frontend layer: decompresses the embedded JS bundle, forces
//     showAd=false and adConsentAvailable=false, neuters the ad helpers,
//     and removes the Modrinth Plus / Modrinth Hosting promo banners.
//
// It also installs an auto-repatch watcher so ads stay gone after the app
// self-updates (macOS: LaunchAgent; Windows: scheduled task).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nzxt/modrinth-patcher/internal/patch"
)

const version = "0.1.0"

func main() {
	var (
		binaryPath string
		doUnpatch  bool
		doWatch    bool
		noWatch    bool
		showVer    bool
	)
	flag.StringVar(&binaryPath, "binary", "", "path to the Modrinth App binary (auto-detected if empty)")
	flag.BoolVar(&doUnpatch, "unpatch", false, "restore the original binary from the backup")
	flag.BoolVar(&doWatch, "watch", false, "run the auto-repatch watcher (installed by --install-watch)")
	flag.BoolVar(&noWatch, "no-watch", false, "do not install the auto-repatch watcher")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.Usage = usage
	flag.Parse()

	if showVer {
		fmt.Printf("modrinth-patcher %s\n", version)
		return
	}

	if doWatch {
		if err := runWatcher(); err != nil {
			fmt.Fprintln(os.Stderr, "watcher error:", err)
			os.Exit(1)
		}
		return
	}

	// Locate the binary.
	path := binaryPath
	if path == "" {
		var err error
		path, err = findAppBinary()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			fmt.Fprintln(os.Stderr, "pass --binary <path> to point at your Modrinth App binary.")
			os.Exit(1)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if doUnpatch {
		if err := unpatch(abs); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("restored original binary from backup: %s\n", abs)
		return
	}

	if err := runPatch(abs, !noWatch); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `modrinth-patcher %s — remove ads from the Modrinth App launcher

Usage:
  modrinth-patcher [--binary <path>] [--no-watch]     patch the app (and install auto-repatch)
  modrinth-patcher --unpatch [--binary <path>]        restore the original from backup
  modrinth-patcher --watch                             run the auto-repatch watcher (internal)

Options:
  --binary <path>   path to the app binary (default: auto-detect)
  --no-watch        don't install the auto-repatch watcher
  --unpatch         restore original binary
  --watch           internal: run the watcher loop
  --version         print version
  -h                this help
`, version)
}

// runPatch patches the binary, backs up the original, re-signs on macOS, and
// optionally installs the watcher.
func runPatch(abs string, installWatch bool) error {
	b, err := patch.Open(abs)
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	if patch.IsPatched(b) {
		fmt.Println("binary already patched, skipping")
	} else {
		// backup
		backup := abs + ".orig"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.WriteFile(backup, b.Data(), 0o755); err != nil {
				return fmt.Errorf("backup: %w", err)
			}
			fmt.Printf("backed up original to %s\n", backup)
		}
		summary, err := patch.ApplyPatches(b)
		if err != nil {
			return err
		}
		if err := b.Write(abs); err != nil {
			return fmt.Errorf("write patched binary: %w", err)
		}
		fmt.Println("patched:", summary)

		if runtime.GOOS == "darwin" {
			if err := resignApp(abs); err != nil {
				fmt.Fprintln(os.Stderr, "warning: could not re-sign app bundle:", err)
				fmt.Fprintln(os.Stderr, "you may need to right-click → Open the app once, or run: codesign --force --deep -s - <app>")
			} else {
				fmt.Println("re-signed app bundle (ad-hoc)")
			}
		}
	}

	if installWatch {
		if err := installWatcher(); err != nil {
			fmt.Fprintln(os.Stderr, "warning: could not install auto-repatch watcher:", err)
			fmt.Fprintln(os.Stderr, "re-run the patcher after each app update.")
		} else {
			fmt.Println("auto-repatch watcher installed")
		}
	}
	return nil
}

// unpatch restores the original binary from the .orig backup.
func unpatch(abs string) error {
	backup := abs + ".orig"
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	if err := os.WriteFile(abs, data, 0o755); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if runtime.GOOS == "darwin" {
		if err := resignApp(abs); err != nil {
			fmt.Fprintln(os.Stderr, "warning: could not re-sign app bundle:", err)
		}
	}
	// keep the backup so --unpatch stays idempotent
	return nil
}

// findAppBinary locates the Modrinth App binary on the current platform.
func findAppBinary() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		candidates := []string{
			"/Applications/Modrinth App.app/Contents/MacOS/Modrinth App",
			filepath.Join(os.Getenv("HOME"), "Applications", "Modrinth App.app", "Contents", "MacOS", "Modrinth App"),
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
		}
		return "", fmt.Errorf("Modrinth App not found in /Applications or ~/Applications")
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		candidates := []string{
			filepath.Join(local, "Modrinth App", "Modrinth App.exe"),
			filepath.Join(local, "Programs", "Modrinth App", "Modrinth App.exe"),
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
		}
		return "", fmt.Errorf("Modrinth App.exe not found under %%LOCALAPPDATA%%\\Modrinth App")
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

// resignApp ad-hoc re-signs the app bundle after modifying its binary.
func resignApp(binPath string) error {
	// binary path: .../Modrinth App.app/Contents/MacOS/Modrinth App
	app := binPath
	for i := 0; i < 3; i++ {
		app = filepath.Dir(app)
	}
	if !strings.HasSuffix(app, ".app") {
		return fmt.Errorf("not inside an .app bundle: %s", binPath)
	}
	return codesignAdHoc(app)
}
