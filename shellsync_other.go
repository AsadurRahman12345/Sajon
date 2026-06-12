// shellsync_other.go — Shell Integration no-op for unsupported platforms.
//
// Build tag: compiled on any OS that is NOT windows, linux, or darwin.
// (e.g. FreeBSD, OpenBSD, Plan9, WASM, etc.)
//
// This file ensures the compiler builds cleanly on every platform without
// panics, crashes, or missing symbol errors.

//go:build !windows && !linux && !darwin

package main

// runShellSync is a no-op on unsupported/unknown platforms.
func runShellSync() bool { return false }

// regKeyExists stub — not applicable on this platform.
func regKeyExists(_ string) bool { return false }
