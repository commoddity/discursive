package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"discursive/internal/config"
)

func testConfig() SanitizeConfig {
	return SanitizeConfig{
		SanitizeTools:              true,
		InjectReasoningPlaceholder: true,
		MaxTokensDefault:           defaultMaxTokens,
		MaxTokensCap:               maxTokensCap,
		ThinkingDisabled:           true,
	}
}

func TestSanitizeRequest_K3Thinking(t *testing.T) {
	body := map[string]any{
		"model":       "gpt-5-high",
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"thinking":    map[string]any{"type": "enabled"},
		"temperature": 0.7,
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "kimi-k3" || res.Provider != config.ProviderMoonshot {
		t.Fatalf("route: %+v", res)
	}
	if res.Body["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort: %v", res.Body["reasoning_effort"])
	}
	if _, ok := res.Body["thinking"]; ok {
		t.Fatal("thinking should be stripped for K3")
	}
	if _, ok := res.Body["temperature"]; ok {
		t.Fatal("temperature should be stripped")
	}
}

func TestSanitizeRequest_K2Thinking(t *testing.T) {
	body := map[string]any{
		"model":            "gpt-5-codex",
		"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
		"reasoning_effort": "high",
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	thinking, ok := res.Body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking: %v", res.Body["thinking"])
	}
	if _, ok := res.Body["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort should be stripped for K2")
	}
}

func TestSanitizeRequest_DeepSeekThinking(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != config.ProviderDeepSeek {
		t.Fatalf("provider: %s", res.Provider)
	}
	thinking, ok := res.Body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking: %v", res.Body["thinking"])
	}
}

func TestSanitizeRequest_MaxTokens(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "maps max_completion_tokens",
			body: map[string]any{
				"model":                 "gpt-5-codex",	"max_completion_tokens": float64(500),
				"messages":              []any{},
				"tools":                 []any{probeTool()},
			},
			want: 500,
		},
		{
			name: "default when missing",
			body: map[string]any{
				"model":    "gpt-5-codex",
				"messages": []any{},
				"tools":    []any{probeTool()},
			},
			want: defaultMaxTokens,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := SanitizeRequest(tt.body, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			got, ok := jsonNumberInt(res.Body["max_tokens"])
			if !ok || got != tt.want {
				t.Fatalf("max_tokens: got %d want %d", got, tt.want)
			}
		})
	}
}

func TestSanitizeRequest_ReasoningPlaceholder(t *testing.T) {
	body := map[string]any{
		"model": "gpt-5-codex",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read",
							"arguments": "{}",
						},
					},
				},
			},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.Body["messages"].([]any)
	assistant := msgs[0].(map[string]any)
	if assistant["reasoning_content"] != reasoningPlaceholder {
		t.Fatalf("reasoning_content: %v", assistant["reasoning_content"])
	}
}

