package gateway

import (
	"log/slog"
	"sync"
)

// zaiStickyFreeStreak is how many consecutive requests must observe a free
// Z.AI lane before a sticky fallback session returns to the direct model.
const zaiStickyFreeStreak = 3

// stickyEntry tracks one model's sticky-fallback state after a lane overflow.
type stickyEntry struct {
	freeStreak int // consecutive lane probes that found a free slot
}

// stickyFallbacks maps original Z.AI model → sticky state. Once a model
// overflows the lane mid-session, it stays on the fallback model until the
// lane has been free for zaiStickyFreeStreak consecutive requests. This keeps
// prompt-cache locality (no cold-cold double pay between two upstreams) and
// model quality consistent within a conversation turn sequence.
type stickyFallbacks struct {
	mu      sync.Mutex
	entries map[string]*stickyEntry
}

func newStickyFallbacks() *stickyFallbacks {
	return &stickyFallbacks{entries: make(map[string]*stickyEntry)}
}

// sticky returns true when the model is currently pinned to its fallback.
func (sf *stickyFallbacks) sticky(model string) bool {
	if sf == nil {
		return false
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	_, ok := sf.entries[model]
	return ok
}

// markOverflowed pins the model to its fallback after a lane overflow.
func (sf *stickyFallbacks) markOverflowed(model string) {
	if sf == nil {
		return
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	sf.entries[model] = &stickyEntry{}
}

// probeFree records a lane-free observation while sticky. Returns true when
// the streak threshold is met and stickiness is lifted.
func (sf *stickyFallbacks) probeFree(model string) bool {
	if sf == nil {
		return true
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	e, ok := sf.entries[model]
	if !ok {
		return true
	}
	e.freeStreak++
	if e.freeStreak >= zaiStickyFreeStreak {
		delete(sf.entries, model)
		return true
	}
	return false
}

// probeBusy resets the streak while sticky (lane occupied again).
func (sf *stickyFallbacks) probeBusy(model string) {
	if sf == nil {
		return
	}
	if e, ok := sf.entries[model]; ok {
		e.freeStreak = 0
	}
}

// stickyLaneProbe is called while a model is sticky: try a non-blocking lane
// acquire. A success releases the slot immediately and counts toward the
// streak; a failure resets it. Returns true when stickiness is lifted.
func (s *Server) stickyLaneProbe(model, requestID string) bool {
	select {
	case s.zaiSem <- struct{}{}:
		<-s.zaiSem
		if s.stickyFallbacks.probeFree(model) {
			slog.Info("zai_lane: sticky fallback lifted, returning to direct model",
				"request_id", requestID, "model", model)
			return true
		}
		return false
	default:
		s.stickyFallbacks.probeBusy(model)
		return false
	}
}
