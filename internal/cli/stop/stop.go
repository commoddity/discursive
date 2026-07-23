package stop

import (
	"fmt"
	"log/slog"
	"os"
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
		Long:  "🛑  Reads the PID file from the data directory and sends SIGTERM\nto the gateway process.  No-op if no gateway is running.",
		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			pidPath := filepath.Join(dataRoot, "gateway.pid")
			raw, err := os.ReadFile(pidPath)
			if err != nil {
				if os.IsNotExist(err) {
					slog.Info("gateway not running (no pid file)")
					return nil
				}
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				return fmt.Errorf("invalid pid file: %w", err)
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				slog.Warn("stop process lookup failed", "pid", pid, "err", err)
				_ = os.Remove(pidPath)
				return nil
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				slog.Warn("stop signal failed (process may already be dead)", "pid", pid, "err", err)
				_ = os.Remove(pidPath)
				return nil
			}
			slog.Info("stop signal sent", "pid", pid)
			return nil
		},
	}
}