func TestSanitizeRequest_DeveloperToSystem(t *testing.T) {
	body := map[string]any{
		"model": "gpt-5-codex",
		"messages": []any{
			map[string]any{"role": "developer", "content": "rules"},
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.Body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("role: %v", msgs[0])
	}
}

func TestSanitizeRequest_FullPipelineResponses(t *testing.T) {
	body := map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "You are helpful.",
		"input": []any{
			map[string]any{"role": "user", "content": "Build a todo app"},
			map[string]any{"role": "assistant", "content": "Starting scaffold."},
			map[string]any{"role": "user", "content": "Continue the build"},
		},
		"tools": []any{
			map[string]any{
				"type":       "function",
				"name":       "read_file",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
			},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.Body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages len: %d", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Fatal("expected system from instructions")
	}
	if _, ok := res.Body["input"]; ok {
		t.Fatal("input should be removed")
	}
}

func TestSanitizeRequest_DeepSeekPipeline(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "deepseek-v4-flash" {
		t.Fatalf("model: %s", res.Model)
	}
}

func TestSanitizeRequest_StripsImages(t *testing.T) {
	tests := []struct {
		name  string
		body  map[string]any
		check func(t *testing.T, res SanitizeResult)
	}{
		{
			name: "chat image_url mixed with text",
			body: map[string]any{
				"model": "gpt-4o-mini",
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "hello"},
							map[string]any{
								"type":      "image_url",
								"image_url": map[string]any{"url": "data:image/png;base64,abc"},
							},
						},
					},
				},
			},
			check: func(t *testing.T, res SanitizeResult) {
				if res.Provider != config.ProviderDeepSeek || res.Model != "deepseek-v4-flash" {
					t.Fatalf("route: %s/%s", res.Provider, res.Model)
				}
				assertNoImageURL(t, res.Body["messages"])
				msgs := res.Body["messages"].([]any)
				// first message should be the system warning
				assertSystemWarning(t, msgs[0])
				content := msgs[1].(map[string]any)["content"]
				parts, ok := content.([]any)
				if !ok {
					t.Fatalf("expected content parts, got %T %v", content, content)
				}
				found := false
				for _, p := range parts {
					m, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if stringField(m, "text") == imageOmittedPlaceholder {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing placeholder in %v", content)
				}
			},
		},
		{
			name: "responses input_image",
			body: map[string]any{
				"model": "gpt-4o-mini",
				"input": []any{
					map[string]any{
						"type":      "input_image",
						"image_url": "https://example.com/shot.png",
					},
					map[string]any{"role": "user", "content": "what do you see?"},
				},
			},
			check: func(t *testing.T, res SanitizeResult) {
				assertNoImageURL(t, res.Body["messages"])
				msgs := res.Body["messages"].([]any)
				if len(msgs) < 2 {
					t.Fatal("expected at least 2 messages")
				}
				assertSystemWarning(t, msgs[0])
				first := msgs[1].(map[string]any)
				if first["content"] != imageOmittedPlaceholder {
					t.Fatalf("first content: %v", first["content"])
				}
			},
		},
		{
			name: "responses content parts image_url",
			body: map[string]any{
				"model": "gpt-4o",
				"input": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "input_text", "text": "describe"},
							map[string]any{
								"type":      "image_url",
								"image_url": map[string]any{"url": "https://example.com/a.png"},
							},
						},
					},
				},
			},
			check: func(t *testing.T, res SanitizeResult) {
				if res.Model != "deepseek-v4-pro" {
					t.Fatalf("model: %s", res.Model)
				}
				assertNoImageURL(t, res.Body["messages"])
				msgs := res.Body["messages"].([]any)
				assertSystemWarning(t, msgs[0])
				content := msgs[1].(map[string]any)["content"]
				blob, _ := json.Marshal(content)
				s := string(blob)
				if !strings.Contains(s, imageOmittedPlaceholder) {
					t.Fatalf("missing placeholder in %s", s)
				}
				if !strings.Contains(s, "describe") {
					t.Fatalf("missing text in %s", s)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := SanitizeRequest(tt.body, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, res)
		})
	}
}

func TestSanitizeRequest_PassesImagesForKimi(t *testing.T) {
	tests := []struct {
		name  string
		model string
		body  map[string]any
	}{
		{
			name:  "kimi k3 passes image_url through",
			model: "gpt-5-high",
			body: map[string]any{
				"model": "gpt-5-high",
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "describe this"},
							map[string]any{
								"type":      "image_url",
								"image_url": map[string]any{"url": "data:image/png;base64,abc"},
							},
						},
					},
				},
			},
		},
		{
			name:  "kimi k2.7-code passes image_url through",
			model: "gpt-5-codex",
			body: map[string]any{
				"model": "gpt-5-codex",
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{
								"type":      "image_url",
								"image_url": map[string]any{"url": "data:image/png;base64,abc"},
							},
						},
					},
				},
			},
		},
		{
			name:  "kimi k2.7-code responses input_image passes through",
			model: "gpt-5-codex",
			body: map[string]any{
				"model": "gpt-5-codex",
				"input": []any{
					map[string]any{
						"type":      "input_image",
						"image_url": "https://example.com/shot.png",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := SanitizeRequest(tt.body, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			if res.Provider != config.ProviderMoonshot {
				t.Fatalf("expected Moonshot provider, got %s", res.Provider)
			}
			assertNoImageOmittedPlaceholder(t, res.Body["messages"])
		})
	}
}

func TestSanitizeRequest_NoWarningForKimiWithImages(t *testing.T) {
	body := map[string]any{
		"model": "gpt-5-codex",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,abc"},
					},
				},
			},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.Body["messages"].([]any)
	// First message should NOT be a system warning
	if first, ok := msgs[0].(map[string]any); ok {
		if stringField(first, "role") == "system" {
			if strings.Contains(stringField(first, "content"), "does not support vision") {
				t.Fatal("Kimi should not get image-stripped warning")
			}
		}
	}
}

