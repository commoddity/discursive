package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"discursive/internal/config"
	"discursive/internal/crypto"
	"discursive/internal/usage"
)

var (
	Version      = "0.0.0-dev"
	portableFlag bool
)

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "discursive",
		Short:         "🌉 Local OpenAI-compatible gateway → Moonshot Kimi & DeepSeek for Cursor",
		Long:          "🌉  Discursive — a local sanitizing proxy that routes Cursor requests to Moonshot Kimi or DeepSeek via a public HTTPS tunnel.\n\n  Pick a provider by changing the model alias in Cursor.  Secrets stay on your machine.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd)
		},
	}

	cmd.PersistentFlags().BoolVar(&portableFlag, "portable", false, "store data next to the executable")
	// Log level: DISCURSIVE_LOG_LEVEL=debug|info|warn|error (default info).
	// slog is always JSON on stdout (jq-friendly).

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "🏷️  Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
			return err
		},
	})
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newSetMoonshotKeyCmd())
	cmd.AddCommand(newSetDeepSeekKeyCmd())
	cmd.AddCommand(newRotateGatewayKeyCmd())
	cmd.AddCommand(newSetTunnelTokenCmd())
	cmd.AddCommand(newSetPublicURLCmd())
	cmd.AddCommand(newSetModelCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newUsageCmd())
	cmd.AddCommand(newLogLevelCmd())

	return cmd
}

func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runRoot(cmd *cobra.Command) error {
	_ = cmd
	setupLogger()

	opts, err := config.DefaultResolveOpts(portableFlag)
	if err != nil {
		return err
	}
	root, err := config.EnsureDataRoot(opts)
	if err != nil {
		return err
	}

	settings, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := config.Save(root, settings); err != nil {
		return err
	}

	slog.Info("discursive ready",
		"data_root", root,
		"local_port", settings.LocalPort,
		"real_model", settings.RealModel,
		"alias_model", settings.AliasModel,
		"has_moonshot_key", settings.HasMoonshotKey(),
		"has_deepseek_key", settings.HasDeepSeekKey(),
		"gateway_key_masked", crypto.MaskSecret(settings.GatewayKey),
		"version", Version,
	)
	return nil
}

func setupLogger() {
	level := usage.LogLevelFromEnv()
	opts := &slog.HandlerOptions{Level: level}
	// stdout so operators can: go run ./cmd/kimi-cursor start | jq
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}
