//go:build darwin || linux

// Package daemon isolates background-process helpers for `discursive start --background`.
//
// Contract: no Cobra; start command calls these after forking.
package daemon

import (
	"io"
	"os"
	"syscall"
)

// SysProcAttr returns attributes for os.StartProcess that create a
// new session (setsid) without a controlling terminal.
func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// Detach redirects stdin to /dev/null and stdout/stderr to the given writer.
// Called by the background child process (--_bg flag) to detach from terminal.
// The writer is typically a rotating log file writer.
func Detach(stdout io.Writer, stderr io.Writer) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		devNull = nil
	}
	if devNull != nil {
		os.Stdin = devNull
	}

	// We must assign *os.File values for os.Stdout/os.Stderr because the Go
	// runtime and some libraries write to these directly.  The caller should
	// pass the same underlying *os.File wrapped in the io.Writer for slog.
	if f, ok := stdout.(*os.File); ok {
		os.Stdout = f
	}
	if f, ok := stderr.(*os.File); ok {
		os.Stderr = f
	}
}