func assertSystemWarning(t *testing.T, msg any) {
	t.Helper()
	m, ok := msg.(map[string]any)
	if !ok {
		t.Fatalf("expected map message, got %T", msg)
	}
	if stringField(m, "role") != "system" {
		t.Fatal("expected system warning message")
	}
	if !strings.Contains(stringField(m, "content"), "does not support vision") {
		t.Fatalf("missing vision warning in system message: %v", m)
	}
}

func assertNoImageOmittedPlaceholder(t *testing.T, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		if stringField(x, "text") == imageOmittedPlaceholder {
			t.Fatalf("found imageOmittedPlaceholder in Kimi output: %v", x)
		}
		for _, child := range x {
			assertNoImageOmittedPlaceholder(t, child)
		}
	case []any:
		for _, child := range x {
			assertNoImageOmittedPlaceholder(t, child)
		}
	}
}

func assertNoImageURL(t *testing.T, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		if stringField(x, "type") == "image_url" {
			t.Fatalf("found image_url part: %v", x)
		}
		if _, ok := x["image_url"]; ok && stringField(x, "type") != "text" {
			t.Fatalf("found image_url field: %v", x)
		}
		for _, child := range x {
			assertNoImageURL(t, child)
		}
	case []any:
		for _, child := range x {
			assertNoImageURL(t, child)
		}
	}
}

func TestParseUsageObject(t *testing.T) {
	tests := []struct {
		name     string
		usage    map[string]any
		wantHit  uint64
		wantMiss uint64
		wantIn   uint64
		wantOut  uint64
	}{
		{
			name: "DeepSeek hit/miss",
			usage: map[string]any{
				"prompt_tokens":            1000,
				"completion_tokens":        200,
				"prompt_cache_hit_tokens":  800,
				"prompt_cache_miss_tokens": 200,
			},
			wantHit: 800, wantMiss: 200, wantIn: 1000, wantOut: 200,
		},
		{
			name: "DeepSeek alt field names",
			usage: map[string]any{
				"prompt_tokens":     1000,
				"completion_tokens": 200,
				"cache_hit_tokens":  800,
				"cache_miss_tokens": 200,
			},
			wantHit: 800, wantMiss: 200, wantIn: 1000, wantOut: 200,
		},
		{
			name: "Kimi K3 top-level cached_tokens",
			usage: map[string]any{
				"prompt_tokens":     260016,
				"completion_tokens": 16,
				"cached_tokens":     249856,
			},
			wantHit: 249856, wantMiss: 10160, wantIn: 260016, wantOut: 16,
		},
		{
			name: "Kimi K3 nested prompt_tokens_details.cached_tokens",
			usage: map[string]any{
				"prompt_tokens":     1113,
				"completion_tokens": 50,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 1024,
				},
			},
			wantHit: 1024, wantMiss: 89, wantIn: 1113, wantOut: 50,
		},
		{
			name: "Kimi full cache hit",
			usage: map[string]any{
				"prompt_tokens":     800000,
				"completion_tokens": 100,
				"cached_tokens":     800000,
			},
			wantHit: 800000, wantMiss: 0, wantIn: 800000, wantOut: 100,
		},
		{
			name: "no cache fields (classic OpenAI)",
			usage: map[string]any{
				"prompt_tokens":     500,
				"completion_tokens": 100,
			},
			wantHit: 0, wantMiss: 0, wantIn: 500, wantOut: 100,
		},
		{
			name: "Kimi cached > prompt (defensive clamp)",
			usage: map[string]any{
				"prompt_tokens":     1000,
				"completion_tokens": 100,
				"cached_tokens":     2000,
			},
			wantHit: 2000, wantMiss: 0, wantIn: 1000, wantOut: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUsageObject(tt.usage)
			if got.CacheHitTokens != tt.wantHit {
				t.Errorf("CacheHitTokens: got %d, want %d", got.CacheHitTokens, tt.wantHit)
			}
			if got.CacheMissTokens != tt.wantMiss {
				t.Errorf("CacheMissTokens: got %d, want %d", got.CacheMissTokens, tt.wantMiss)
			}
			if got.PromptTokens != tt.wantIn {
				t.Errorf("PromptTokens: got %d, want %d", got.PromptTokens, tt.wantIn)
			}
			if got.CompletionTokens != tt.wantOut {
				t.Errorf("CompletionTokens: got %d, want %d", got.CompletionTokens, tt.wantOut)
			}
		})
	}
}

func probeTool() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":       "probe",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}
