// shellsync_linux.go — Automatic Linux Shell Integration for Sajon
//
// Build tag: compiled ONLY on Linux (GOOS=linux).
//
// On Linux, file-type associations and icons are managed through the
// XDG Freedesktop specification:
//
//  1. MIME type definition  → ~/.local/share/mime/packages/sajon.xml
//  2. .desktop file         → ~/.local/share/applications/sajon.desktop
//  3. Application icon      → ~/.local/share/icons/hicolor/256x256/apps/sajon.png
//
// After writing these files, `update-mime-database` and
// `update-desktop-database` are called silently to activate them.
//
// A sentinel file (~/.local/share/sajon/.shellsync_done) is created after
// the first successful registration so that subsequent boots are no-ops.
//
// All errors are swallowed silently — the compiler pipeline is NEVER
// interrupted by DX/shell logic.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public entry point called from main()
// ─────────────────────────────────────────────────────────────────────────────

// runShellSync is the OS-agnostic entry point that main() calls.
// On Linux it delegates to runLinuxShellSync().
func runShellSync() bool {
	return runLinuxShellSync()
}

// ─────────────────────────────────────────────────────────────────────────────
// Linux implementation
// ─────────────────────────────────────────────────────────────────────────────

// runLinuxShellSync registers .saj files with the XDG MIME and icon
// subsystem silently.  Returns true on first-time successful setup.
func runLinuxShellSync() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// ── Sentinel check ────────────────────────────────────────────────────────
	sentinelPath := filepath.Join(home, ".local", "share", "sajon", ".shellsync_done")
	if _, statErr := os.Stat(sentinelPath); statErr == nil {
		return false // already configured
	}

	// ── Locate own executable ─────────────────────────────────────────────────
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err2 := os.Readlink(exePath); err2 == nil {
		exePath = resolved
	}

	// ── 1. MIME package XML ───────────────────────────────────────────────────
	mimeDir := filepath.Join(home, ".local", "share", "mime", "packages")
	if err := os.MkdirAll(mimeDir, 0755); err != nil {
		return false
	}
	mimeXML := `<?xml version="1.0" encoding="UTF-8"?>
<mime-info xmlns="http://www.freedesktop.org/standards/shared-mime-info">
  <mime-type type="text/x-sajon">
    <comment>Sajon Infrastructure File</comment>
    <icon name="sajon"/>
    <glob pattern="*.saj"/>
    <sub-class-of type="text/plain"/>
  </mime-type>
</mime-info>
`
	mimeFile := filepath.Join(mimeDir, "sajon.xml")
	if err := os.WriteFile(mimeFile, []byte(mimeXML), 0644); err != nil {
		return false
	}

	// ── 2. .desktop file ──────────────────────────────────────────────────────
	appDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return false
	}
	desktopContent := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Sajon\n" +
		"Comment=Sajon Cloud Infrastructure Compiler\n" +
		"Exec=" + exePath + " \"%f\"\n" +
		"Icon=sajon\n" +
		"Terminal=true\n" +
		"MimeType=text/x-sajon;\n" +
		"Categories=Development;IDE;\n" +
		"Keywords=cloud;infrastructure;sajon;saj;\n"
	desktopFile := filepath.Join(appDir, "sajon.desktop")
	if err := os.WriteFile(desktopFile, []byte(desktopContent), 0644); err != nil {
		return false
	}

	// ── 3. Icon (copy current executable's directory icon if present, ─────────
	//         otherwise write a minimal embedded placeholder PNG)
	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		return false
	}
	iconDest := filepath.Join(iconDir, "sajon.png")

	// Try to copy sajon_icon.png from next to the binary first
	iconSrc := filepath.Join(filepath.Dir(exePath), "sajon_icon.png")
	if _, statErr := os.Stat(iconSrc); statErr == nil {
		if srcData, readErr := os.ReadFile(iconSrc); readErr == nil {
			_ = os.WriteFile(iconDest, srcData, 0644)
		}
	}
	// If icon doesn't exist yet, the .desktop + mime registration still works
	// (file manager falls back to a generic text icon).

	// ── 4. Update databases (best-effort, swallow all errors) ────────────────
	for _, cmd := range []*exec.Cmd{
		exec.Command("update-mime-database", filepath.Join(home, ".local", "share", "mime")),
		exec.Command("update-desktop-database", appDir),
		exec.Command("xdg-mime", "default", "sajon.desktop", "text/x-sajon"),
	} {
		_ = cmd.Run()
	}

	// ── 5. Write sentinel ─────────────────────────────────────────────────────
	sentinelDir := filepath.Dir(sentinelPath)
	if err := os.MkdirAll(sentinelDir, 0755); err != nil {
		return false
	}
	_ = os.WriteFile(sentinelPath, []byte("1.2.0\n"), 0644)

	return true
}

// regKeyExists stub — not applicable on Linux.
func regKeyExists(_ string) bool { return false }
