//go:build live

package gateway_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"discursive/internal/config"
	"discursive/internal/gateway"
	"discursive/internal/usage"
)

// startLiveGateway creates a real gateway server (no ChatURLOverride) with keys
// from env vars, ready for live E2E testing.
func startLiveGateway(t *testing.T, port uint16) (*gateway.Server, string, string) {
	t.Helper()

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	settings.LocalPort = port

	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatalf("ensure gateway key: %v", err)
	}

	if k := os.Getenv("MOONSHOT_API_KEY"); k != "" {
		if err := settings.SetMoonshotKey(dataRoot, k); err != nil {
			t.Fatalf("set moonshot key: %v", err)
		}
	}
	if k := os.Getenv("DEEPSEEK_API_KEY"); k != "" {
		if err := settings.SetDeepSeekKey(dataRoot, k); err != nil {
			t.Fatalf("set deepseek key: %v", err)
		}
	}

	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	go func() {
		_ = srv.ListenAndServe(t.Context())
	}()

	return srv, dataRoot, settings.GatewayKey
}

// liveGatewayAddr returns the address of a live gateway server.
func liveGatewayAddr(port uint16) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// liveDoJSON sends a JSON request to the live gateway and returns the response + body bytes.
func liveDoJSON(t *testing.T, method, url, gatewayKey string, body any) (*http.Response, []byte) {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(raw))
	}

	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if gatewayKey != "" {
		req.Header.Set("Authorization", "Bearer "+gatewayKey)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, b
}

// liveGetBody extracts a JSON body, failing on error.
func liveGetJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("parse JSON: %v\nbody: %s", err, body)
	}
	return m
}

// verifyUsage checks that at least one usage event exists with the given provider and model,
// and that it has non-zero token counts.
func verifyUsage(t *testing.T, dataRoot string, provider config.Provider, model string) {
	t.Helper()

	store, err := usage.NewStore(dataRoot)
	if err != nil {
		t.Fatalf("new usage store: %v", err)
	}
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected at least 1 usage event, got 0")
	}

	found := false
	for _, ev := range events {
		if ev.Provider == provider && ev.Model == model {
			found = true
			if ev.PromptTokens == 0 {
				t.Errorf("usage event has zero PromptTokens: %+v", ev)
			}
			if ev.CompletionTokens == 0 && ev.PromptTokens > 0 {
				t.Errorf("usage event has zero CompletionTokens: %+v", ev)
			}
			break
		}
	}
	if !found {
		t.Errorf("no usage event for %s/%s: %+v", provider, model, events)
	}
}

// ---------------------------------------------------------------------------
// 1. Upstream key validation
// ---------------------------------------------------------------------------

