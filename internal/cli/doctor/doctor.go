package doctor

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/config"
	doctpkg "github.com/commoddity/discursive/internal/doctor"
)

// NewCmd returns the doctor subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "🩺 Run health checks (keys, port, tunnel, cloudflared, logs)",
		Long: `🩺  Health checks covering: API keys present? Gateway key valid? Local port free?
Tunnel token saved? cloudflared binary found? Log dir writable?

Outputs a single pretty-printed JSON object with all check results.
Exits non-zero if any check fails.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			util.SetupLogger()
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			settings, err := config.Load(dataRoot)
			if err != nil {
				return err
			}
			report := doctpkg.RunAll(settings, dataRoot)

			if err := util.EmitPretty(report); err != nil {
				return err
			}

			if !report.OK {
				return fmt.Errorf("doctor: %d check(s) failed", countFailed(report))
			}
			return nil
		},
	}
}

func countFailed(report doctpkg.Report) int {
	n := 0
	for _, c := range report.Checks {
		if !c.OK {
			n++
		}
	}
	return n
}
