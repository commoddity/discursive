package start

import (
	"github.com/spf13/cobra"
)

// Options wires start from the root command without import cycles.
type Options struct {
	Version  string
	Portable func() bool
	RunSetup func(cmd *cobra.Command, fromStart bool) error
}

// NewCmd returns the start subcommand.
func NewCmd(opts Options) *cobra.Command {
	var tunnelFlag, publicURLFlag, logLevelFlag string
	var backgroundFlag, bgChildFlag bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "🚀 Start the gateway on 127.0.0.1 + launch the Cloudflare tunnel",
		Long: `🚀  Listen on 127.0.0.1 (default port 4001) and supervise the Cloudflare tunnel.

On first run the interactive setup wizard runs automatically if config is
incomplete.  Logs are JSON on stdout — pipe through | jq .
Use Ctrl-C to stop cleanly.

  --background    Detach and run in the background.  Logs go to
                  {dataRoot}/gateway.log — use 'discursive logs' to watch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, opts, tunnelFlag, publicURLFlag, logLevelFlag, backgroundFlag, bgChildFlag)
		},
	}
	cmd.Flags().StringVar(&tunnelFlag, "tunnel", "", "tunnel mode: named, none, or quick (persists to config)")
	cmd.Flags().StringVar(&publicURLFlag, "public-url", "", "public HTTPS base URL ending in /v1 (persists to config)")
	cmd.Flags().StringVar(&logLevelFlag, "log-level", "", "log verbosity: debug, info, warn, error (overrides DISCURSIVE_LOG_LEVEL)")
	cmd.Flags().BoolVar(&backgroundFlag, "background", false, "detach and run in the background")
	cmd.Flags().BoolVar(&bgChildFlag, "_bg", false, "")
	_ = cmd.Flags().MarkHidden("_bg")
	_ = cmd.RegisterFlagCompletionFunc("tunnel", cobra.FixedCompletions(
		[]string{"named", "none", "quick"}, cobra.ShellCompDirectiveNoFileComp,
	))
	_ = cmd.RegisterFlagCompletionFunc("log-level", cobra.FixedCompletions(
		[]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp,
	))
	return cmd
}
