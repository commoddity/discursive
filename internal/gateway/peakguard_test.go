package gateway

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func peakTime(hour int, weekday time.Weekday) time.Time {
	// 2026-01-06 is a Tuesday; shift by weekday delta.
	base := time.Date(2026, time.January, 6, hour, 0, 0, 0, time.UTC)
	return base.AddDate(0, 0, int(weekday-time.Tuesday))
}

func TestOpenRouterModelFor(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"deepseek-v4-pro", openRouterPro},
		{"glm-5.3", openRouterPro},
		{"kimi-k3", openRouterPro},
		{"deepseek-v4-flash", openRouterFlash},
		{"glm-4.7", openRouterFlash},
		{"kimi-k2.7-code", openRouterFlash},
		{"thaura", openRouterFlash},
		{"unknown", openRouterFlash},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := openRouterModelFor(tt.model); got != tt.want {
				t.Fatalf("openRouterModelFor(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestPeakNow(t *testing.T) {
	tests := []struct {
		name  string
		model string
		at    time.Time
		want  bool
	}{
		{"deepseek pro peak morning", "deepseek-v4-pro", peakTime(7, time.Tuesday), true},
		{"deepseek flash peak early", "deepseek-v4-flash", peakTime(2, time.Sunday), true},
		{"deepseek off peak", "deepseek-v4-pro", peakTime(12, time.Tuesday), false},
		{"zai peak weekday", "glm-4.7", peakTime(7, time.Friday), true},
		{"zai off peak weekend", "glm-4.7", peakTime(7, time.Saturday), false},
		{"zai off peak weekday", "glm-5.3", peakTime(12, time.Monday), false},
		{"moonshot never peak", "kimi-k3", peakTime(8, time.Tuesday), false},
		{"thaura never peak", "thaura", peakTime(8, time.Tuesday), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := peakNow(tt.model, tt.at); got != tt.want {
				t.Fatalf("peakNow(%q, %v) = %v, want %v", tt.model, tt.at, got, tt.want)
			}
		})
	}
}

func TestApplyPeakReroute(t *testing.T) {
	withKey := func() *Server {
		s := &Server{settings: &config.AppSettings{}}
		_ = s.settings.SetOpenRouterKey(t.TempDir(), "sk-or")
		return s
	}
	withoutKey := func() *Server {
		return &Server{settings: &config.AppSettings{}}
	}

	tests := []struct {
		name    string
		s       *Server
		model   string
		at      time.Time
		want    string
		reroute bool
	}{
		{"deepseek big peak with key", withKey(), "deepseek-v4-pro", peakTime(8, time.Tuesday), openRouterPro, true},
		{"deepseek small peak with key", withKey(), "deepseek-v4-flash", peakTime(8, time.Tuesday), openRouterFlash, true},
		{"zai big peak with key", withKey(), "glm-5.3", peakTime(7, time.Wednesday), openRouterPro, true},
		{"zai small peak with key", withKey(), "glm-4.7", peakTime(7, time.Wednesday), openRouterFlash, true},
		{"peak without key falls through", withoutKey(), "deepseek-v4-pro", peakTime(8, time.Tuesday), "deepseek-v4-pro", false},
		{"off peak unchanged", withKey(), "deepseek-v4-pro", peakTime(12, time.Tuesday), "deepseek-v4-pro", false},
		{"non-peak provider unchanged", withKey(), "kimi-k3", peakTime(8, time.Tuesday), "kimi-k3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rerouted := tt.s.applyPeakReroute(tt.model, "req", tt.at)
			if got != tt.want || rerouted != tt.reroute {
				t.Fatalf("applyPeakReroute(%q, %v) = (%q, %v), want (%q, %v)",
					tt.model, tt.at, got, rerouted, tt.want, tt.reroute)
			}
		})
	}
}

func TestOpenRouterTargetsResolveToOpenRouter(t *testing.T) {
	for _, id := range []string{openRouterFlash, openRouterPro} {
		route, err := ResolveModel(id)
		if err != nil {
			t.Fatalf("ResolveModel(%q): %v", id, err)
		}
		if route.Provider != config.ProviderOpenRouter {
			t.Fatalf("%q resolved to provider %s, want openrouter", id, route.Provider)
		}
		if route.Policy != PolicyDeepSeek {
			t.Fatalf("%q resolved to policy %d, want PolicyDeepSeek", id, route.Policy)
		}
	}
}
