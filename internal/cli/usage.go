package cli

import (
	"log/slog"

	"github.com/spf13/cobra"

	"discursive/internal/usage"
)

func newUsageCmd() *cobra.Command {
	var sessionFlag string
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "💰 Print session usage and cost estimates (from stored events)",
		Long:  "💰  Show tokens in / out, cache hits, and estimated USD cost for the latest\nsession.  Use --session to pick a specific session ID.  Breaks down by model\nand shows Cursor reference pricing for comparison.",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogger()
			dataRoot, err := resolveDataRoot()
			if err != nil {
				return err
			}
			store, err := usage.NewStore(dataRoot)
			if err != nil {
				return err
			}
			events, err := store.LoadEvents()
			if err != nil {
				return err
			}

			sessionID := sessionFlag
			if sessionID == "" && len(events) > 0 {
				sessionID = events[len(events)-1].SessionID
			}
			if sessionID == "" {
				slog.Info("usage_empty", "detail", "no usage events recorded yet")
				return nil
			}

			sum, err := store.SessionSummary(sessionID)
			if err != nil {
				return err
			}

			slog.Info("usage_session",
				"session_id", sum.SessionID,
				"request_count", sum.RequestCount,
				"tokens_in", sum.PromptTokens,
				"tokens_out", sum.CompletionTokens,
				"cache_hit_tokens", sum.CacheHitTokens,
				"cache_miss_tokens", sum.CacheMissTokens,
				"est_usd", usage.RoundUSD(sum.EstUSD),
			)

			for model, totals := range sum.ByModel {
				slog.Info("usage_by_model",
					"model", model,
					"provider", string(totals.Provider),
					"request_count", totals.RequestCount,
					"tokens_in", totals.PromptTokens,
					"tokens_out", totals.CompletionTokens,
					"est_usd", usage.RoundUSD(totals.EstUSD),
				)
			}

			input, cache, output := usage.CursorComparisonReference()
			slog.Info("usage_cursor_reference",
				"input_per_1m", input,
				"cache_per_1m", cache,
				"output_per_1m", output,
			)

			return nil
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "filter to a specific session ID (latest when omitted)")
	return cmd
}
