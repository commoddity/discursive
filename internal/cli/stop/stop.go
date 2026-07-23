package stop

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
)

// NewCmd returns the stop subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "🛑 Stop the running gateway (sends SIGTERM)",
		Long: `🛑  Reads the PID file from the data directory and sends SIGTERM
to the gateway process.  Falls back to scanning for "discursive start"
processes when no PID file exists.`,

		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			pidPath := filepath.Join(dataRoot, "gateway.pid")

			raw, err := os.ReadFile(pidPath)
			if err == nil {
				pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
				if err == nil {
					if killPID(pid) {
						_ = os.Remove(pidPath)
						return nil
					}
					// Process dead or not found — clean up stale pid file.
					_ = os.Remove(pidPath)
				}
			}

			// Fallback: no usable PID file — scan for "discursive start" processes.
			if killed := killGatewayProcesses(); killed > 0 {
				_ = os.Remove(pidPath)
				return nil
			}

			slog.Info("gateway not running")
			return nil
		},
	}
}

// killPID sends SIGTERM to the given PID. Returns true if the signal was sent.
func killPID(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		slog.Warn("stop process lookup failed", "pid", pid, "err", err)
		return false
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		slog.Warn("stop signal failed (process may already be dead)", "pid", pid, "err", err)
		return false
	}
	slog.Info("stop signal sent", "pid", pid)
	return true
}

// killGatewayProcesses scans for "discursive start" processes via pgrep,
// sends SIGTERM to each, and returns the count of killed processes.
//
// Uses pgrep -f to match against the full command line (argv), not just the
// kernel process name, so it works correctly with Go binaries on macOS/Linux.
// Since we match for the substring "discursive" and then check the first
// argument after the executable path, we won't accidentally match processes
// like "discursive stop" or "go install".
func killGatewayProcesses() int {
	// pgrep -f matches against the full command line (argv).
	// pgrep -x matches only against the kernel process name (16 chars on macOS) — skip it.
	cmd := exec.Command("pgrep", "-f", "discursive")
	output, err := cmd.Output()
	if err != nil {
		// pgrep exits 1 when no processes match — not an error.
		return 0
	}

	pids := strings.Fields(strings.TrimSpace(string(output)))
	if len(pids) == 0 {
		return 0
	}

	killed := 0
	myPID := os.Getpid()
	for _, pidStr := range pids {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if pid == myPID {
			continue
		}
		if !isGatewayStart(pid) {
			continue
		}
		if killPID(pid) {
			killed++
		}
	}

	if killed > 0 {
		slog.Info("gateway stopped via process scan", "count", killed)
	}
	return killed
}

// isGatewayStart checks whether a process's command line contains " start " as
// the first argument after the executable.  Uses `ps -o args=` (cross-platform
// macOS/Linux).
//
// This filters out "discursive stop", "discursive status", etc. so we only
// kill background gateway processes whose argv looks like:
//
//	discursive start ...
//	/path/to/discursive start ...
func isGatewayStart(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	args := strings.TrimSpace(string(output))

	parts := strings.Fields(args)
	// Need at least "discursive start" → 2 parts.
	if len(parts) < 2 {
		return false
	}
	return parts[1] == "start"
}
