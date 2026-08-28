package gateway

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func peakTime(hour int, weekday time.Weekday) time.Time {
	base := time.Date(2026, time.January, 6, hour, 0, 0, 0, time.UTC)
	return base.AddDate(0, 0, int(weekday-time.Tuesday))
}

func TestOpenRouterModelFor(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"deepseek-v4-pro", config.ModelOpenRouterDeepSeekV4Pro},
		{"glm-5.3", config.ModelOpenRouterZaiGLM53},
		{"kimi-k3", "kimi-k3"},
		{"deepseek-v4-flash", config.ModelOpenRouterDeepSeekV4Flash},
		{"deepseek-v4-flash-vision-exp", config.ModelOpenRouterDeepSeekV4Flash},
		{"glm-5.3-flash", config.ModelOpenRouterZaiGLM53Flash},
		{"glm-4.7", config.ModelOpenRouterZaiGLM53Flash},
		{"kimi-k2.7-code", "kimi-k2.7-code"},
		{"thaura", "thaura"},
		{"unknown", "unknown"},
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
		{"deepseek flash peak early sunday pre-cutover", "deepseek-v4-flash", peakTime(2, time.Sunday), true},
		{"deepseek flash sunday post-cutover off-peak", "deepseek-v4-flash", time.Date(2026, 8, 23, 7, 0, 0, 0, time.UTC), false},
		{"deepseek flash saturday post-cutover off-peak", "deepseek-v4-flash", time.Date(2026, 8, 29, 7, 0, 0, 0, time.UTC), false},
		{"deepseek off peak", "deepseek-v4-pro", peakTime(12, time.Tuesday), false},
		{"zai peak weekday", "glm-5.3-flash", peakTime(7, time.Friday), true},
		{"zai off peak weekend", "glm-5.3-flash", peakTime(7, time.Saturday), false},
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

func TestPeakNowForcePeak(t *testing.T) {
	t.Setenv(EnvForcePeak, "1")
	offpeak := peakTime(12, time.Tuesday)
	for model, want := range map[string]bool{
		"deepseek-v4-pro":              true,
		"deepseek-v4-flash":            true,
		"deepseek-v4-flash-vision-exp": true,
		"glm-5.3":                      true,
		"glm-5.3-flash":                true,
		"glm-4.7":                      true,
		"kimi-k3":                      false,
		"kimi-k2.7-code":               false,
		"thaura":                       false,
	} {
		if got := peakNow(model, offpeak); got != want {
			t.Fatalf("peakNow(%q, offpeak) under force-peak = %v, want %v", model, got, want)
		}
	}
}

func TestApplyPeakReroute(t *testing.T) {
	withKey := func() *Server {
		s := &Server{settings: &config.AppSettings{}}
		_ = s.settings.SetOpenRouterKey(t.TempDir(), "sk-or")
		s.openRouterPeakUsable = func() bool { return true }
		return s
	}
	withoutKey := func() *Server {
		return &Server{settings: &config.AppSettings{}}
	}
	zeroBalance := func() *Server {
		s := withKey()
		s.openRouterPeakUsable = func() bool { return false }
		return s
	}

	tests := []struct {
		name    string
		s       *Server
		model   string
		at      time.Time
		want    string
		reroute bool
	}{
		{"deepseek big peak with key", withKey(), "deepseek-v4-pro", peakTime(8, time.Tuesday), config.ModelOpenRouterDeepSeekV4Pro, true},
		{"deepseek small peak with key", withKey(), "deepseek-v4-flash-vision-exp", peakTime(8, time.Tuesday), config.ModelOpenRouterDeepSeekV4Flash, true},
		{"zai big peak with key", withKey(), "glm-5.3", peakTime(7, time.Wednesday), config.ModelOpenRouterZaiGLM53, true},
		{"zai small peak with key", withKey(), "glm-5.3-flash", peakTime(7, time.Wednesday), config.ModelOpenRouterZaiGLM53Flash, true},
		{"peak without key falls through", withoutKey(), "deepseek-v4-pro", peakTime(8, time.Tuesday), "deepseek-v4-pro", false},
		{"peak zero balance falls through", zeroBalance(), "deepseek-v4-pro", peakTime(8, time.Tuesday), "deepseek-v4-pro", false},
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
	for _, id := range []string{config.ModelOpenRouterDeepSeekV4Flash, config.ModelOpenRouterDeepSeekV4Pro, config.ModelOpenRouterZaiGLM53, config.ModelOpenRouterZaiGLM53Flash} {
		route, err := ResolveModel(id)
		if err != nil {
			t.Fatalf("ResolveModel(%q): %v", id, err)
		}
		if route.Provider != config.ProviderOpenRouter {
			t.Fatalf("%q resolved to provider %s, want openrouter", id, route.Provider)
		}
	}
}

func TestOpenRouterOnlyDuringPeak(t *testing.T) {
	withKey := func() *Server {
		s := &Server{settings: &config.AppSettings{}}
		_ = s.settings.SetOpenRouterKey(t.TempDir(), "sk-or")
		return s
	}

	t.Run("guard corrects DeepSeek OpenRouter id when not forced", func(t *testing.T) {
		s := withKey()
		if peakNow("deepseek-v4-flash", time.Now()) {
			t.Skip("test run during real DeepSeek peak hours; guard check non-deterministic")
		}
		if got := s.isPeakAllowedOpenRouter(config.ModelOpenRouterDeepSeekV4Flash, "req"); got != config.ModelDeepSeekV4FlashVisionExp {
			t.Fatalf("guard returned %q, want corrected %s", got, config.ModelDeepSeekV4FlashVisionExp)
		}
	})
	t.Run("guard corrects Z.AI OpenRouter id when not forced", func(t *testing.T) {
		s := withKey()
		if peakNow("glm-5.3-flash", time.Now()) {
			t.Skip("test run during real Z.AI peak hours; guard check non-deterministic")
		}
		if got := s.isPeakAllowedOpenRouter(config.ModelOpenRouterZaiGLM53Flash, "req"); got != config.ModelZaiGLM53Flash {
			t.Fatalf("guard returned %q, want corrected %s", got, config.ModelZaiGLM53Flash)
		}
	})
	t.Run("guard keeps DeepSeek OpenRouter id when peak forced", func(t *testing.T) {
		t.Setenv(EnvForcePeak, "1")
		s := withKey()
		if got := s.isPeakAllowedOpenRouter(config.ModelOpenRouterDeepSeekV4Flash, "req"); got != config.ModelOpenRouterDeepSeekV4Flash {
			t.Fatalf("guard returned %q, want %s under forced peak", got, config.ModelOpenRouterDeepSeekV4Flash)
		}
	})
	t.Run("openRouterTwinFor maps real models", func(t *testing.T) {
		if twin, ok := openRouterTwinFor(config.ModelDeepSeekV4Flash); !ok || twin != config.ModelOpenRouterDeepSeekV4Flash {
			t.Fatalf("openRouterTwinFor flash = %q, %v", twin, ok)
		}
	})
}
