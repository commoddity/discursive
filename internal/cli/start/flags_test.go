package start

import (
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestApplyStartFlagsTunnel(t *testing.T) {
	s := config.DefaultSettings()
	if err := ApplyStartFlags(&s, "quick", ""); err != nil {
		t.Fatal(err)
	}
	if s.TunnelMode != config.TunnelModeQuick {
		t.Fatalf("tunnel mode %q", s.TunnelMode)
	}
}

func TestApplyStartFlagsPublicURL(t *testing.T) {
	s := config.DefaultSettings()
	if err := ApplyStartFlags(&s, "", "https://gw.example.com/v1"); err != nil {
		t.Fatal(err)
	}
	if s.PublicBaseURL != "https://gw.example.com/v1" {
		t.Fatalf("public url %q", s.PublicBaseURL)
	}
}

func TestApplyStartFlagsInvalidURL(t *testing.T) {
	s := config.DefaultSettings()
	if err := ApplyStartFlags(&s, "", "not-a-url"); err == nil {
		t.Fatal("expected error")
	}
}
