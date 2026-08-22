package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/usage"
)

func TestProxyDeepSeekImagesDescribedByVision(t *testing.T) {
	// Upstream text model (deepseek) — verifies no image_url in body.
	var textCallCount atomic.Int32
	var lastTextBody map[string]any
	textUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		textCallCount.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&lastTextBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("deepseek-v4-pro"))
	}))
	t.Cleanup(textUp.Close)

	// Vision server (glm-4.6v) — returns a fake description.
	var visionCallCount atomic.Int32
	visionUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCallCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "Screenshot of an IDE showing a Go file",
					},
				},
			},
		})
	}))
	t.Cleanup(visionUp.Close)

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	// Peak guard OFF: test DeepSeek image+vision path directly.
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetZaiKey(dataRoot, "sk-zai"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: "127.0.0.1:0",
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
		HTTPClient: textUp.Client(),
		ChatURLOverride: map[config.Provider]string{
			config.ProviderDeepSeek: textUp.URL + "/deepseek/chat/completions",
		},
		VisionChatURLOverride: visionUp.URL + "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	env := &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot}

	// Send a request with an image to a deepseek model.
	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model": "o1",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "what is in this screenshot?"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,fakeimagedata"},
					},
				},
			},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	// Vision should have been called once.
	if visionCallCount.Load() != 1 {
		t.Fatalf("expected 1 vision call, got %d", visionCallCount.Load())
	}

	// Text model should have been called once.
	if textCallCount.Load() != 1 {
		t.Fatalf("expected 1 text call, got %d", textCallCount.Load())
	}

	// The text model should receive no image_url parts.
	msgs := lastTextBody["messages"].([]any)
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		content := msg["content"]
		switch c := content.(type) {
		case []any:
			for _, p := range c {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if part["type"] == "image_url" {
					t.Fatalf("text model received image_url part: %v", part)
				}
				if _, ok := part["image_url"]; ok && part["type"] != "text" {
					t.Fatalf("text model received image_url field: %v", part)
				}
			}
		case string:
			if strings.Contains(c, "image_url") {
				t.Fatalf("text model received image_url in string content: %s", c)
			}
		}
	}

	// Verify the description is present.
	foundDesc := false
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		content := msg["content"]
		switch c := content.(type) {
		case []any:
			for _, p := range c {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := part["text"].(string); ok && strings.Contains(text, "Screenshot of an IDE") {
					foundDesc = true
				}
			}
		case string:
			if strings.Contains(c, "Screenshot of an IDE") {
				foundDesc = true
			}
		}
	}
	if !foundDesc {
		t.Fatalf("expected image description in text model input, got %v", lastTextBody["messages"])
	}

	// Usage should be recorded for both the text model and the vision worker
	// (vision calls are now metered into the usage store, in their own sentinel
	// session so they don't pollute the chat session grouping).
	store, _ := usage.NewStore(env.dataRoot)
	events, _ := store.LoadEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 usage events (chat + vision), got %d: %+v", len(events), events)
	}
	var chatEvent, visionEvent *usage.Event
	for i := range events {
		switch events[i].Provider {
		case config.ProviderDeepSeek:
			chatEvent = &events[i]
		case config.ProviderZai:
			visionEvent = &events[i]
		}
	}
	if chatEvent == nil {
		t.Fatalf("expected a deepseek chat usage event, got %+v", events)
	}
	if visionEvent == nil {
		t.Fatalf("expected a zai vision usage event, got %+v", events)
	}
	if visionEvent.Model != "glm-4.6v" {
		t.Fatalf("vision usage event model = %q, want glm-4.6v", visionEvent.Model)
	}
	if visionEvent.SessionID != "vision-worker" {
		t.Fatalf("vision usage event session = %q, want vision-worker", visionEvent.SessionID)
	}
}

func TestProxyVisionAppliedToAllProviders(t *testing.T) {
	// The text model (moonshot) receives the image description, and vision IS
	// called — the vision fallback applies to every provider now.
	var visionCallCount atomic.Int32
	visionUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCallCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "Vision-described image for moonshot",
					},
				},
			},
		})
	}))
	t.Cleanup(visionUp.Close)

	textUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockCompletion("kimi-k3"))
	}))
	t.Cleanup(textUp.Close)

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetMoonshotKey(dataRoot, "sk-moon"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetZaiKey(dataRoot, "sk-zai"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: "127.0.0.1:0",
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
		HTTPClient: textUp.Client(),
		ChatURLOverride: map[config.Provider]string{
			config.ProviderMoonshot: textUp.URL + "/moonshot/chat/completions",
		},
		VisionChatURLOverride: visionUp.URL + "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	env := &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot}

	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "describe this"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,fake"},
					},
				},
			},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if visionCallCount.Load() != 1 {
		t.Fatalf("expected 1 vision call for moonshot image, got %d", visionCallCount.Load())
	}

	// kimi-k2.7-code (via the gpt-4o-mini alias) must also flow through the
	// vision worker before the text model. Use a distinct image to bypass cache.
	res, body = env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model": "gpt-4o-mini",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "describe this"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,cccc"},
					},
				},
			},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if visionCallCount.Load() != 2 {
		t.Fatalf("expected 2 vision calls (k3 + k2.7-code), got %d", visionCallCount.Load())
	}
}

