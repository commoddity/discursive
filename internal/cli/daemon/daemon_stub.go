//go:build !darwin && !linux

package daemon

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// SysProcAttr is unavailable on this platform.
func SysProcAttr() *syscall.SysProcAttr {
	return nil
}

// Detach warns that daemon mode is unsupported.
func Detach(dataRoot string) {
	_ = dataRoot
	fmt.Fprintf(os.Stderr, "WARNING: daemon mode not supported on %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
