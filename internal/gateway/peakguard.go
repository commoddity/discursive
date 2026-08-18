package gateway

import (
	"log/slog"
	"time"

	"github.com/commoddity/discursive/internal/usage"
)

// peakRedirectMap remaps DeepSeek models to GLM models during DeepSeek peak
// hours (a "hard guard": never route to DeepSeek at peak). The GLM Coding Plan
// only supports glm-5.3 / glm-5-turbo / glm-4.7, so both tiers redirect to
// glm-4.7 (the cheapest plan model per the credit table).
var peakRedirectMap = map[string]string{
	"deepseek-v4-flash": "glm-4.7",
	"deepseek-v4-pro":   "glm-4.7",
}

// peakGuardEnabled reports whether the DeepSeek peak-hour guard is currently
// on, reading the live setting when present and otherwise defaulting to the
// persisted settings or the product default (ON).
func (s *Server) peakGuardEnabled() bool {
	if s.live != nil {
		return s.live.PeakGuardEnabled()
	}
	if s.settings != nil {
		return s.settings.PeakGuardEnabled
	}
	return true
}

// isDeepSeekModel reports whether a real model id belongs to the DeepSeek
// provider (i.e. can be peak-redirected).
func isDeepSeekModel(model string) bool {
	switch model {
	case "deepseek-v4-pro", "deepseek-v4-flash":
		return true
	default:
		return false
	}
}

// applyPeakGuard is the DeepSeek peak-hour hard guard. It inspects the final
// served model (after any subagent-router downgrade) and, when the guard is
// enabled and the model is a DeepSeek model during a DeepSeek peak hour,
// remaps it to an equivalent GLM model. It returns the new model (unchanged
// when no redirect applies) and whether a redirect fired. nowUTC is
// injectable for tests; nil defaults to time.Now.
func (s *Server) applyPeakGuard(model, requestID string, nowUTC func() time.Time) (string, bool) {
	if !isDeepSeekModel(model) {
		return model, false
	}
	if !s.peakGuardEnabled() {
		return model, false
	}
	clock := nowUTC
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	if !usage.DeepSeekPeakHours(clock().UTC().Hour()) {
		return model, false
	}
	target, ok := peakRedirectMap[model]
	if !ok || target == model {
		return model, false
	}
	slog.Info("peak_redirect",
		"request_id", requestID,
		"from", model,
		"to", target,
		"reason", "deepseek peak hour",
	)
	return target, true
}
