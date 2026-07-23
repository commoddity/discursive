package start

import (
	"strings"

	"github.com/commoddity/discursive/internal/config"
)

// ApplyStartFlags persists CLI overrides onto settings before save.
func ApplyStartFlags(settings *config.AppSettings, tunnelFlag, publicURLFlag string) error {
	if strings.TrimSpace(tunnelFlag) != "" {
		settings.TunnelMode = config.NormalizeTunnelMode(tunnelFlag)
	}
	if strings.TrimSpace(publicURLFlag) != "" {
		norm, err := config.NormalizePublicBaseURL(publicURLFlag)
		if err != nil {
			return err
		}
		settings.PublicBaseURL = norm
	}
	return nil
}
