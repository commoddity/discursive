package gateway

import (
	"encoding/json"
	"net/http"
)

// Quota-exhaustion fallback:
//
//	plan (zai glm-4.7 / glm-5.3) normally
//	  → exhausted + OpenRouter key present → deepseek/deepseek-v4-flash-0731
//	  → exhausted + no OpenRouter key     → deepseek-v4-flash (direct)
//
// "Exhausted" is detected per-request from upstream Z.AI error responses
// (429 with codes 1305 "overloaded" / 1113 "insufficient balance") — the plan
// has no quota API we can poll cheaply.

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

// applyFallback computes the fallback model for a failed Z.AI plan request.
// It always returns an OpenRouter DeepSeek flash model when an OpenRouter key
// is configured, otherwise falls back to direct DeepSeek flash.
// This is used for quota-exhaustion fallback (429 errors), where flash is
// always appropriate because the upstream Z.AI plan has already rejected the
// request regardless of model size.
func (s *Server) applyFallback(requestID string) string {
	if s.settings != nil && s.settings.HasOpenRouterKey() {
		return openRouterFlash
	}
	return "deepseek-v4-flash"
}

// applyFallbackWithModel computes the fallback model for a Z.AI lane overflow.
// Unlike quota-exhaustion, lane overflow preserves model size: direct requests
// for big models (glm-5.3, deepseek-v4-pro, kimi-k3) are rerouted to OpenRouter
// pro, while downgraded or small-model requests go to flash.
func (s *Server) applyFallbackWithModel(originalModel, requestID string) string {
	if s.settings != nil && s.settings.HasOpenRouterKey() {
		return openRouterModelFor(originalModel)
	}
	// No OpenRouter key: always fall back to direct flash regardless of model size
	return "deepseek-v4-flash"
}
