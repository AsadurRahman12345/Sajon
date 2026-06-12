// winshell_stub.go — No-op stubs for non-Windows platforms.
//
// On Linux and macOS there is no Windows Registry, so all shell-sync
// functions are compiled as no-ops.  The build tag ensures this file
// is included on every platform EXCEPT Windows.

//go:build !windows

package main

// runWindowsShellSync is a no-op on non-Windows platforms.
// Returns false (no registration performed).
func runWindowsShellSync() bool { return false }

// regKeyExists is a no-op on non-Windows platforms.
func regKeyExists(key string) bool { return false }

// hiddenWindow returns nil on non-Windows platforms.
// It is not called on these platforms but must exist for compilation.
func hiddenWindow() interface{} { return nil }
