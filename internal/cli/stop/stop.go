package stop

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
)

const stopWaitTimeout = 12 * time.Second

// NewCmd returns the stop subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running gateway",
		Long: `Writes a stop file that the background gateway polls, then waits for
the process to exit.  Falls back to scanning for "discursive start"
processes when no PID file exists.  Sends SIGKILL only if graceful stop
times out.`,

		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			pidPath := filepath.Join(dataRoot, "gateway.pid")
			stopFile := filepath.Join(dataRoot, "gateway.stop")

			if err := os.WriteFile(stopFile, []byte("stop"), 0o600); err != nil {
				return err
			}
			slog.Info("stop request written", "path", stopFile)

			pids := collectGatewayPIDs(pidPath)
			if len(pids) == 0 {
				slog.Info("gateway not running")
				cleanupStopArtifacts(pidPath, stopFile)
				return nil
			}

			// SIGTERM is ignored by background gateways; stop-file is the real signal.
			// Still send for foreground / legacy processes.
			for _, pid := range pids {
				signalPID(pid, syscall.SIGTERM)
			}

			stopped := 0
			for _, pid := range pids {
				if waitProcessExit(pid, stopWaitTimeout) {
					stopped++
					continue
				}
				slog.Warn("gateway did not exit gracefully, sending SIGKILL", "pid", pid)
				if signalPID(pid, syscall.SIGKILL) {
					if waitProcessExit(pid, 3*time.Second) {
						stopped++
					}
				}
			}

			cleanupStopArtifacts(pidPath, stopFile)
			if stopped > 0 {
				slog.Info("gateway stopped", "count", stopped)
			}
			return nil
		},
	}
}

func cleanupStopArtifacts(pidPath, stopFile string) {
	_ = os.Remove(pidPath)
	_ = os.Remove(stopFile)
}

func collectGatewayPIDs(pidPath string) []int {
	seen := make(map[int]bool)
	var pids []int

	if raw, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
			if processAlive(pid) {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}

	for _, pid := range scanGatewayPIDs() {
		if !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}
	return pids
}

func scanGatewayPIDs() []int {
	// When DISCURSIVE_TEST_STOP_SKIP_FALLBACK is set, skip the pgrep fallback
	// that scans for all discursive processes on the system.  This protects
	// the real running gateway during tests.
	if os.Getenv("DISCURSIVE_TEST_STOP_SKIP_FALLBACK") == "1" {
		return nil
	}

	cmd := exec.Command("pgrep", "-f", "discursive")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var pids []int
	myPID := os.Getpid()
	for _, pidStr := range strings.Fields(strings.TrimSpace(string(output))) {
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == myPID {
			continue
		}
		if !isGatewayStart(pid) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func signalPID(pid int, sig syscall.Signal) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(sig); err != nil {
		return false
	}
	if sig == syscall.SIGTERM {
		slog.Info("stop signal sent", "pid", pid)
	}
	return true
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func waitProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return !processAlive(pid)
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
