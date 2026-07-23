package start

import (
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/tunnel"
)

func TestBuildTunnelConfigQuick(t *testing.T) {
	s := config.DefaultSettings()
	s.TunnelMode = config.TunnelModeQuick
	cfg, err := BuildTunnelConfig(s, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != tunnel.ModeQuick {
		t.Fatalf("mode %q", cfg.Mode)
	}
}

func TestBuildTunnelConfigNamedRequiresToken(t *testing.T) {
	s := config.DefaultSettings()
	s.TunnelMode = config.TunnelModeNamed
	s.PublicBaseURL = "https://ai.example.com/v1"
	_, err := BuildTunnelConfig(s, t.TempDir(), s.PublicBaseURL)
	if err == nil {
		t.Fatal("expected error without token")
	}
}
