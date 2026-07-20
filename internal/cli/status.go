package cli

import (
	"fmt"
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
		Long: `📊  Show configuration + runtime status — single pretty-printed JSON object.

Includes: gateway key (masked by default), tunnel mode, public URL, active
model alias, provider routing, all available models, whether the gateway PID
is alive, uptime (if running), and log file path.

  discursive status              # gateway key masked
  discursive status --show-key   # print full gateway_key for Cursor setup`,
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

			running, pid, uptime := gatewayRuntime(dataRoot)

			logPath := filepath.Join(dataRoot, "gateway.log")
			logSize := ""
			if fi, err := os.Stat(logPath); err == nil {
				logSize = fmt.Sprintf("%d bytes", fi.Size())
			}

			keyField := "gateway_key_masked"
			keyValue := maskGatewayKey(settings.GatewayKey)
			if showKey {
				keyField = "gateway_key"
				keyValue = settings.GatewayKey
			}

			out := map[string]any{
				"version":          Version,
				"alias_model":      settings.AliasModel,
				"real_model":       settings.RealModel,
				"provider":         provider,
				"has_moonshot_key": settings.HasMoonshotKey(),
				"has_deepseek_key": settings.HasDeepSeekKey(),
				"tunnel_mode":      config.NormalizeTunnelMode(settings.TunnelMode),
				"public_url":       settings.PublicBaseURL,
				"local_port":       settings.LocalPort,
				"data_root":        dataRoot,
				keyField:           keyValue,
				"models":           gateway.ListAdvertisedModels(),
				"running":          running,
				"pid":              pid,
				"uptime_seconds":   uptime,
				"log_file":         logPath,
				"log_size":         logSize,
			}

			return emitPretty(out)
		},
	}
	cmd.Flags().BoolVar(&showKey, "show-key", false, "print the full gateway API key (default: masked)")
	return cmd
}

// maskGatewayKey masks a gateway key. Uses crypto.MaskSecret for consistency.
func maskGatewayKey(key string) string {
	if len(key) <= 6 {
		return "••••••"
	}
	return key[:3] + "••••••" + key[len(key)-4:]
}

// gatewayRuntime reads the PID file and checks whether the process is alive.
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
	fi, err := os.Stat(pidPath)
	if err != nil {
		return true, pid, 0
	}
	uptime := int64(time.Since(fi.ModTime()).Seconds())
	return true, pid, uptime
}

// processAlive checks whether a process with the given PID exists.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(os.Signal(nil))
	return err == nil
}
