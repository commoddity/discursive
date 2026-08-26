package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/usage"
)

// verboseFlashCompletion returns a flash completion with a code block followed
// by trailing verbose prose. Verbosity is prompt-only, so responses pass
// through unchanged; this content is used to assert passthrough.
func verboseFlashCompletion() map[string]any {
	return map[string]any{
		"id":      "chatcmpl-flash",
		"object":  "chat.completion",
		"model":   "deepseek-v4-flash",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "Here is the fix.\n\n```go\nx := 1\n```\n\nThis first sentence warns the reader. This second sentence restates the code. This third sentence adds a redundant remark. This fourth sentence is pure filler. This fifth sentence explains nothing new."}, "finish_reason": "stop"}},
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(30),
			"total_tokens":      float64(40),
		},
	}
}

func TestVerbosityEnabled_AppliesRequestSideControls_PassthroughResponse(t *testing.T) {
	var upstreamBody map[string]any
	upstream := func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstreamBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verboseFlashCompletion())
	}

	env := setupVerbosityEnv(t, upstream)

	// o3-mini alias resolves to deepseek-v4-flash.
	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":          "o3-mini",
		"messages":       []any{map[string]any{"role": "user", "content": "fix it"}},
		"max_tokens":     json.Number("20000"),
		"stream":         false,
		"stream_options": map[string]any{"include_usage": true},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}

	// 1. Request-side: directive injected into system message, tokens capped.
	directiveInjected := false
	for _, m := range upstreamBody["messages"].([]any) {
		mm := m.(map[string]any)
		if role, _ := mm["role"].(string); role == "user" {
			continue
		}
		if c, _ := mm["content"].(string); strings.Contains(c, "CRITICAL OUTPUT CONSTRAINT") {
			directiveInjected = true
		}
	}
	if !directiveInjected {
		t.Fatalf("verbosity directive not injected; body=%+v", upstreamBody)
	}
	if mt, ok := upstreamBody["max_tokens"].(float64); !ok || mt != 4096 {
		t.Fatalf("expected max_tokens capped to 4096, got %v", upstreamBody["max_tokens"])
	}

	// 2. Response-side: content passes through byte-for-byte — verbosity only
	//    coerces the model; it never edits response content.
	var completion map[string]any
	_ = json.Unmarshal(body, &completion)
	content := completion["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "```go") || !strings.Contains(content, "x := 1") {
		t.Fatalf("code block lost: %q", content)
	}
	// The verbose trailing prose must be preserved intact (prompt-coercion
	// only — no trimming, no ellipsis).
	if !strings.Contains(content, "fifth sentence") {
		t.Fatalf("verbose prose should pass through unchanged: %q", content)
	}
	if strings.Contains(content, "…") {
		t.Fatalf("response content must never be trimmed with an ellipsis: %q", content)
	}
}

// TestVerbosityEnabled_StreamingPassthrough verifies that a pure-text verbose
// stream is passed through byte-for-byte (verbosity never edits streams).
func TestVerbosityEnabled_StreamingPassthrough(t *testing.T) {
	upstream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		verb := "Here is the fix.\n\n```go\nx := 1\n```\n\nThis first sentence warns the reader. This second sentence restates the code. This third sentence adds a redundant remark. This fourth sentence is pure filler. This fifth sentence explains nothing new."
		_, _ = w.Write([]byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":""}}]}` + "\n\n"))
		chunk, _ := json.Marshal(map[string]any{"id": "1", "object": "chat.completion.chunk", "choices": []any{map[string]any{"delta": map[string]any{"content": verb}}}, "usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 20}})
		_, _ = w.Write([]byte("data: " + string(chunk) + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: " + `{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}

	env := setupVerbosityEnv(t, upstream)

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "o3-mini",
		"messages": []any{map[string]any{"role": "user", "content": "fix it"}},
		"stream":   true,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type %q", res.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "```go") || !strings.Contains(string(body), "x := 1") {
		t.Fatalf("code lost in stream: %s", body)
	}
	// Verbose prose must pass through unchanged (no trim).
	if !strings.Contains(string(body), "fifth sentence") {
		t.Fatalf("verbose prose not passed through in stream: %s", body)
	}
	// Usage must still be recorded.
	store, _ := usage.NewStore(env.dataRoot)
	events, _ := store.LoadEvents()
	if len(events) != 1 || events[0].PromptTokens != 5 {
		t.Fatalf("usage events: %+v", events)
	}
}

// TestVerbosityEnabled_StreamingToolCallsPassthrough verifies that a streaming
// response carrying tool calls is never trimmed (safety invariant).
func TestVerbosityEnabled_StreamingToolCallsPassthrough(t *testing.T) {
	upstream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/a/b\"}"}}]}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}

	env := setupVerbosityEnv(t, upstream)

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "o3-mini",
		"messages": []any{map[string]any{"role": "user", "content": "do it"}},
		"stream":   true,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "tool_calls") || !strings.Contains(string(body), "call_1") {
		t.Fatalf("tool-call stream lost/trimmed: %s", body)
	}
}

// TestVerbosityEnabled_DowngradedToFlashAppliesControls verifies that when a
// request targeting deepseek-v4-pro (o1 alias) is downgraded by the subagent
// router, the verbosity controls still apply to the FINALLY-served model.
// This confirms verbosity runs AFTER model override.
func TestVerbosityEnabled_DowngradedToFlashAppliesControls(t *testing.T) {
	// "What is..." (simple lookup) triggers a subagent-router downgrade from
	// pro → deepseek-v4-flash (same-provider small). No OpenRouter key here so
	// peak reroute cannot swap the model — keeps this test time-independent.
	var upstreamBody map[string]any
	var upstreamModel string
	upstream := func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstreamBody)
		upstreamModel, _ = upstreamBody["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verboseFlashCompletion())
	}

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	up := httptest.NewServer(http.HandlerFunc(upstream))
	t.Cleanup(up.Close)

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr:            "127.0.0.1:0",
		GatewayKey:            settings.GatewayKey,
		DataRoot:              dataRoot,
		Settings:              &settings,
		HTTPClient:            up.Client(),
		SubAgentRouterEnabled: true,
		ChatURLOverride: map[config.Provider]string{
			config.ProviderDeepSeek: up.URL + "/deepseek/chat/completions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	env := &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot}

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":      "o1",
		"messages":   []any{map[string]any{"role": "user", "content": "What is the difference between goroutines and threads?"}},
		"max_tokens": json.Number("20000"),
		"stream":     false,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}

	// Downgraded to same-provider small; no OR key → no peak reroute swap.
	if upstreamModel != "deepseek-v4-flash" {
		t.Fatalf("expected downgrade to deepseek-v4-flash, got %q", upstreamModel)
	}

	// DeepSeek flash has verbosity config — the directive must be injected and
	// the token cap applied to the final served model.
	found := false
	for _, m := range upstreamBody["messages"].([]any) {
		if c, _ := m.(map[string]any)["content"].(string); strings.Contains(c, "CRITICAL OUTPUT CONSTRAINT") {
			found = true
		}
	}
	if !found {
		t.Fatalf("verbosity directive must be applied to the flash downgrade target")
	}
	if mt, ok := upstreamBody["max_tokens"].(float64); ok && mt > 8192 {
		t.Fatalf("max_tokens must be capped for the flash downgrade target, got %v", upstreamBody["max_tokens"])
	}
}

// setupVerbosityEnv builds a gateway for verbosity integration tests.
func setupVerbosityEnv(t *testing.T, upstream http.HandlerFunc, enableRouter ...bool) *testEnv {
	t.Helper()
	routerOn := false
	if len(enableRouter) > 0 {
		routerOn = enableRouter[0]
	}
	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	// Peak guard OFF for deterministic DeepSeek-routing in these tests.
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr:            "127.0.0.1:0",
		GatewayKey:            settings.GatewayKey,
		DataRoot:              dataRoot,
		Settings:              &settings,
		HTTPClient:            up.Client(),
		SubAgentRouterEnabled: routerOn,
		ChatURLOverride: map[config.Provider]string{
			config.ProviderDeepSeek: up.URL + "/deepseek/chat/completions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	return &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot}
}