func TestProxyImageWithoutVisionKeyFallsBack(t *testing.T) {
	// No Z.AI key configured. An image request must still proceed: the image
	// is replaced with a placeholder note and the text model is called.
	var textCalled atomic.Int32
	textUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		textCalled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(textUp.Close)

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	// Peak guard OFF for deterministic DeepSeek-routing in this test.
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	// No Z.AI key set.
	if err := settings.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: "127.0.0.1:0",
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
		HTTPClient: textUp.Client(),
		ChatURLOverride: map[config.Provider]string{
			config.ProviderDeepSeek: textUp.URL + "/deepseek/chat/completions",
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
		"model": "o1",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "what is this?"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,fake"},
					},
				},
			},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("expected 200 graceful fallback, got %d body %s", res.StatusCode, body)
	}
	if textCalled.Load() == 0 {
		t.Fatalf("text model must still be called when vision falls back to a placeholder")
	}
}

// TestProxy_SubAgentRouterProviderSwitch verifies that when the subagent router
// downgrades a model to a model from a DIFFERENT provider, the gateway
// re-resolves the upstream provider so the request is sent to the correct
// endpoint. Regression for the "modelCode: does not exist" cross-provider
// bug (e.g. gpt-4o→kimi-k3 downgraded to deepseek-v4-flash must reach the
// DeepSeek endpoint, not the Moonshot endpoint).
func TestProxy_SubAgentRouterProviderSwitch(t *testing.T) {
	var moonshotCalled atomic.Int32
	moonshotUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moonshotCalled.Add(1)
		t.Log("moonshot upstream hit (should not happen for downgraded request)")
		_ = json.NewEncoder(w).Encode(mockCompletion("kimi-k3"))
	}))
	t.Cleanup(moonshotUp.Close)

	var zaiModel string
	var zaiCalled atomic.Int32
	zaiUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaiCalled.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		zaiModel, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(mockCompletion(zaiModel))
	}))
	t.Cleanup(zaiUp.Close)

	dataRoot := t.TempDir()
	settings := config.DefaultSettings()
	if err := settings.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetMoonshotKey(dataRoot, "sk-ms"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetZaiKey(dataRoot, "sk-zai"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetOpenRouterKey(dataRoot, "sk-or"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, settings); err != nil {
		t.Fatal(err)
	}

	var orCalled atomic.Int32
	var orModel string
	orUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orCalled.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		orModel, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(mockCompletion(orModel))
	}))
	t.Cleanup(orUp.Close)

	var dsCalled atomic.Int32
	var dsModel string
	dsUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dsCalled.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		dsModel, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(mockCompletion(dsModel))
	}))
	t.Cleanup(dsUp.Close)

	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr:            "127.0.0.1:0",
		GatewayKey:            settings.GatewayKey,
		DataRoot:              dataRoot,
		Settings:              &settings,
		HTTPClient:            orUp.Client(),
		SubAgentRouterEnabled: true,
		ChatURLOverride: map[config.Provider]string{
			config.ProviderMoonshot:   moonshotUp.URL + "/moonshot/chat/completions",
			config.ProviderZai:        zaiUp.URL + "/zai/chat/completions",
			config.ProviderOpenRouter: orUp.URL + "/openrouter/chat/completions",
			config.ProviderDeepSeek:   dsUp.URL + "/deepseek/chat/completions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	env := &testEnv{srv: srv, ts: ts, gatewayKey: settings.GatewayKey, dataRoot: dataRoot}

	// gpt-4o resolves to kimi-k3 (Moonshot). It's a short, simple lookup —
	// classified SimpleLookup → downgraded to defaultFlashModel
	// (deepseek-v4-flash). Off-peak the request must reach the direct
	// DeepSeek upstream (OpenRouter is peak-only), with the overridden model.
	res, body := env.doJSON(t, http.MethodPost, "/v1/chat/completions", true, map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "what is a goroutine?"},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	if zaiCalled.Load() != 0 {
		t.Fatalf("Z.AI upstream was called %d times for a downgraded request, it must not be", zaiCalled.Load())
	}
	if moonshotCalled.Load() != 0 {
		t.Fatalf("Moonshot upstream was called %d times for a downgraded request, it must not be", moonshotCalled.Load())
	}
	if orCalled.Load() != 0 {
		t.Fatalf("OpenRouter upstream was called %d times off-peak, it must not be", orCalled.Load())
	}
	if dsCalled.Load() != 1 {
		t.Fatalf("expected DeepSeek upstream to be called exactly once, got %d", dsCalled.Load())
	}
	if dsModel != "deepseek-v4-flash" {
		t.Fatalf("expected overridden model sent to DeepSeek, got %q", dsModel)
	}
}
