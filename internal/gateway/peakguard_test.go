package gateway

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func peakClock(hour int) func() time.Time {
	return func() time.Time {
		return time.Date(2026, time.January, 6, hour, 0, 0, 0, time.UTC)
	}
}

func TestApplyPeakGuard_RedirectsDuringPeakHours(t *testing.T) {
	s := &Server{settings: &config.AppSettings{PeakGuardEnabled: true}}
	tests := []struct {
		name  string
		model string
		hour  int
		want  string
		fired bool
	}{
		{"deepseek pro at peak", "deepseek-v4-pro", 8, "glm-4.7", true},
		{"deepseek flash at peak", "deepseek-v4-flash", 2, "glm-4.7", true},
		{"deepseek flash early peak", "deepseek-v4-flash", 1, "glm-4.7", true},
		{"deepseek pro late peak", "deepseek-v4-pro", 9, "glm-4.7", true},
		{"deepseek pro off-peak", "deepseek-v4-pro", 12, "deepseek-v4-pro", false},
		{"deepseek flash off-peak", "deepseek-v4-flash", 23, "deepseek-v4-flash", false},
		{"non-deepseek never redirected", "glm-4.7", 8, "glm-4.7", false},
		{"moonshot never redirected", "kimi-k3", 8, "kimi-k3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fired := s.applyPeakGuard(tt.model, "req", peakClock(tt.hour))
			if got != tt.want || fired != tt.fired {
				t.Fatalf("applyPeakGuard(%q, hour=%d) = (%q, %v), want (%q, %v)",
					tt.model, tt.hour, got, fired, tt.want, tt.fired)
			}
		})
	}
}

func TestApplyPeakGuard_RespectsToggle(t *testing.T) {
	enabled := &Server{settings: &config.AppSettings{PeakGuardEnabled: true}}
	if got, _ := enabled.applyPeakGuard("deepseek-v4-pro", "req", peakClock(8)); got != "glm-4.7" {
		t.Fatalf("guard enabled at peak should redirect, got %q", got)
	}
	disabled := &Server{settings: &config.AppSettings{PeakGuardEnabled: false}}
	if model, fired := disabled.applyPeakGuard("deepseek-v4-pro", "req", peakClock(8)); fired || model != "deepseek-v4-pro" {
		t.Fatalf("guard disabled should never redirect, got (%q, %v)", model, fired)
	}
}

func TestIsDeepSeekModel(t *testing.T) {
	for model, want := range map[string]bool{
		"deepseek-v4-pro":   true,
		"deepseek-v4-flash": true,
		"glm-4.7":           false,
		"kimi-k3":           false,
		"thaura":            false,
	} {
		if got := isDeepSeekModel(model); got != want {
			t.Fatalf("isDeepSeekModel(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestPeakRedirectMapTargetsResolveToZAI(t *testing.T) {
	for from, to := range peakRedirectMap {
		route, err := ResolveModel(to)
		if err != nil {
			t.Fatalf("peak redirect target %q (from %q) not resolvable: %v", to, from, err)
		}
		if route.Provider != config.ProviderZai {
			t.Fatalf("peak redirect target %q resolves to provider %s, want zai", to, route.Provider)
		}
	}
}
