package gateway

import (
	"log/slog"
	"os"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

// EnvForcePeak makes the gateway behave as if the current time is a peak
// billing hour for every peak-eligible model (DeepSeek, Z.AI). Any non-empty
// value (e.g. "1") enables it. Used to test peak reroute/fallback behavior
// outside real peak hours, without changing the system clock.
const EnvForcePeak = "DISCURSIVE_FORCE_PEAK"

// forcePeak reports whether peak behavior is forced via env for testing.
func forcePeak() bool {
	return os.Getenv(EnvForcePeak) != ""
}

// openRouterFlash is the small-model target for all OpenRouter fallback traffic.
const openRouterFlash = "deepseek/deepseek-v4-flash-0731"

// openRouterPro is the big-model target for peak reroutes of non-downgraded
// requests that originally asked for a big model.
const openRouterPro = "deepseek/deepseek-v4-pro-0813"

// openRouterBigModels is the set of requested real model ids that should be
// rerouted to openRouterPro during peak hours. Everything else goes to flash.
var openRouterBigModels = map[string]bool{
	"deepseek-v4-pro": true,
	"glm-5.3":         true,
	"kimi-k3":         true,
}

// openRouterModelFor maps any requested real model id to the appropriate
// OpenRouter DeepSeek target: big models → pro, everything else → flash.
func openRouterModelFor(requestedModel string) string {
	if openRouterBigModels[requestedModel] {
		return openRouterPro
	}
	return openRouterFlash
}

// isZaiModel reports whether a real model id is served by Z.AI.
func isZaiModel(model string) bool {
	switch model {
	case "glm-5.3", "glm-5.2", "glm-5-turbo", "glm-4.7", "glm-4.6v":
		return true
	default:
		return false
	}
}

// isDeepSeekModel reports whether a real model id is served by DeepSeek.
func isDeepSeekModel(model string) bool {
	switch model {
	case "deepseek-v4-pro", "deepseek-v4-flash":
		return true
	default:
		return false
	}
}

// peakNow reports whether model is in peak billing for its provider at at.
// Z.AI models peak Mon–Fri 06:00–10:00 UTC; DeepSeek models peak on Beijing
// weekdays 01:00–04:00 and 06:00–10:00 UTC (Beijing weekends off-peak all day
// from 2026-08-23 00:00 Beijing).
// When EnvForcePeak is set, any peak-eligible model (DeepSeek, Z.AI) is
// treated as in peak for testing.
func peakNow(model string, at time.Time) bool {
	if forcePeak() && (isZaiModel(model) || isDeepSeekModel(model)) {
		return true
	}
	switch {
	case isZaiModel(model):
		return usage.ZaiPeakHours(at)
	case isDeepSeekModel(model):
		return usage.DeepSeekPeakHours(at)
	default:
		return false
	}
}

// openRouterTargets maps real DeepSeek model ids to their OpenRouter
// equivalents. This is the ONLY sanctioned OpenRouter substitution table;
// OpenRouter ids appear at runtime exclusively through it (peak-gated).
var openRouterTargets = map[string]string{
	"deepseek-v4-flash": openRouterFlash,
	"deepseek-v4-pro":   openRouterPro,
}

// openRouterRealFor returns the real DeepSeek model for an OpenRouter id ("" if unknown).
func openRouterRealFor(id string) string {
	for real, or := range openRouterTargets {
		if or == id {
			return real
		}
	}
	return ""
}

// isPeakAllowedOpenRouter enforces the hard rule at send time: OpenRouter may
// only be used when the equivalent real provider is in peak billing (or peak
// is forced via DISCURSIVE_FORCE_PEAK). It returns the corrected model — the
// input when usage is allowed, or the real DeepSeek model when it is not —
// logging loudly on correction.
func (s *Server) isPeakAllowedOpenRouter(model, requestID string) string {
	if openRouterRealFor(model) == "" {
		return model // not an OpenRouter id
	}
	if peakNow("deepseek-v4-flash", time.Now()) {
		return model // DeepSeek is in peak (or forced): OpenRouter allowed
	}
	real := openRouterRealFor(model)
	slog.Error("openrouter_guard: off-peak OpenRouter usage blocked, rerouting to real provider",
		"request_id", requestID, "model", model, "corrected", real)
	return real
}

// applyPeakReroute reroutes non-downgraded traffic to OpenRouter DeepSeek models
// when the requested model's provider is in peak hours and an OpenRouter key is
// configured. Big requested models become OR pro; small models become OR flash.
// It returns the (possibly unchanged) model and whether a reroute happened.
func (s *Server) applyPeakReroute(model, requestID string, at time.Time) (string, bool) {
	if !peakNow(model, at) {
		return model, false
	}
	if s.settings == nil || !s.settings.HasOpenRouterKey() {
		return model, false
	}
	target := openRouterModelFor(model)
	if target == model {
		return model, false
	}
	slog.Info("peak_reroute",
		"request_id", requestID,
		"from", model,
		"to", target,
		"provider", config.ProviderOpenRouter,
	)
	return target, true
}
