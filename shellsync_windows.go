// shellsync_windows.go — Automatic Windows Shell Integration for Sajon
//
// Build tag: compiled ONLY on Windows (GOOS=windows).
//
// Registers .saj files in the Windows Registry so they appear with the
// Sajon custom icon in Windows Explorer — silently, without admin rights.
//
// Registry layout (HKCU — no admin required):
//
//	HKCU\Software\Classes\.saj                          → "SajonLanguageFile"
//	HKCU\Software\Classes\SajonLanguageFile             → "Sajon Infrastructure File"
//	HKCU\Software\Classes\SajonLanguageFile\DefaultIcon → "<exe>,0"
//	HKCU\Software\Classes\SajonLanguageFile\shell\open\command → "\"<exe>\" \"%1\""
//
// A sentinel registry key is written on first-time success so that
// subsequent boots skip re-registration silently.
//
// All errors are swallowed silently — the compiler pipeline is NEVER
// interrupted by DX/shell logic.

package main

import (
	"os"
	"os/exec"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Sentinel
// ─────────────────────────────────────────────────────────────────────────────

// registrySentinelKey is written after a successful first-time registration.
// Its presence on the next boot signals that setup has already run.
const registrySentinelKey = `HKCU\Software\Classes\SajonLanguageFile\SajonSetup`

// ─────────────────────────────────────────────────────────────────────────────
// Public entry point called from main()
// ─────────────────────────────────────────────────────────────────────────────

// runShellSync is the OS-agnostic entry point that main() calls.
// On Windows it delegates to runWindowsShellSync().
// Returns true ONLY on the very first successful registration so that the
// caller can display the one-time "[🎨] Environment Synced" message.
func runShellSync() bool {
	return runWindowsShellSync()
}

// ─────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ─────────────────────────────────────────────────────────────────────────────

// runWindowsShellSync checks if the .saj file association is already in the
// Windows Registry and, if not, silently injects it via HKCU.
func runWindowsShellSync() bool {
	// ── Locate own executable path ────────────────────────────────────────────
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	// Resolve symlinks
	if resolved, err2 := os.Readlink(exePath); err2 == nil {
		exePath = resolved
	}

	// ── Sentinel check — already configured? ─────────────────────────────────
	checkCmd := exec.Command("reg", "query", registrySentinelKey, "/ve")
	checkCmd.SysProcAttr = hiddenWindow()
	if checkCmd.Run() == nil {
		return false // sentinel found — skip silently
	}

	// ── Pre-existing .saj key check ───────────────────────────────────────────
	sajCheck := exec.Command("reg", "query", `HKCU\Software\Classes\.saj`, "/ve")
	sajCheck.SysProcAttr = hiddenWindow()
	if sajCheck.Run() == nil {
		writeSentinel() // already set by another tool — mark done
		return false
	}

	// ── First-time registration (HKCU — no admin needed) ─────────────────────
	entries := [][]string{
		{`HKCU\Software\Classes\.saj`, "/ve", "/d", "SajonLanguageFile", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile`, "/ve", "/d", "Sajon Infrastructure File", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\DefaultIcon`, "/ve", "/d", exePath + ",0", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\shell`, "/ve", "/d", "open", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\shell\open`, "/ve", "/d", "Open with Sajon", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\shell\open\command`, "/ve", "/d", `"` + exePath + `" "%1"`, "/f"},
		{`HKCU\Software\Classes\.saj`, "/v", "Content Type", "/d", "text/x-sajon", "/f"},
		{`HKCU\Software\Classes\.saj`, "/v", "PerceivedType", "/d", "text", "/f"},
	}

	allOK := true
	for _, args := range entries {
		cmdArgs := append([]string{"add"}, args...)
		cmd := exec.Command("reg", cmdArgs...)
		cmd.SysProcAttr = hiddenWindow()
		if err := cmd.Run(); err != nil {
			allOK = false
		}
	}
	if !allOK {
		return false // partial failure — don't claim success
	}

	// ── Refresh Windows shell icon cache (best-effort) ────────────────────────
	refresh := exec.Command("ie4uinit.exe", "-show")
	refresh.SysProcAttr = hiddenWindow()
	_ = refresh.Run()

	writeSentinel()
	return true
}

// writeSentinel writes the sentinel registry key.
func writeSentinel() {
	cmd := exec.Command("reg", "add", registrySentinelKey,
		"/v", "version", "/d", "1.2.0", "/f")
	cmd.SysProcAttr = hiddenWindow()
	_ = cmd.Run()
}

// regKeyExists checks whether a registry key exists (used in tests).
func regKeyExists(key string) bool {
	parts := strings.Fields(key)
	_ = parts
	cmd := exec.Command("reg", "query", key, "/ve")
	cmd.SysProcAttr = hiddenWindow()
	return cmd.Run() == nil
}
