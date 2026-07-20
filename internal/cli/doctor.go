package cli

import (
	"fmt"
	"log/slog"

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

Each check is a JSON line on stdout — pipe through | jq . for readability.
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
			for _, check := range report.Checks {
				if check.OK {
					slog.Info("doctor_check", "name", check.Name, "ok", true, "detail", check.Detail)
				} else {
					slog.Warn("doctor_check", "name", check.Name, "ok", false, "detail", check.Detail)
				}
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
