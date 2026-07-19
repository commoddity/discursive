//go:build darwin || linux

package cli

import (
	"os"
	"path/filepath"
	"syscall"
)

// daemonSysProcAttr returns attributes for os.StartProcess that create a
// new session (setsid) without a controlling terminal.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// daemonize re-opens stdio to detach from the terminal.  Called by the
// background child process (--_bg flag).
func daemonize(dataRoot string) {
	logPath := filepath.Join(dataRoot, "gateway.log")

	// Redirect stdout + stderr to log file; stdin to /dev/null.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		// Best-effort: if we can't open the log, just use /dev/null.
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
