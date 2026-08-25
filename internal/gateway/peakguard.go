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

// isZaiModel reports whether a real model id is served by Z.AI.
func isZaiModel(model string) bool {
	p, ok := config.ProviderForModel(model)
	return ok && p == config.ProviderZai
}

// isDeepSeekModel reports whether a real model id is served by DeepSeek.
func isDeepSeekModel(model string) bool {
	p, ok := config.ProviderForModel(model)
	return ok && p == config.ProviderDeepSeek
}

// peakNow reports whether model is in peak billing for its provider at at.
// Z.AI models peak Mon–Fri 06:00–10:00 UTC; DeepSeek models peak on Beijing
// weekdays 01:00–04:00 and 06:00–10:00 UTC (Beijing weekends off-peak all day
// from 2026-08-23 00:00 Beijing).
// When EnvForcePeak is set, any peak-eligible model (DeepSeek, Z.AI) is
// treated as in peak for testing.
func peakNow(model string, at time.Time) bool {
	if forcePeak() {
		spec, ok := config.ProviderSpecFor(providerForPeak(model))
		if ok && spec.HasPeak {
			return true
		}
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

func providerForPeak(model string) config.Provider {
	if _, p, ok := config.OpenRouterRealFor(model); ok {
		return p
	}
	if p, ok := config.ProviderForModel(model); ok {
		return p
	}
	return ""
}

// openRouterModelFor maps a requested real model id to the appropriate
// OpenRouter twin for its provider during peak hours.
func openRouterModelFor(requestedModel string) string {
	provider, ok := config.ProviderForModel(requestedModel)
	if !ok {
		return requestedModel
	}
	spec, ok := config.ProviderSpecFor(provider)
	if !ok || !spec.HasPeak {
		return requestedModel
	}
	target := spec.SmallModel
	if config.IsBigModel(provider, requestedModel) {
		target = spec.BigModel
	}
	if twin, ok := config.OpenRouterTwinFor(target); ok {
		return twin
	}
	return requestedModel
}

// isPeakAllowedOpenRouter enforces the hard rule at send time: OpenRouter may
// only be used when the equivalent real provider is in peak billing (or peak
// is forced via DISCURSIVE_FORCE_PEAK). It returns the corrected model — the
// input when usage is allowed, or the real model when it is not — logging
// loudly on correction.
func (s *Server) isPeakAllowedOpenRouter(model, requestID string) string {
	real, provider, ok := config.OpenRouterRealFor(model)
	if !ok {
		return model
	}
	spec, ok := config.ProviderSpecFor(provider)
	if !ok || !spec.HasPeak {
		return model
	}
	var probe string
	if config.IsBigModel(provider, real) {
		probe = spec.BigModel
	} else {
		probe = spec.SmallModel
	}
	if peakNow(probe, time.Now()) {
		return model
	}
	slog.Error("openrouter_guard: off-peak OpenRouter usage blocked, rerouting to real provider",
		"request_id", requestID, "model", model, "corrected", real, "provider", provider)
	return real
}

// applyPeakReroute reroutes non-downgraded traffic to OpenRouter twins when
// the requested model's provider is in peak hours and an OpenRouter key is
// configured. Moonshot and Thaura never peak. It returns the (possibly
// unchanged) model and whether a reroute happened.
func (s *Server) applyPeakReroute(model, requestID string, at time.Time) (string, bool) {
	provider, ok := config.ProviderForModel(model)
	if !ok {
		return model, false
	}
	spec, ok := config.ProviderSpecFor(provider)
	if !ok || !spec.HasPeak || !peakNow(model, at) {
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
