package gateway

import (
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestOptimize_PromptCacheKeyMoonshot(t *testing.T) {
	body := map[string]any{
		"model":    "kimi-k3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	cfg := OptimizeConfig{PromptCacheKey: "sess_test123"}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderMoonshot, Model: "kimi-k3"}, cfg)
	k, ok := body["prompt_cache_key"].(string)
	if !ok || k != "sess_test123" {
		t.Fatalf("expected prompt_cache_key=sess_test123, got %v", body["prompt_cache_key"])
	}
}

func TestOptimize_PromptCacheKeyDeepSeekOmitted(t *testing.T) {
	body := map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	cfg := OptimizeConfig{PromptCacheKey: "sess_test123"}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash"}, cfg)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatal("prompt_cache_key should not be present for DeepSeek")
	}
}

func TestOptimize_NoPromptCacheKeyWhenEmpty(t *testing.T) {
	body := map[string]any{
		"model":    "kimi-k3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderMoonshot, Model: "kimi-k3"}, OptimizeConfig{})
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatal("prompt_cache_key should not be present when PromptCacheKey is empty")
	}
}

func TestOptimize_SortsToolsByName(t *testing.T) {
	body := map[string]any{
		"model":    "kimi-k3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "zzz_last",
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "aaa_first",
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "mmm_middle",
				},
			},
		},
	}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderMoonshot, Model: "kimi-k3"}, OptimizeConfig{})
	tools := body["tools"].([]any)
	names := make([]string, len(tools))
	for i, t := range tools {
		m := t.(map[string]any)
		fn, _ := mapField(m, "function")
		names[i] = stringField(fn, "name")
	}
	if names[0] != "aaa_first" || names[1] != "mmm_middle" || names[2] != "zzz_last" {
		t.Fatalf("tools not sorted: %v", names)
	}
}

func TestOptimize_SkipsNilTools(t *testing.T) {
	body := map[string]any{
		"model":    "kimi-k3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderMoonshot, Model: "kimi-k3"}, OptimizeConfig{})
	// Should not panic.
}

func TestOptimize_SkipsSingleTool(t *testing.T) {
	body := map[string]any{
		"model":    "kimi-k3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "only_tool",
				},
			},
		},
	}
	OptimizeRequest(SanitizeResult{Body: body, Provider: config.ProviderMoonshot, Model: "kimi-k3"}, OptimizeConfig{})
	tools := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
}
