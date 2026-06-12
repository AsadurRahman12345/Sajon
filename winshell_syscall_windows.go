// winshell_syscall_windows.go — SysProcAttr helper for Windows child processes
//
// Separated from winshell_windows.go so the syscall import is isolated.
// hiddenWindow() returns a *syscall.SysProcAttr that hides the console
// window of spawned processes (reg.exe, ie4uinit.exe) so users never see
// a flashing black terminal box.

package main

import "syscall"

// hiddenWindow returns a SysProcAttr that creates child processes without
// a visible console window (CREATE_NO_WINDOW = 0x08000000).
func hiddenWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
