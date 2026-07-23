package start

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/commoddity/discursive/internal/cli/daemon"
)

// backgroundChildArgs strips --background and persisted flag values, then appends --_bg.
func backgroundChildArgs(osArgs []string) []string {
	args := make([]string, 0, len(osArgs))
	skipNext := false
	for _, a := range osArgs[1:] {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--background" || a == "-background" {
			continue
		}
		if a == "--tunnel" || a == "-tunnel" ||
			a == "--public-url" || a == "-public-url" {
			skipNext = true
			continue
		}
		args = append(args, a)
	}
	return append(args, "--_bg")
}

func forkBackground(dataRoot string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't resolve executable: %w", err)
	}

	args := backgroundChildArgs(os.Args)

	logPath := filepath.Join(dataRoot, "gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	_ = logFile.Close()

	procAttr := &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
		Env:   os.Environ(),
		Dir:   "",
		Sys:   daemon.SysProcAttr(),
	}

	proc, err := os.StartProcess(exe, append([]string{exe}, args...), procAttr)
	if err != nil {
		return fmt.Errorf("start background process: %w", err)
	}

	pidPath := filepath.Join(dataRoot, "gateway.pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(proc.Pid)), 0o600)

	_, _ = fmt.Fprintf(os.Stderr, "🚀  Gateway started in background (PID: %d)\n", proc.Pid)
	_, _ = fmt.Fprintf(os.Stderr, "📄  Logs:  %s\n", logPath)
	_, _ = fmt.Fprintf(os.Stderr, "💡  Watch logs:  discursive logs --follow\n")
	_, _ = fmt.Fprintf(os.Stderr, "💡  Stop:        discursive stop\n")

	_ = proc.Release()
	return nil
}
