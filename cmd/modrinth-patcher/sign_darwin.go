//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// codesignAdHoc ad-hoc re-signs an app bundle on macOS. Modifying the binary
// invalidates the Developer ID signature; ad-hoc signing keeps the app
// launchable locally (Gatekeeper will still block remote-downloaded apps
// unless the user right-click → Open once, or the quarantine attribute is
// cleared).
func codesignAdHoc(app string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		return fmt.Errorf("codesign not found: %w", err)
	}
	// Extract entitlements from the existing signature so they're preserved.
	ent := app + ".entitlements.plist"
	cmd := exec.Command("codesign", "-d", "--entitlements", ent, "--xml", app)
	if out, err := cmd.CombinedOutput(); err != nil {
		// unsigned or no entitlements — proceed without them
		_ = out
		_ = os.Remove(ent)
	}
	args := []string{"--force", "--deep", "--sign", "-"}
	if _, err := os.Stat(ent); err == nil {
		args = append(args, "--entitlements", ent)
	}
	args = append(args, app)
	out, err := exec.Command("codesign", args...).CombinedOutput()
	_ = os.Remove(ent)
	if err != nil {
		return fmt.Errorf("codesign failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