func TestLive_UpstreamKeyValid(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	modelsURL := "https://api.moonshot.ai/v1/models"
	req, _ := http.NewRequest(http.MethodGet, modelsURL, nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("MOONSHOT_API_KEY"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("direct moonshot request: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("direct moonshot status %d: %s", res.StatusCode, body)
	}
}

func TestLive_UpstreamKeyValid_DeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	modelsURL := "https://api.deepseek.com/models"
	req, _ := http.NewRequest(http.MethodGet, modelsURL, nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("direct deepseek request: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("direct deepseek status %d: %s", res.StatusCode, body)
	}
}

// ---------------------------------------------------------------------------
// 2. Chat completion through gateway
// ---------------------------------------------------------------------------

func TestLive_ChatCompletion_AssistantMessage(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18420
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":       "gpt-5-high",
		"messages":    []any{map[string]any{"role": "user", "content": "Reply with exactly: KIMI_OK"}},
		"max_tokens":  50,
		"temperature": 0,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	jsonBody := liveGetJSON(t, body)
	content, ok := jsonBody["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !ok || content == "" {
		t.Fatalf("no content in response: %s", body)
	}
	if !strings.Contains(strings.ToUpper(content), "KIMI") {
		t.Fatalf("expected KIMI in content, got: %s", content)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

func TestLive_ChatCompletion_DeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	const port uint16 = 18421
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":       "gpt-4o-mini",
		"messages":    []any{map[string]any{"role": "user", "content": "Reply with exactly: DEEPSEEK_OK"}},
		"max_tokens":  50,
		"temperature": 0,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	jsonBody := liveGetJSON(t, body)
	content, ok := jsonBody["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !ok || content == "" {
		t.Fatalf("no content in response: %s", body)
	}
	if !strings.Contains(strings.ToUpper(content), "DEEPSEEK") {
		t.Fatalf("expected DEEPSEEK in content, got: %s", content)
	}

	verifyUsage(t, dataRoot, config.ProviderDeepSeek, "deepseek-v4-flash")
}

// ---------------------------------------------------------------------------
// 3. Tool schema sanitization
// ---------------------------------------------------------------------------

func TestLive_ToolSchemaSanitized(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18422
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":    "gpt-5-high",
		"messages": []any{map[string]any{"role": "user", "content": "What is 2+2? Reply with just the number."}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "calc",
				"strict": true,
				"parameters": map[string]any{
					"$schema":     "http://json-schema.org/draft-07/schema#",
					"definitions": map[string]any{"Num": map[string]any{"type": "number"}},
					"type":        "object",
					"properties":  map[string]any{"n": map[string]any{"$ref": "#/definitions/Num"}},
				},
			},
		}},
		"max_tokens":  80,
		"temperature": 0,
	})
	jsonBody := liveGetJSON(t, body)

	if errMsg, ok := jsonBody["error"].(map[string]any)["message"].(string); ok {
		if strings.Contains(errMsg, "#/definitions") || strings.Contains(errMsg, "json schema") {
			t.Fatalf("schema sanitizer failed: %s\nstatus: %d", errMsg, res.StatusCode)
		}
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

func TestLive_ToolSchemaSanitized_DeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	const port uint16 = 18423
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []any{map[string]any{"role": "user", "content": "What is 2+2? Reply with just the number."}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "calc",
				"strict": true,
				"parameters": map[string]any{
					"$schema":     "http://json-schema.org/draft-07/schema#",
					"definitions": map[string]any{"Num": map[string]any{"type": "number"}},
					"type":        "object",
					"properties":  map[string]any{"n": map[string]any{"$ref": "#/definitions/Num"}},
				},
			},
		}},
		"max_tokens":  80,
		"temperature": 0,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	verifyUsage(t, dataRoot, config.ProviderDeepSeek, "deepseek-v4-flash")
}

// ---------------------------------------------------------------------------
// 4. MFJS edge cases
// ---------------------------------------------------------------------------

