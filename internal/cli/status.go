package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
)

func newStatusCmd() *cobra.Command {
	var showKey bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "📊 Print gateway config, model aliases, provider mapping, and runtime state",
		Long: `📊  Show configuration + runtime status.

Includes: gateway key (masked by default), tunnel mode, public URL, active
model alias, provider routing, all available models, whether the gateway PID
is alive, uptime (if running), and log file path.

  discursive status              # gateway key masked
  discursive status --show-key   # print full gateway_key for Cursor setup

Use with | jq . for readable output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogger()
			dataRoot, err := resolveDataRoot()
			if err != nil {
				return err
			}
			settings, err := config.Load(dataRoot)
			if err != nil {
				return err
			}

			route, _ := gateway.ResolveModel(settings.AliasModel)
			provider := ""
			if route.Provider != "" {
				provider = string(route.Provider)
			}

			// Runtime state.
			running, pid, uptime := gatewayRuntime(dataRoot)

			logPath := filepath.Join(dataRoot, "gateway.log")
			logSize := ""
			if fi, err := os.Stat(logPath); err == nil {
				logSize = fmt.Sprintf("%d bytes", fi.Size())
			}

			attrs := []any{
				"version", Version,
				"alias_model", settings.AliasModel,
				"real_model", settings.RealModel,
				"provider", provider,
				"has_moonshot_key", settings.HasMoonshotKey(),
				"has_deepseek_key", settings.HasDeepSeekKey(),
				"tunnel_mode", config.NormalizeTunnelMode(settings.TunnelMode),
				"public_url", settings.PublicBaseURL,
				"local_port", settings.LocalPort,
				"data_root", dataRoot,
			}
			attrs = append(attrs, gatewayKeyLogAttrs(settings.GatewayKey, showKey)...)
			slog.Info("status", attrs...)

			slog.Info("status_models", "models", gateway.ListAdvertisedModels())

			slog.Info("status_runtime",
				"running", running,
				"pid", pid,
				"uptime_seconds", uptime,
				"log_file", logPath,
				"log_size", logSize,
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&showKey, "show-key", false, "print the full gateway API key (default: masked)")
	return cmd
}

// gatewayRuntime reads the PID file and checks whether the process is alive.
// Returns (running, pid, uptime_seconds).
func gatewayRuntime(dataRoot string) (bool, int, int64) {
	pidPath := filepath.Join(dataRoot, "gateway.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return false, 0, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return false, 0, 0
	}
	if !processAlive(pid) {
		return false, pid, 0
	}
	// Calculate uptime from PID file modification time.
	fi, err := os.Stat(pidPath)
	if err != nil {
		return true, pid, 0
	}
	// The PID file gets deleted when the process exits, so mtime ~= start time.
	uptime := int64(time.Since(fi.ModTime()).Seconds())
	return true, pid, uptime
}

// processAlive checks whether a process with the given PID exists.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal with nil on Unix is equivalent to kill -0 (existence check).
	err = proc.Signal(os.Signal(nil))
	return err == nil
}
