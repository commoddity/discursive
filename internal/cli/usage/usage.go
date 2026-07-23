package usage

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
	usagepkg "github.com/commoddity/discursive/internal/usage"
)

// NewCmd returns the usage subcommand (includes purge).
func NewCmd(portable func() bool) *cobra.Command {
	var dateFlag, sessionFlag string
	var daysFlag int

	cmd := &cobra.Command{
		Use:   "usage",
		Short: "💰 Print session usage and cost estimates (from stored events)",
		Long: `💰  Show daily or session usage with model breakdowns and Cursor reference
pricing.  Defaults to today's usage.  Use --date for a specific day,
--session for a session breakdown, or --days for the last N days.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			store, err := usagepkg.NewStore(dataRoot)
			if err != nil {
				return err
			}

			switch {
			case sessionFlag != "":
				ds, err := store.QuerySessionDetail(sessionFlag)
				if err != nil {
					return fmt.Errorf("query session: %w", err)
				}
				return util.EmitPretty(ds)

			case daysFlag > 0:
				summaries, err := store.QueryLastNDays(daysFlag)
				if err != nil {
					return fmt.Errorf("query last %d days: %w", daysFlag, err)
				}
				return util.EmitPretty(summaries)

			default:
				date := dateFlag
				if date == "" {
					date = time.Now().UTC().Format("2006-01-02")
				}
				ds, err := store.QueryDailyTotals(date)
				if err != nil {
					return fmt.Errorf("query daily totals: %w", err)
				}
				return util.EmitPretty(ds)
			}
		},
	}

	cmd.Flags().StringVar(&dateFlag, "date", "", "show usage for a specific date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&sessionFlag, "session", "", "show usage for a specific session ID")
	cmd.Flags().IntVar(&daysFlag, "days", 0, "show usage for the last N days (array of daily summaries)")

	cmd.AddCommand(newPurgeCmd(portable))
	return cmd
}

func newPurgeCmd(portable func() bool) *cobra.Command {
	var maxAgeFlag string
	var dryRunFlag bool

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "🗑️  Delete usage events older than a max age",
		Long: `🗑️  Purge usage events older than --max-age.

Supports Go duration strings: 24h, 7d, 90d, 30d, etc.
Use --dry-run to see how many events would be deleted without actually deleting.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			store, err := usagepkg.NewStore(dataRoot)
			if err != nil {
				return err
			}

			dur, err := time.ParseDuration(maxAgeFlag)
			if err != nil {
				if len(maxAgeFlag) > 1 && maxAgeFlag[len(maxAgeFlag)-1] == 'd' {
					daysStr := maxAgeFlag[:len(maxAgeFlag)-1]
					if daysVal, err2 := time.ParseDuration(daysStr + "h"); err2 == nil {
						dur = daysVal * 24
					} else {
						return fmt.Errorf("invalid --max-age %q: must be a Go duration (24h, 90d, etc.)", maxAgeFlag)
					}
				} else {
					return fmt.Errorf("invalid --max-age %q: %w", maxAgeFlag, err)
				}
			}

			cutoff := time.Now().UTC().Add(-dur)
			n, err := store.DeleteEventsBefore(cutoff)
			if err != nil {
				return err
			}

			if dryRunFlag {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would delete %d events older than %v (%s ago)\n", n, cutoff.Format("2006-01-02 15:04"), maxAgeFlag)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d events older than %v (%s ago)\n", n, cutoff.Format("2006-01-02 15:04"), maxAgeFlag)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&maxAgeFlag, "max-age", "90d", "delete events older than this duration (e.g. 24h, 7d, 30d, 90d)")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "show what would be deleted without actually deleting")
	return cmd
}
