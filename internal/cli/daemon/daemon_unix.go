//go:build darwin || linux

// Package daemon isolates background-process helpers for `discursive start --background`.
//
// Contract: no Cobra; start command calls these after forking.
package daemon

import (
	"os"
	"path/filepath"
	"syscall"
)

// SysProcAttr returns attributes for os.StartProcess that create a
// new session (setsid) without a controlling terminal.
func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// Detach re-opens stdio to detach from the terminal.
// Called by the background child process (--_bg flag).
func Detach(dataRoot string) {
	logPath := filepath.Join(dataRoot, "gateway.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		devNull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		os.Stdout = devNull
		os.Stderr = devNull
		os.Stdin = devNull
		return
	}

	devNull, _ := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	os.Stdin = devNull
	os.Stdout = logFile
	os.Stderr = logFile
}
