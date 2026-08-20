package gateway

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// TestAcquireZaiLaneOrOverflow covers the direct-request lane: free slot keeps
// the model, full lane + grace-wait expiry overflows to the fallback lane
// (OpenRouter flash when an OpenRouter key is configured, otherwise direct
// DeepSeek flash), and the release func is a no-op when no slot was taken.
func TestAcquireZaiLaneOrOverflow(t *testing.T) {
	peak := laneClock(8)     // 08:00 UTC = DeepSeek peak window
	offpeak := laneClock(12) // 12:00 UTC = off-peak

	fillSem := func(s *Server, n int) {
		for i := 0; i < n; i++ {
			s.zaiSem <- struct{}{}
		}
	}

	newServerWithKey := func() *Server {
		s := &Server{settings: &config.AppSettings{}}
		_ = s.settings.SetOpenRouterKey(t.TempDir(), "sk-or")
		return s
	}

	tests := []struct {
		name     string
		s        *Server
		clock    func() time.Time
		inFlight int
		want     string
	}{
		{"free slot keeps glm-4.7 off-peak", &Server{}, offpeak, 0, "glm-4.7"},
		{"free slot keeps glm-4.7 at peak", &Server{}, peak, 0, "glm-4.7"},
		{"full sem off-peak overflows to direct flash (no OR key)", &Server{}, offpeak, glm47LaneCap, "deepseek-v4-flash"},
		{"full sem peak overflows to openrouter flash (OR key)", newServerWithKey(), peak, glm47LaneCap, openRouterFlash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.zaiSem = make(chan struct{}, glm47LaneCap)
			fillSem(tt.s, tt.inFlight)
			got, release, ok := tt.s.acquireZaiLaneOrOverflow("glm-4.7", "req_test", tt.clock)
			if !ok {
				t.Fatal("expected ok=true")
			}
			defer release()
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("acquired slot release frees semaphore", func(t *testing.T) {
		s := &Server{zaiSem: make(chan struct{}, glm47LaneCap)}
		_, release, ok := s.acquireZaiLaneOrOverflow("glm-4.7", "req_test", offpeak)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if len(s.zaiSem) != 1 {
			t.Fatalf("sem should hold 1 slot after acquire, holds %d", len(s.zaiSem))
		}
		release()
		if len(s.zaiSem) != 0 {
			t.Fatalf("sem should be empty after release, holds %d", len(s.zaiSem))
		}
	})

	t.Run("overflow release is a no-op and does not drain the sem", func(t *testing.T) {
		s := &Server{zaiSem: make(chan struct{}, glm47LaneCap)}
		fillSem(s, glm47LaneCap)
		_, release, ok := s.acquireZaiLaneOrOverflow("glm-4.7", "req_test", offpeak)
		if !ok {
			t.Fatal("expected ok=true")
		}
		release()
		if len(s.zaiSem) != glm47LaneCap {
			t.Fatalf("overflow release must not drain the sem, holds %d", len(s.zaiSem))
		}
	})

	t.Run("slot freed during grace wait keeps model", func(t *testing.T) {
		s := &Server{zaiSem: make(chan struct{}, glm47LaneCap)}
		fillSem(s, glm47LaneCap)
		// Free a slot after 100ms (within the grace wait).
		go func() {
			time.Sleep(100 * time.Millisecond)
			<-s.zaiSem
		}()
		got, release, ok := s.acquireZaiLaneOrOverflow("glm-4.7", "req_test", offpeak)
		if !ok {
			t.Fatal("expected ok=true")
		}
		defer release()
		if got != "glm-4.7" {
			t.Fatalf("grace-wait acquire should keep glm-4.7, got %q", got)
		}
	})
}
