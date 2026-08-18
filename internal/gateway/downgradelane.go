package gateway

import (
	"log/slog"
	"time"
)

// glm47LaneCap matches the Z.AI GLM Coding Plan concurrency limit for
// glm-4.7 (Pro tier: 2 in-flight requests). Downgraded requests take a lane
// slot first; overflow beyond this limit is delegated to another lane instead
// of queueing on (and being rejected by) glm-4.7.
const glm47LaneCap = 2

// hasFreeZaiKeyFunc allows tests to stub the free-tier key availability
// check. nil = use the real encrypted-settings lookup.
var hasFreeZaiKeyFunc = (*Server).hasZaiFreeKey

// selectDowngradeLane picks the target model for a router downgrade that would
// land on glm-4.7:
//
//	glm-4.7 while a lane slot is free (plan concurrency)
//	→ lane full + DeepSeek OFF-peak → deepseek-v4-flash (cheap overflow)
//	→ lane full + DeepSeek PEAK     → free-tier glm-4.7-flash (on-demand
//	  endpoint, free key — avoids DeepSeek 2x peak pricing)
//	→ lane full + PEAK + no free key → block on the lane slot (stay within
//	  the plan limit rather than eat guaranteed 429s)
//
// Returns the chosen model and a release func that frees the lane slot when
// the request completes (no-op for overflow lanes).
func (s *Server) selectDowngradeLane(requestID string, nowUTC func() time.Time) (string, func()) {
	if s.glm47Lane == nil {
		return "glm-4.7", func() {}
	}
	// Try to acquire a lane slot without blocking.
	select {
	case s.glm47Lane <- struct{}{}:
		s.glm47LaneInUse.Add(1)
		slog.Info("downgrade_lane: acquired glm-4.7 lane slot",
			"request_id", requestID,
			"in_use", s.glm47LaneInUse.Load(),
			"capacity", glm47LaneCap)
		return "glm-4.7", s.releaseGLM47LaneSlot
	default:
		// Lane full: route to overflow model.
		slog.Info("downgrade_lane: glm-4.7 lane full",
			"request_id", requestID,
			"in_use", s.glm47LaneInUse.Load(),
			"capacity", glm47LaneCap)
	}
	if deepseekPeakNow(nowUTC) {
		if hasFreeZaiKeyFunc(s) {
			slog.Info("downgrade_lane: overflow during deepseek peak → free glm-4.7-flash",
				"request_id", requestID)
			return freeFlashModel, func() {}
		}
		slog.Info("downgrade_lane: glm-4.7 lane full, no free key — waiting for slot",
			"request_id", requestID,
			"in_use", s.glm47LaneInUse.Load(),
			"capacity", glm47LaneCap)
		// Block until a slot frees — we'd rather queue within the plan limit
		// than eat guaranteed 429s from the free-tier endpoint.
		s.glm47Lane <- struct{}{}
		s.glm47LaneInUse.Add(1)
		return "glm-4.7", s.releaseGLM47LaneSlot
	}
	slog.Info("downgrade_lane: overflow off-peak → deepseek-v4-flash",
		"request_id", requestID)
	return "deepseek-v4-flash", func() {}
}

// releaseGLM47LaneSlot decrements the in-use counter and frees the semaphore slot.
func (s *Server) releaseGLM47LaneSlot() {
	if s.glm47Lane == nil {
		return
	}
	<-s.glm47Lane
	s.glm47LaneInUse.Add(-1)
}

// injectZaiLanguageDirective appends the English-only pin to the last
// system/developer message (prepending a system message when none exists).
// Mirrors verbosity.injectDirective but is provider-scoped rather than
// toggle-gated — language drift is a correctness bug, not a style preference.
func (s *Server) injectZaiLanguageDirective(body map[string]any) {
	directive := map[string]any{"role": "system", "content": languageDirective}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			m["content"] = c + "\n\n" + languageDirective
		case []any:
			m["content"] = append(c, map[string]any{"type": "text", "text": languageDirective})
		default:
			m["content"] = languageDirective
		}
		return
	}
	// No system message: prepend one so the pin is still instruction-weighted.
	body["messages"] = append([]any{directive}, msgs...)
}
