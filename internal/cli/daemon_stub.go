//go:build !darwin && !linux

package cli

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

func daemonSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func daemonize(dataRoot string) {
	fmt.Fprintf(os.Stderr, "WARNING: daemon mode not supported on %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
