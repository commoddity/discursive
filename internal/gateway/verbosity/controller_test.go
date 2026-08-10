package verbosity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestController_ApplyRequest_InjectsDirective(t *testing.T) {
	tests := []struct {
		name      string
		messages  []any
		wantCount int // expected number of system/developer messages with directive
	}{
		{
			name: "appends to last system message",
			messages: []any{
				map[string]any{"role": "system", "content": "You are a coding agent."},
				map[string]any{"role": "user", "content": "hello"},
			},
			wantCount: 1,
		},
		{
			name: "no system message prepends new one",
			messages: []any{
				map[string]any{"role": "user", "content": "hello"},
			},
			wantCount: 1,
		},
		{
			name:      "nil messages stays nil",
			messages:  nil,
			wantCount: 0,
		},
		{
			name: "directive appended to developer message kept in place",
			messages: []any{
				map[string]any{"role": "developer", "content": "base"},
				map[string]any{"role": "user", "content": "hi"},
			},
			wantCount: 1,
		},
	}

	const directive = "CRITICAL OUTPUT CONSTRAINT — ALWAYS FOLLOW"
	c := NewController(VerbosityConfig{
		Models: map[string]ModelConfig{
			"deepseek-v4-flash": {SystemMessageDirective: directive, MaxTokens: 4096, TrimEnabled: true},
		},
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{"messages": tt.messages, "model": "deepseek-v4-flash"}
			c.ApplyRequest(body, "deepseek-v4-flash")

			msgs := body["messages"].([]any)
			count := 0
			for _, m := range msgs {
				mm := m.(map[string]any)
				role, _ := mm["role"].(string)
				if role != "system" && role != "developer" {
					continue
				}
				content, _ := mm["content"].(string)
				if strings.Contains(content, directive) {
					count++
				}
			}
			if count != tt.wantCount {
				t.Fatalf("expected %d messages with directive, got %d", tt.wantCount, count)
			}
		})
	}
}

func TestController_ApplyRequest_CapsTokens(t *testing.T) {
	tests := []struct {
		name      string
		body      map[string]any
		cfg       ModelConfig
		wantMax   any    // nil means expect no max_tokens change
		expectKey string // "max_tokens" or ""
	}{
		{
			name:      "caps high max_tokens",
			body:      map[string]any{"max_tokens": json.Number("32768")},
			cfg:       ModelConfig{MaxTokens: 4096},
			wantMax:   4096,
			expectKey: "max_tokens",
		},
		{
			name:      "preserves smaller max_tokens",
			body:      map[string]any{"max_tokens": json.Number("200")},
			cfg:       ModelConfig{MaxTokens: 4096},
			wantMax:   json.Number("200"),
			expectKey: "max_tokens",
		},
		{
			name:      "maps max_completion_tokens then caps",
			body:      map[string]any{"max_completion_tokens": json.Number("8000")},
			cfg:       ModelConfig{MaxTokens: 4096},
			wantMax:   4096,
			expectKey: "max_tokens",
		},
		{
			name:      "zero cap no-op",
			body:      map[string]any{"max_tokens": json.Number("1000")},
			cfg:       ModelConfig{MaxTokens: 0},
			wantMax:   json.Number("1000"),
			expectKey: "max_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{cfg: VerbosityConfig{Models: map[string]ModelConfig{"m": tt.cfg}}}
			body := tt.body
			controller.ApplyRequest(body, "m")

			switch v := body["max_tokens"].(type) {
			case nil:
				if tt.expectKey != "" {
					t.Fatalf("expected max_tokens set, got nil")
				}
			case json.Number:
				got, _ := v.Int64()
				iv, _ := tt.wantMax.(json.Number)
				want, _ := iv.Int64()
				if got != want {
					t.Fatalf("expected max_tokens=%v, got %v", want, got)
				}
			case int:
				want, _ := tt.wantMax.(int)
				if v != want {
					t.Fatalf("expected max_tokens=%d, got %d", want, v)
				}
			default:
				t.Fatalf("unexpected type %T for max_tokens", v)
			}
		})
	}
}

