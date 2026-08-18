package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

// Quota-exhaustion fallback:
//
//	plan (zai glm-4.7 / glm-5.3) normally
//	  → exhausted + DeepSeek PEAK hours    → free-tier glm-4.7-flash (on-demand endpoint, free key)
//	  → exhausted + DeepSeek OFF-peak hours → deepseek-v4-flash
//
// "Peak" is DeepSeek's pricing window (peak hours bill deepseek-v4-flash at
// 2x), NOT a Z.AI concept — Z.AI's coding plan has flat credit multipliers.
// "Exhausted" is detected per-request from upstream Z.AI error responses
// (429 with codes 1305 "overloaded" / 1113 "insufficient balance") — the plan
// has no quota API we can poll cheaply.

const freeFlashModel = "glm-4.7-flash"

var errNoZaiFreeKey = errors.New("zai free-tier key not configured")

// deepseekPeakNow reports whether the current instant is inside DeepSeek's
// peak pricing window (when deepseek-v4-flash bills at 2x, prefer the free
// Z.AI flash lane instead). nowUTC is injectable for tests; nil = time.Now.
func deepseekPeakNow(nowUTC func() time.Time) bool {
	clock := nowUTC
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	t := clock().UTC()
	if t.Before(usage.DeepSeekPeakCutover) {
		return false
	}
	return usage.DeepSeekPeakHours(t.Hour())
}

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

// applyFallback computes the fallback route for a failed Z.AI plan request.
// It returns the fallback model and whether the free-tier Z.AI endpoint/key
// must be used. ok=false means no fallback could be computed.
func (s *Server) applyFallback(requestID string, nowUTC func() time.Time) (model string, useFreeZai bool, ok bool) {
	if deepseekPeakNow(nowUTC) {
		if s.hasZaiFreeKey() {
			slog.Warn("fallback: zai quota exhausted during deepseek peak → free glm-4.7-flash",
				"request_id", requestID)
			return freeFlashModel, true, true
		}
		slog.Warn("fallback: no zai free key configured; using deepseek-v4-flash",
			"request_id", requestID)
		return "deepseek-v4-flash", false, true
	}
	slog.Warn("fallback: zai quota exhausted during deepseek off-peak → deepseek-v4-flash",
		"request_id", requestID)
	return "deepseek-v4-flash", false, true
}

// hasZaiFreeKey reports whether the free-tier Z.AI key is configured.
func (s *Server) hasZaiFreeKey() bool {
	_, err := s.zaiFreeUpstreamKey()
	return err == nil
}

// zaiFreeUpstreamKey returns the decrypted free-tier key.
func (s *Server) zaiFreeUpstreamKey() (string, error) {
	if s.live != nil {
		k, err := s.live.GetZaiFreeKey()
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", errNoZaiFreeKey
		}
		return *k, nil
	}
	if s.settings != nil {
		k, err := s.settings.GetZaiFreeKey(s.cfg.DataRoot)
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", errNoZaiFreeKey
		}
		return *k, nil
	}
	return "", errNoZaiFreeKey
}

// zaiOnDemandChatURL is the free/on-demand endpoint (distinct from the coding
// plan endpoint the plan key uses).
func zaiOnDemandChatURL() string {
	return config.ZaiOnDemandChatURL()
}
