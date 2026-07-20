package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/usage"
)

type testEnv struct {
	srv        *gateway.Server
	ts         *httptest.Server
	gatewayKey string
	dataRoot   string
	upCalls    *atomic.Int32
}

func setupEnv(t *testing.T, moonshotKey, deepseekKey string, upstream http.HandlerFunc) *testEnv {
	t.Helper()
	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if moonshotKey != "" {
		if err := settings.SetMoonshotKey(dataRoot, moonshotKey); err != nil {
			t.Fatal(err)
		}
	}
	if deepseekKey != "" {
		if err := settings.SetDeepSeekKey(dataRoot, deepseekKey); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	var upCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upCalls.Add(1)
		upstream(w, r)
	}))
	t.Cleanup(up.Close)

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: "127.0.0.1:0",
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
		HTTPClient: up.Client(),
		ChatURLOverride: map[config.Provider]string{
			config.ProviderMoonshot: up.URL + "/moonshot/chat/completions",
			config.ProviderDeepSeek: up.URL + "/deepseek/chat/completions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	return &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot, upCalls: &upCalls}
}

func (e *testEnv) doJSON(t *testing.T, method, path string, auth bool, body any) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+e.gatewayKey)
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

func mockCompletion(model string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
			"total_tokens":      float64(15),
		},
	}
}

func TestAuthAndHealth(t *testing.T) {
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called")
	})

	res, body := env.doJSON(t, http.MethodGet, "/health", false, nil)
	if res.StatusCode != 200 {
		t.Fatalf("health status %d", res.StatusCode)
	}
	if strings.Contains(string(body), "sk-moon") || strings.Contains(string(body), env.gatewayKey) {
		t.Fatalf("secrets in health: %s", body)
	}
	var health map[string]any
	_ = json.Unmarshal(body, &health)
	if health["ok"] != true {
		t.Fatalf("health: %v", health)
	}
	if _, ok := health["has_moonshot_key"]; ok {
		t.Fatal("unexpected health fields")
	}

	tests := []struct {
		name string
		auth bool
		hdr  string
		key  string
		want int
	}{
		{name: "missing", want: 401},
		{name: "wrong", auth: true, key: "wrong", want: 401},
		{name: "bearer ok", auth: true, want: 200},
		{name: "x-api-key", hdr: "x-api-key", want: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, env.ts.URL+"/v1/models", nil)
			switch {
			case tt.hdr != "":
				req.Header.Set(tt.hdr, env.gatewayKey)
			case tt.auth && tt.key != "":
				req.Header.Set("Authorization", "Bearer "+tt.key)
			case tt.auth:
				req.Header.Set("Authorization", "Bearer "+env.gatewayKey)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != tt.want {
				b, _ := io.ReadAll(res.Body)
				t.Fatalf("got %d want %d body %s", res.StatusCode, tt.want, b)
			}
			if tt.want == 401 {
				var errBody map[string]any
				_ = json.NewDecoder(res.Body).Decode(&errBody)
				if errBody["error"].(map[string]any)["code"] != "invalid_api_key" {
					t.Fatalf("shape: %v", errBody)
				}
			}
		})
	}
}

func TestModelsListContent(t *testing.T) {
	env := setupEnv(t, "sk-moon", "sk-ds", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called")
	})
	res, body := env.doJSON(t, http.MethodGet, "/v1/models", true, nil)
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Object != "list" || len(payload.Data) != 5 {
		t.Fatalf("payload: %+v", payload)
	}
	ids := map[string]bool{}
	for _, m := range payload.Data {
		if m.ID == "" || m.OwnedBy != "openai" {
			t.Fatalf("entry: %+v", m)
		}
		ids[m.ID] = true
	}
	for _, want := range []string{"gpt-4o", "o3-mini", "gpt-5-nano"} {
		if !ids[want] {
			t.Fatalf("missing id %s", want)
		}
	}
}

func TestProxyMoonshotAndUsage(t *testing.T) {
	var sawModel string
	var sawPath string
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sawModel, _ = body["model"].(string)
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk-moon" {
			t.Errorf("upstream auth: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("kimi-k3"))
	})

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if sawModel != "kimi-k3" {
		t.Fatalf("upstream model %q", sawModel)
	}
	if !strings.Contains(sawPath, "moonshot") {
		t.Fatalf("path %q", sawPath)
	}

	store, err := usage.NewStore(env.dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events: %d", len(events))
	}
	if events[0].Provider != config.ProviderMoonshot || events[0].Model != "kimi-k3" {
		t.Fatalf("event: %+v", events[0])
	}
	if events[0].PromptTokens != 10 || events[0].CompletionTokens != 5 {
		t.Fatalf("tokens: %+v", events[0])
	}
}

func TestProxyDeepSeek(t *testing.T) {
	var sawPath string
	env := setupEnv(t, "", "sk-ds", func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "deepseek-v4-pro" {
			t.Errorf("model %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("deepseek-v4-pro"))
	})

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "o1",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	if !strings.Contains(sawPath, "deepseek") {
		t.Fatalf("path %q", sawPath)
	}
	store, _ := usage.NewStore(env.dataRoot)
	events, _ := store.LoadEvents()
	if len(events) != 1 || events[0].Provider != config.ProviderDeepSeek {
		t.Fatalf("events: %+v", events)
	}
}

func TestMissingDeepSeekKeyNoMoonshotFallback(t *testing.T) {
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not hit upstream")
	})
	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "o1",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "API key not configured") {
		t.Fatalf("expected key error: %s", body)
	}
	if env.upCalls.Load() != 0 {
		t.Fatalf("upstream calls %d", env.upCalls.Load())
	}
}

func TestStreamPassthrough(t *testing.T) {
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "gpt-4o-mini",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	if !strings.Contains(string(body), "data:") {
		t.Fatalf("not sse: %s", body)
	}
	store, _ := usage.NewStore(env.dataRoot)
	events, _ := store.LoadEvents()
	if len(events) != 1 || events[0].PromptTokens != 3 {
		t.Fatalf("usage events: %+v", events)
	}
}

func TestStreamSynthesize(t *testing.T) {
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("kimi-k2.6"))
	})
	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model":    "gpt-4o-mini",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("ct %q", res.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("synth body: %s", body)
	}
}

func TestToolCallIDRetry(t *testing.T) {
	var calls atomic.Int32
	env := setupEnv(t, "sk-moon", "", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"tool_call_id call_x not found"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("kimi-k2.6"))
	})

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model": "gpt-4o-mini",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id": "call_a", "type": "function",
					"function": map[string]any{"name": "x", "arguments": "{}"},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "wrong_id", "content": "ok"},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	if calls.Load() < 2 {
		t.Fatalf("expected retry, calls=%d", calls.Load())
	}
}
