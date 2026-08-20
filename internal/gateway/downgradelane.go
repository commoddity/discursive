package gateway

import (
	"log/slog"
)

// glm47LaneCap matches the Z.AI GLM Coding Plan concurrency limit for
// glm-4.7 (Pro tier: 2 in-flight requests). Downgraded requests take a lane
// slot first; overflow beyond this limit is delegated to OpenRouter flash
// instead of queueing on (and being rejected by) glm-4.7.
const glm47LaneCap = 2

// selectDowngradeLane picks the target model for a router downgrade that would
// land on glm-4.7:
//
//	glm-4.7 while a lane slot is free (plan concurrency)
//	→ lane full → OpenRouter deepseek-v4-flash-0731 (cheap overflow)
//
// Returns the chosen model and a release func that frees the lane slot when
// the request completes (no-op for overflow lanes).
func (s *Server) selectDowngradeLane(requestID string) (string, func()) {
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
		// Lane full: route to OpenRouter flash overflow.
		slog.Info("downgrade_lane: glm-4.7 lane full",
			"request_id", requestID,
			"in_use", s.glm47LaneInUse.Load(),
			"capacity", glm47LaneCap)
	}
	slog.Info("downgrade_lane: overflow → openrouter deepseek-v4-flash-0731",
		"request_id", requestID)
	return openRouterFlash, func() {}
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
