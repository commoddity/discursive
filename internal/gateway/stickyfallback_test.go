package gateway

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func newStickyServerWithKey(t *testing.T) *Server {
	t.Helper()
	s := &Server{settings: &config.AppSettings{}}
	_ = s.settings.SetOpenRouterKey(t.TempDir(), "sk-or")
	s.stickyFallbacks = newStickyFallbacks()
	return s
}

// TestStickyFallback pins the model to the fallback after an overflow and
// keeps it there while the lane stays busy, then returns to the direct model
// only after the free-streak threshold is met.
func TestStickyFallback(t *testing.T) {
	s := newStickyServerWithKey(t)
	s.zaiSem = make(chan struct{}, glm47LaneCap)
	clock := func() time.Time { return time.Now() }

	fill := func() { s.zaiSem <- struct{}{} }
	drain := func() { <-s.zaiSem }

	// Lane full → overflow marks sticky.
	fill()
	fill()
	got, _, ok := s.acquireZaiLaneOrOverflow("glm-5.3", "req_1", clock)
	if !ok || got != openRouterPro {
		t.Fatalf("overflow: got %q ok=%v, want %q", got, ok, openRouterPro)
	}
	if !s.stickyFallbacks.sticky("glm-5.3") {
		t.Fatal("model should be sticky after overflow")
	}

	// Lane still full → sticky shortcut stays on fallback, no grace wait.
	start := time.Now()
	got, _, _ = s.acquireZaiLaneOrOverflow("glm-5.3", "req_2", clock)
	if got != openRouterPro {
		t.Fatalf("sticky busy: got %q, want %q", got, openRouterPro)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("sticky busy path should not grace-wait, took %v", elapsed)
	}

	// Lane free: streak counts up but stays sticky below threshold.
	drain()
	for i := 1; i < zaiStickyFreeStreak; i++ {
		got, _, _ = s.acquireZaiLaneOrOverflow("glm-5.3", "req_streak", clock)
		if got != openRouterPro {
			t.Fatalf("streak %d: still sticky, want fallback, got %q", i, got)
		}
		if !s.stickyFallbacks.sticky("glm-5.3") {
			t.Fatalf("streak %d: should still be sticky", i)
		}
	}
	// Final probe lifts stickiness and takes the direct slot.
	got, release, _ := s.acquireZaiLaneOrOverflow("glm-5.3", "req_lift", clock)
	if got != "glm-5.3" {
		t.Fatalf("after streak: got %q, want glm-5.3", got)
	}
	release()
	if s.stickyFallbacks.sticky("glm-5.3") {
		t.Fatal("stickiness should be lifted after streak")
	}
}

// TestStickyFallbackStreakReset verifies a busy probe resets the streak.
func TestStickyFallbackStreakReset(t *testing.T) {
	s := newStickyServerWithKey(t)
	s.zaiSem = make(chan struct{}, glm47LaneCap)
	clock := func() time.Time { return time.Now() }

	s.stickyFallbacks.markOverflowed("glm-4.7")

	// Two free probes (streak = 2).
	for i := 0; i < zaiStickyFreeStreak-1; i++ {
		got, _, _ := s.acquireZaiLaneOrOverflow("glm-4.7", "req", clock)
		if got != openRouterFlash {
			t.Fatalf("probe %d: got %q, want %q", i, got, openRouterFlash)
		}
	}
	// One busy probe resets the streak to 0.
	s.zaiSem <- struct{}{}
	s.zaiSem <- struct{}{}
	got, _, _ := s.acquireZaiLaneOrOverflow("glm-4.7", "req_busy", clock)
	if got != openRouterFlash {
		t.Fatalf("busy probe: got %q, want %q", got, openRouterFlash)
	}
	<-s.zaiSem
	<-s.zaiSem
	if !s.stickyFallbacks.sticky("glm-4.7") {
		t.Fatal("should still be sticky after reset")
	}
	// A single free probe must NOT lift (streak was reset).
	got, _, _ = s.acquireZaiLaneOrOverflow("glm-4.7", "req_free", clock)
	if got != openRouterFlash {
		t.Fatalf("after reset: got %q, want sticky fallback %q", got, openRouterFlash)
	}
}

// TestStickyNilSafe verifies zero-value Servers (nil stickyFallbacks) still
// take direct slots and overflow normally.
func TestStickyNilSafe(t *testing.T) {
	s := &Server{} // no stickyFallbacks, no settings
	s.zaiSem = make(chan struct{}, glm47LaneCap)
	clock := func() time.Time { return time.Now() }

	got, release, ok := s.acquireZaiLaneOrOverflow("glm-4.7", "req", clock)
	if !ok || got != "glm-4.7" {
		t.Fatalf("nil sticky: got %q ok=%v, want glm-4.7", got, ok)
	}
	release()

	s.zaiSem <- struct{}{}
	s.zaiSem <- struct{}{}
	got, _, _ = s.acquireZaiLaneOrOverflow("glm-4.7", "req", clock)
	if got != "deepseek-v4-flash" {
		t.Fatalf("nil sticky overflow: got %q, want deepseek-v4-flash", got)
	}
}
