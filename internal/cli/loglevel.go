package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/usage"
)

func newLogLevelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log-level [debug|info|warn|error]",
		Short: "🔊 Show or set log verbosity (debug / info / warn / error)",
		Long: `🔊  Show or set log verbosity

  discursive log-level          # show current level
  discursive log-level debug    # set to debug (lots of detail)
  discursive log-level info     # set to info (default)
  discursive log-level warn     # set to warn (warnings only)
  discursive log-level error    # set to error (errors only)

Levels are stored in the DISCURSIVE_LOG_LEVEL environment variable.
Use export DISCURSIVE_LOG_LEVEL=debug in your shell profile to persist.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			current := usage.LogLevelFromEnv()
			currentName := slogLevelName(current)

			if len(args) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  Log level: %s\n",
					levelEmoji(current), currentName)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "   💡  Set with: %s\n",
					"discursive log-level <debug|info|warn|error>")
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "   💡  Persist with: %s\n",
					"export DISCURSIVE_LOG_LEVEL=debug")
				return nil
			}

			requested := strings.ToLower(strings.TrimSpace(args[0]))
			level := usage.ParseLogLevel(requested)
			levelName := slogLevelName(level)

			// Validate that the string maps correctly.
			if requested != levelName && requested != "warning" {
				return fmt.Errorf("unknown log level %q — use: debug, info, warn, error", args[0])
			}
			if requested == "warning" {
				levelName = "warn"
			}

			if err := os.Setenv(usage.EnvLogLevel, levelName); err != nil {
				return fmt.Errorf("set env: %w", err)
			}
			reloadLogger(level)

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  Log level set to %s\n",
				levelEmoji(level), levelName)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "   💡  Current process: %s\n", levelName)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "   💡  To persist across sessions:\n")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "          export DISCURSIVE_LOG_LEVEL=%s\n", levelName)
			return nil
		},
	}
}

func levelEmoji(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "🐛"
	case l <= slog.LevelInfo:
		return "ℹ️"
	case l <= slog.LevelWarn:
		return "⚠️"
	default:
		return "🚨"
	}
}

func slogLevelName(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "debug"
	case l <= slog.LevelInfo:
		return "info"
	case l <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

func reloadLogger(level slog.Level) {
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}