func TestLive_MFJSEdgeCases(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18424
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	// Deep schema (12 levels) that must be flattened under the depth-10 cap.
	deep := map[string]any{"type": "string"}
	for i := 0; i < 12; i++ {
		deep = map[string]any{"type": "object", "properties": map[string]any{"child": deep}}
	}

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":       "gpt-5-high",
		"messages":    []any{map[string]any{"role": "user", "content": "Say hi."}},
		"max_tokens":  80,
		"temperature": 0,
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "set_mode",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"mode": map[string]any{"enum": []string{"start", "end"}}},
					},
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "pick_value",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"val": map[string]any{
								"type":   "string",
								"anyOf":  []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}},
							},
						},
					},
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "deep_tool",
					"parameters": deep,
				},
			},
		},
	})

	jsonBody := liveGetJSON(t, body)
	if errObj, ok := jsonBody["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok {
			if strings.Contains(msg, "moonshot flavored json schema") {
				t.Fatalf("MFJS normalization failed: %s", msg)
			}
		}
	}
	if res.StatusCode != 200 {
		t.Fatalf("moonshot rejected MFJS schemas: status %d body %s", res.StatusCode, body)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

// ---------------------------------------------------------------------------
// 5. MCP tool names
// ---------------------------------------------------------------------------

func TestLive_MCPToolNames(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18425
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	var tools []any
	mcpNames := []string{
		"mcp.filesystem.read_file",
		"mcp.terminal.run",
		"server/search",
		"apply_patch",
		"2invalid",
	}
	for i, name := range mcpNames {
		toolType := "function"
		if i == 3 {
			toolType = "custom"
		}
		tools = append(tools, map[string]any{
			"type":        toolType,
			"name":        name,
			"description": fmt.Sprintf("tool %d", i),
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":       "gpt-5-high",
		"messages":    []any{},
		"tools":       tools,
		"max_tokens":  80,
		"temperature": 0,
	})

	jsonBody := liveGetJSON(t, body)
	if errObj, ok := jsonBody["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok {
			if strings.Contains(msg, "tool") || strings.Contains(msg, "schema") {
				t.Fatalf("MCP tool name sanitization failed: %s", msg)
			}
		}
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

func TestLive_MCPToolNames_DeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	const port uint16 = 18426
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	var tools []any
	mcpNames := []string{
		"mcp.filesystem.read_file",
		"mcp.terminal.run",
		"server/search",
		"apply_patch",
		"2invalid",
	}
	for i, name := range mcpNames {
		toolType := "function"
		if i == 3 {
			toolType = "custom"
		}
		tools = append(tools, map[string]any{
			"type":        toolType,
			"name":        name,
			"description": fmt.Sprintf("tool %d", i),
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":       "gpt-4o-mini",
		"messages":    []any{},
		"tools":       tools,
		"max_tokens":  80,
		"temperature": 0,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	verifyUsage(t, dataRoot, config.ProviderDeepSeek, "deepseek-v4-flash")
}

// ---------------------------------------------------------------------------
// 6. Cursor Agent multiturn
// ---------------------------------------------------------------------------

func TestLive_CursorAgentMultiturn(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18427
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	// Turn 1 — Cursor-like first request with developer role
	turn1Body := map[string]any{
		"model":             "gpt-5-high",
		"temperature":       0,
		"presence_penalty":  0,
		"frequency_penalty": 0,
		"max_tokens":        80,
		"stream":            false,
		"messages": []any{
			map[string]any{"role": "developer", "content": "You are a coding agent. Use tools when asked."},
			map[string]any{"role": "user", "content": "Say hi and tell me what tools you have."},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "hello_tool",
				"strict": true,
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"message": map[string]any{"type": "string"}},
					"required":   []string{"message"},
				},
			},
		}},
		"tool_choice": "auto",
	}

	res1, body1 := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, turn1Body)
	if res1.StatusCode != 200 {
		t.Fatalf("turn1 status %d body %s", res1.StatusCode, body1)
	}

	json1 := liveGetJSON(t, body1)
	choice1 := json1["choices"].([]any)[0].(map[string]any)
	msg1 := choice1["message"].(map[string]any)

	// Extract tool_calls from turn 1 (if present) for turn 2
	toolCalls, hasToolCalls := msg1["tool_calls"].([]any)
	if hasToolCalls && len(toolCalls) > 0 {
		call0 := toolCalls[0].(map[string]any)
		callID := call0["id"].(string)

		// Turn 2 — Cursor replays history WITHOUT reasoning_content (null-content assistant + tool result)
		turn2Body := map[string]any{
			"model":       "gpt-5-high",
			"temperature": 0.2,
			"stream":      false,
			"messages": []any{
				map[string]any{"role": "developer", "content": "You are a coding agent. Use tools when asked."},
				map[string]any{"role": "user", "content": "Say hi and tell me what tools you have."},
				map[string]any{"role": "assistant", "content": nil, "tool_calls": toolCalls},
				map[string]any{"role": "tool", "tool_call_id": callID, "content": "{\\\"greeting\\\": \\\"hi\\\"}"},
			},
			"tools":      turn1Body["tools"],
			"max_tokens": 80,
		}

		res2, body2 := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, turn2Body)
		if res2.StatusCode != 200 {
			t.Fatalf("turn2 status %d body %s", res2.StatusCode, body2)
		}
		_ = body2
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

// ---------------------------------------------------------------------------
// 7. Streaming SSE
// ---------------------------------------------------------------------------

func TestLive_StreamingSSE(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18428
	srv, _, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	req, _ := http.NewRequest(http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5-high","stream":true,"messages":[{"role":"user","content":"Reply with exactly: STREAM_OK"}],"max_tokens":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+gatewayKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("stream status %d: %s", res.StatusCode, body)
	}

	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(raw)

	if !strings.Contains(bodyStr, "data:") {
		t.Fatalf("not SSE, no data: prefix: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("SSE missing [DONE]: %s", bodyStr)
	}
	if !strings.Contains(strings.ToUpper(bodyStr), "STREAM") {
		t.Fatalf("SSE content missing STREAM: %s", bodyStr)
	}
}

func TestLive_StreamingSSE_DeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	const port uint16 = 18429
	srv, _, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	req, _ := http.NewRequest(http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"Reply with exactly: DS_STREAM_OK"}],"max_tokens":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+gatewayKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deepseek stream request: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("deepseek stream status %d: %s", res.StatusCode, body)
	}

	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("deepseek content-type %q", ct)
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(raw)

	if !strings.Contains(bodyStr, "data:") {
		t.Fatalf("deepseek not SSE, no data: prefix: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("deepseek SSE missing [DONE]: %s", bodyStr)
	}
	if !strings.Contains(strings.ToUpper(bodyStr), "DS_STREAM") {
		t.Fatalf("deepseek SSE content missing DS_STREAM: %s", bodyStr)
	}
}

