package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "🩺 Run health checks (keys, port, tunnel, cloudflared, logs)",
		Long: `🩺  Health checks covering: API keys present? Gateway key valid? Local port free?
Tunnel token saved? cloudflared binary found? Log dir writable?

Outputs a single pretty-printed JSON object with all check results.
Exits non-zero if any check fails.`,
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
			report := doctor.RunAll(settings, dataRoot)

			if err := emitPretty(report); err != nil {
				return err
			}

			if !report.OK {
				return fmt.Errorf("doctor: %d check(s) failed", countFailed(report))
			}
			return nil
		},
	}
}

func countFailed(report doctor.Report) int {
	n := 0
	for _, c := range report.Checks {
		if !c.OK {
			n++
		}
	}
	return n
}
