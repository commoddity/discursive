package gateway

import (
	"log/slog"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

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
// Z.AI models peak Mon–Fri 06:00–10:00 UTC; DeepSeek models peak daily
// 01:00–04:00 and 06:00–10:00 UTC (only after the pricing cutover).
func peakNow(model string, at time.Time) bool {
	switch {
	case isZaiModel(model):
		return usage.ZaiPeakHours(at)
	case isDeepSeekModel(model):
		return usage.DeepSeekPeakHours(at.UTC().Hour())
	default:
		return false
	}
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