func TestController_ApplyRequest_UnknownModelNoOp(t *testing.T) {
	c := NewController(VerbosityConfig{
		Models: map[string]ModelConfig{"deepseek-v4-flash": {MaxTokens: 4096}},
	})
	body := map[string]any{"messages": []any{}, "model": "glm-5.2", "max_tokens": json.Number("5000")}
	c.ApplyRequest(body, "glm-5.2")
	if v, _ := body["max_tokens"].(json.Number); v.String() != "5000" {
		t.Fatalf("expected no mutation for unknown model, got %v", body["max_tokens"])
	}
}

func TestController_TrimResponse(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		trim         bool
		changed      bool
		unconfigured bool   // pass an unconfigured model
		wantSfx      string // expected trim suffix if changed
		contains     string // expected substring that must be preserved (e.g. code)
	}{
		{
			name:    "ends with code fence - no trim",
			content: "Here is the fix.\n\n```go\nx := 1\n```",
			trim:    true,
			changed: false,
		},
		{
			name:     "trailing verbose prose trimmed after code",
			content:  "Done.\n\n```go\nx := 1\n```\n\nThis fixes the bug by assigning one to x. It also ensures the value is set correctly and prevents the incorrect behavior seen previously. Finally it cleans up the formatting.",
			trim:     true,
			changed:  true,
			wantSfx:  trimSuffix,
			contains: "```go",
		},
		{
			name:    "no trim when trim disabled",
			content: "One. Two. Three. Four. Five.",
			trim:    false,
			changed: false,
		},
		{
			name:    "pure prose under threshold - no trim",
			content: "One. Two.",
			trim:    true,
			changed: false,
		},
		{
			name:         "unknown model - no trim",
			content:      "One. Two. Three. Four.",
			trim:         true,
			changed:      false,
			unconfigured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := VerbosityConfig{}
			if tt.trim && !tt.unconfigured {
				cfg.Models = map[string]ModelConfig{"deepseek-v4-flash": {TrimEnabled: true}}
			} else if !tt.unconfigured {
				cfg.Models = map[string]ModelConfig{"deepseek-v4-flash": {TrimEnabled: false}}
			}
			c := NewController(cfg)

			model := "deepseek-v4-flash"
			if tt.unconfigured {
				model = "glm-5.2"
			}

			resp := map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{"content": tt.content},
					},
				},
			}
			body, _ := json.Marshal(resp)
			out, changed := c.TrimResponse(body, model)

			if changed != tt.changed {
				t.Fatalf("expected changed=%v, got %v", tt.changed, changed)
			}
			if tt.contains != "" {
				var parsed map[string]any
				_ = json.Unmarshal(out, &parsed)
				content := parsed["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
				if !strings.Contains(content, tt.contains) {
					t.Fatalf("expected code to be preserved, content=%q", content)
				}
			}
		})
	}
}

func TestTrimProse_DoesNotCutCode(t *testing.T) {
	content := "Here is the change.\n\n```python\nif x:\n    return 1\n```\n\nThat's it. Hope that helps. Let me know if you need anything else."
	out := trimProse(content)
	if !strings.Contains(out, "```python") {
		t.Fatalf("code block lost: %q", out)
	}
	if !strings.Contains(out, "return 1") {
		t.Fatalf("code content lost: %q", out)
	}
	if !strings.HasSuffix(out, trimSuffix) {
		t.Fatalf("expected trim suffix, got %q", out)
	}
}

func TestTrimProse_NoSubstantiveBoundary_TrimsToFloor(t *testing.T) {
	content := "One. Two. Three. Four. Five. Six."
	out := trimProse(content)
	if !strings.HasSuffix(out, trimSuffix) {
		t.Fatalf("expected trim suffix for pure prose, got %q", out)
	}
	// After trimming, at most trimSentenceFloor sentences remain (plus suffix).
	semicolons := strings.Count(out, ".")
	if semicolons > trimSentenceFloorPlusSuffix() {
		t.Fatalf("too many sentences remained: %q", out)
	}
}

func trimSentenceFloorPlusSuffix() int {
	return trimSentenceFloor
}

func TestTrimProse_ToolCallMessage_NOTrimmed(t *testing.T) {
	content := `<tool_calls>
<invoke name="read_file">
<parameter name="path">/a/b/c.go</parameter>
</invoke>
</tool_calls>`
	out := trimProse(content)
	if out != content {
		t.Fatalf("tool-call content must be untrimmed, got %q", out)
	}
}
