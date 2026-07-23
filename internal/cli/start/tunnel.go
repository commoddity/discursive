package start

import (
	"fmt"
	"log/slog"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/tunnel"
)

// BuildTunnelConfig constructs the tunnel supervisor config from app settings.
func BuildTunnelConfig(settings config.AppSettings, dataRoot, publicURL string) (*tunnel.Config, error) {
	mode := config.NormalizeTunnelMode(settings.TunnelMode)
	cfg := &tunnel.Config{
		Port:      settings.LocalPort,
		DataRoot:  dataRoot,
		PublicURL: publicURL,
		OnURLChange: func(u string) {
			slog.Info("tunnel ready",
				"public_url", u,
				"tunnel_mode", mode,
				"gateway_key", settings.GatewayKey,
				"msg", "Set this URL and key in Cursor Settings → Models → OpenAI API Key / Override Base URL",
			)
		},
	}
	switch mode {
	case config.TunnelModeNamed:
		cfg.Mode = tunnel.ModeNamed
		tok, err := settings.GetTunnelToken(dataRoot)
		if err != nil {
			return nil, err
		}
		if tok == nil || *tok == "" {
			return nil, fmt.Errorf("tunnel mode named requires tunnel token in config (run set --tunnel-token)")
		}
		cfg.Token = *tok
	case config.TunnelModeNone:
		cfg.Mode = tunnel.ModeNone
	case config.TunnelModeQuick:
		cfg.Mode = tunnel.ModeQuick
	default:
		return nil, fmt.Errorf("unknown tunnel mode %q", mode)
	}
	return cfg, nil
}
