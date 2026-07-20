package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/usage"
)

func newUsageCmd() *cobra.Command {
	var dateFlag, sessionFlag string
	var daysFlag int

	cmd := &cobra.Command{
		Use:   "usage",
		Short: "💰 Print session usage and cost estimates (from stored events)",
		Long: `💰  Show daily or session usage with model breakdowns and Cursor reference
pricing.  Defaults to today's usage.  Use --date for a specific day,
--session for a session breakdown, or --days for the last N days.`,
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

			switch {
			case sessionFlag != "":
				ds, err := store.QuerySessionDetail(sessionFlag)
				if err != nil {
					return fmt.Errorf("query session: %w", err)
				}
				return emitPretty(ds)

			case daysFlag > 0:
				summaries, err := store.QueryLastNDays(daysFlag)
				if err != nil {
					return fmt.Errorf("query last %d days: %w", daysFlag, err)
				}
				return emitPretty(summaries)

			default:
				date := dateFlag
				if date == "" {
					date = time.Now().UTC().Format("2006-01-02")
				}
				ds, err := store.QueryDailyTotals(date)
				if err != nil {
					return fmt.Errorf("query daily totals: %w", err)
				}
				return emitPretty(ds)
			}
		},
	}

	cmd.Flags().StringVar(&dateFlag, "date", "", "show usage for a specific date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&sessionFlag, "session", "", "show usage for a specific session ID")
	cmd.Flags().IntVar(&daysFlag, "days", 0, "show usage for the last N days (array of daily summaries)")
	return cmd
}
