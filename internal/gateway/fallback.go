package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"log/slog"
)

// Quota-exhaustion fallback:
//
//	plan (zai glm-4.7 / glm-5.3) normally
//	  → exhausted → deepseek-v4-flash (direct), or OpenRouter flash
//	    during DeepSeek peak hours
//
// "Exhausted" is detected per-request from upstream Z.AI error responses
// (429 with codes 1305 "overloaded" / 1113 "insufficient balance") — the plan
// has no quota API we can poll cheaply.
//
// HARD RULE: OpenRouter is used only when the target provider (DeepSeek) is in
// peak billing at that moment. At all other times every fallback lands on the
// real provider's real model.

// isZaiQuotaError reports whether an upstream status/body indicates the Z.AI
// coding-plan quota is exhausted or the service is rejecting plan traffic
// (429 with code 1305 or 1113).
func isZaiQuotaError(status int, body []byte) bool {
	if status != http.StatusTooManyRequests {
		return false
	}
	var obj struct {
		Error struct {
			Code float64 `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return false
	}
	return obj.Error.Code == 1305 || obj.Error.Code == 1113
}

// realFallbackModel maps a requested model id to its real-provider fallback:
// big models (glm-5.3, deepseek-v4-pro, kimi-k3) → deepseek-v4-pro, everything
// else → deepseek-v4-flash. Never returns an OpenRouter id.
func realFallbackModel(originalModel string) string {
	if openRouterBigModels[originalModel] {
		return "deepseek-v4-pro"
	}
	return "deepseek-v4-flash"
}

// fallbackTargetFor is the single mapping every fallback/overflow path uses.
// It returns the real DeepSeek model, substituting the OpenRouter equivalent
// only when the target provider is in peak billing right now (per-provider
// windows; DISCURSIVE_FORCE_PEAK counts as peak) and an OpenRouter key exists.
func (s *Server) fallbackTargetFor(originalModel, requestID string) string {
	real := realFallbackModel(originalModel)
	if s.settings != nil && s.settings.HasOpenRouterKey() && peakNow(real, time.Now()) {
		slog.Info("fallback_target: deepseek peak, using openrouter",
			"request_id", requestID, "real", real, "target", openRouterTargets[real])
		return openRouterTargets[real]
	}
	return real
}

// applyFallback computes the fallback model for a failed Z.AI plan request.
// Quota exhaustion always lands on flash (the plan already rejected the
// request regardless of model size), peak-gated to OpenRouter flash.
func (s *Server) applyFallback(requestID string) string {
	return s.fallbackTargetFor("deepseek-v4-flash", requestID)
}

// applyFallbackWithModel computes the fallback model for a Z.AI lane overflow.
// Lane overflow preserves model size: big requested models (glm-5.3,
// deepseek-v4-pro, kimi-k3) fall back to deepseek-v4-pro, small models to
// deepseek-v4-flash — peak-gated to the OpenRouter equivalents.
func (s *Server) applyFallbackWithModel(originalModel, requestID string) string {
	return s.fallbackTargetFor(originalModel, requestID)
}
