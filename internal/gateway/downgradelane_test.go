package gateway

import (
	"testing"
	"time"
)

// laneClock returns a fixed clock after the DeepSeek peak-pricing cutover.
func laneClock(hour int) func() time.Time {
	return func() time.Time {
		return time.Date(2026, time.August, 18, hour, 0, 0, 0, time.UTC)
	}
}

func TestSelectDowngradeLane(t *testing.T) {
	peak := laneClock(8)     // 08:00 UTC = DeepSeek peak window
	offpeak := laneClock(12) // 12:00 UTC = off-peak

	fillLane := func(s *Server, n int) {
		for i := 0; i < n; i++ {
			s.glm47Lane <- struct{}{}
		}
	}

	tests := []struct {
		name     string
		clock    func() time.Time
		inFlight int
		want     string
	}{
		{"lane free stays glm-4.7", offpeak, 0, "glm-4.7"},
		{"lane free stays glm-4.7 at peak", peak, 0, "glm-4.7"},
		{"full lane off-peak overflows to openrouter flash", offpeak, glm47LaneCap, openRouterFlash},
		{"full lane peak overflows to openrouter flash", peak, glm47LaneCap, openRouterFlash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{glm47Lane: make(chan struct{}, glm47LaneCap)}
			fillLane(s, tt.inFlight)
			got, release := s.selectDowngradeLane("req_test")
			defer release()
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("release frees the lane slot", func(t *testing.T) {
		s := &Server{glm47Lane: make(chan struct{}, glm47LaneCap)}
		_, release := s.selectDowngradeLane("req_test")
		if len(s.glm47Lane) != 1 {
			t.Fatalf("lane should hold 1 slot after acquire, holds %d", len(s.glm47Lane))
		}
		release()
		if len(s.glm47Lane) != 0 {
			t.Fatalf("lane should be empty after release, holds %d", len(s.glm47Lane))
		}
	})
}

func TestInjectZaiLanguageDirective(t *testing.T) {
	tests := []struct {
		name   string
		body   map[string]any
		verify func(t *testing.T, body map[string]any)
	}{
		{
			name: "appends to existing system message",
			body: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "You are a coding agent."},
				map[string]any{"role": "user", "content": "hi"},
			}},
			verify: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				sys := msgs[0].(map[string]any)
				c, _ := sys["content"].(string)
				if c != "You are a coding agent.\n\n"+languageDirective {
					t.Fatalf("system content = %q", c)
				}
				if len(msgs) != 2 {
					t.Fatalf("message count changed: %d", len(msgs))
				}
			},
		},
		{
			name: "prepends system message when none exists",
			body: map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "hi"},
			}},
			verify: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				if len(msgs) != 2 {
					t.Fatalf("want 2 messages, got %d", len(msgs))
				}
				sys := msgs[0].(map[string]any)
				if sys["role"] != "system" || sys["content"] != languageDirective {
					t.Fatalf("prepended message = %v", sys)
				}
			},
		},
		{
			name: "nil messages is a no-op",
			body: map[string]any{},
			verify: func(t *testing.T, body map[string]any) {
				if _, ok := body["messages"]; ok {
					t.Fatal("messages should remain unset")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			s.injectZaiLanguageDirective(tt.body)
			tt.verify(t, tt.body)
		})
	}
}
