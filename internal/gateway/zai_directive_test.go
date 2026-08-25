package gateway

import "testing"

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
