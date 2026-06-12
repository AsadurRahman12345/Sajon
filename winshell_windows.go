// winshell_windows.go — Automatic Windows Shell Integration for Sajon
//
// This file is compiled ONLY on Windows (build tag: GOOS=windows).
// It registers .saj files in the Windows Registry so they appear with
// the Sajon icon in Explorer — silently and without admin rights.
//
// Registry layout (HKCU — no admin required):
//
//	HKCU\Software\Classes\.saj                        → "SajonLanguageFile"
//	HKCU\Software\Classes\SajonLanguageFile           → "Sajon Infrastructure File"
//	HKCU\Software\Classes\SajonLanguageFile\DefaultIcon → "<exe>,0"
//	HKCU\Software\Classes\SajonLanguageFile\shell\open\command → "\"<exe>\" \"%1\""
//
// If HKCU write succeeds, a sentinel key is written so subsequent boots
// skip re-registration silently (no terminal output after first time).
//
// Falls back gracefully — any error is swallowed silently so that the
// main compiler pipeline is never interrupted by DX logic.

package main

import (
	"os"
	"os/exec"
	"strings"
)

// registrySentinelKey is written after successful first-time registration.
// Its presence signals that setup has already run.
const registrySentinelKey = `HKCU\Software\Classes\SajonLanguageFile\SajonSetup`

// runWindowsShellSync checks if .saj file association is registered and,
// if not, silently injects it into HKCU (no admin privileges required).
//
// Returns true if this was the *first* successful registration (so the
// caller can print the one-time "Environment Synced" message).
// Returns false if already registered, or if registration failed silently.
func runWindowsShellSync() bool {
	// ── Find our own executable path ─────────────────────────────────────────
	exePath, err := os.Executable()
	if err != nil {
		return false // cannot determine exe path — skip silently
	}
	// Resolve any symlinks
	if resolved, err2 := os.Readlink(exePath); err2 == nil {
		exePath = resolved
	}

	// ── Check sentinel: already configured? ──────────────────────────────────
	checkCmd := exec.Command("reg", "query", registrySentinelKey, "/ve")
	checkCmd.SysProcAttr = hiddenWindow()
	if checkCmd.Run() == nil {
		// Sentinel found — already configured, skip silently
		return false
	}

	// ── Check if .saj key already exists (installed by another means) ─────────
	sajCheck := exec.Command("reg", "query", `HKCU\Software\Classes\.saj`, "/ve")
	sajCheck.SysProcAttr = hiddenWindow()
	if sajCheck.Run() == nil {
		// .saj key exists — write sentinel and return false (already set)
		writeSentinel()
		return false
	}

	// ── First-time registration ───────────────────────────────────────────────
	// All reg add commands target HKCU — never requires admin rights.
	entries := [][]string{
		// .saj extension → ProgID
		{`HKCU\Software\Classes\.saj`, "/ve", "/d", "SajonLanguageFile", "/f"},
		// ProgID display name
		{`HKCU\Software\Classes\SajonLanguageFile`, "/ve", "/d", "Sajon Infrastructure File", "/f"},
		// Default icon: icon 0 inside the exe
		{`HKCU\Software\Classes\SajonLanguageFile\DefaultIcon`, "/ve", "/d", exePath + ",0", "/f"},
		// Open action
		{`HKCU\Software\Classes\SajonLanguageFile\shell`, "/ve", "/d", "open", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\shell\open`, "/ve", "/d", "Open with Sajon", "/f"},
		{`HKCU\Software\Classes\SajonLanguageFile\shell\open\command`, "/ve", "/d", `"` + exePath + `" "%1"`, "/f"},
		// Content type hint
		{`HKCU\Software\Classes\.saj`, "/v", "Content Type", "/d", "text/x-sajon", "/f"},
		// Perceived type
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

	// ── Notify Windows shell to refresh icons ─────────────────────────────────
	// ie4uinit.exe -show refreshes per-user shell icon cache.
	// We ignore errors — this is best-effort.
	refresh := exec.Command("ie4uinit.exe", "-show")
	refresh.SysProcAttr = hiddenWindow()
	_ = refresh.Run()

	// ── Write sentinel so next boot skips re-registration ─────────────────────
	writeSentinel()

	return true // first-time registration succeeded!
}

// writeSentinel writes the sentinel registry key that marks setup as complete.
func writeSentinel() {
	cmd := exec.Command("reg", "add", registrySentinelKey,
		"/v", "version", "/d", "1.1.9", "/f")
	cmd.SysProcAttr = hiddenWindow()
	_ = cmd.Run()
}

// hiddenSysProcAttr returns a SysProcAttr that hides the console window of
// child processes (so reg.exe flashes don't appear on screen).
// The actual struct is defined in winshell_syscall_windows.go to keep
// syscall imports separate from the logic file.

// regKeyExists is a helper used in tests to check if a key is present.
func regKeyExists(key string) bool {
	parts := strings.Fields(key)
	_ = parts
	cmd := exec.Command("reg", "query", key, "/ve")
	cmd.SysProcAttr = hiddenWindow()
	return cmd.Run() == nil
}