// ---------------------------------------------------------------------------
// 8. Responses format context preservation
// ---------------------------------------------------------------------------

func TestLive_ResponsesFormatContext(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18430
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/chat/completions", gatewayKey, map[string]any{
		"model":        "gpt-5-high",
		"stream":       false,
		"temperature":  0,
		"instructions": "You are a senior engineer building a CLI.",
		"input": []any{
			map[string]any{"role": "developer", "content": "Be direct. Follow instructions exactly."},
			map[string]any{"role": "user", "content": "We are building a todo CLI. Phase 1: use clap + serde. Reply with exactly: PHASE1_OK"},
			map[string]any{"role": "assistant", "content": "PHASE1_OK — scaffolding with clap and serde."},
			map[string]any{"role": "user", "content": "Continue the build. Reply with exactly: PHASE2_CONTINUE"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "read_file",
				"strict": true,
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
			},
		}},
		"tool_choice": "auto",
		"max_tokens":  200,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	jsonBody := liveGetJSON(t, body)
	content, ok := jsonBody["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !ok || content == "" {
		t.Fatalf("no content in responses-format reply: %s", body)
	}
	if !strings.Contains(strings.ToUpper(content), "PHASE2") && !strings.Contains(strings.ToUpper(content), "CONTINUE") {
		t.Fatalf("context not preserved, got: %s", content)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}

// ---------------------------------------------------------------------------
// 9. /v1/responses endpoint
// ---------------------------------------------------------------------------

func TestLive_V1ResponsesEndpoint(t *testing.T) {
	if os.Getenv("MOONSHOT_API_KEY") == "" {
		t.Skip("MOONSHOT_API_KEY not set")
	}

	const port uint16 = 18431
	srv, dataRoot, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	res, body := liveDoJSON(t, http.MethodPost, liveGatewayAddr(port)+"/v1/responses", gatewayKey, map[string]any{
		"model":      "gpt-5-high",
		"stream":     false,
		"input":      []any{map[string]any{"role": "user", "content": "Reply with exactly: RESPONSES_ENDPOINT_OK"}},
		"max_tokens": 50,
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	jsonBody := liveGetJSON(t, body)
	content, ok := jsonBody["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !ok || content == "" {
		t.Fatalf("no content in /v1/responses reply: %s", body)
	}
	if !strings.Contains(strings.ToUpper(content), "RESPONSES") {
		t.Fatalf("/v1/responses content not correct, got: %s", content)
	}

	verifyUsage(t, dataRoot, config.ProviderMoonshot, "kimi-k3")
}
