package usageui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func TestReasoningEffortGetAndPut(t *testing.T) {
	dir := t.TempDir()
	store, err := usage.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	live := config.NewLiveSettings(dir, loaded)

	srv := NewServer("127.0.0.1:0", store)
	srv.SetLive(live)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/reasoning-effort", srv.handleReasoningEffort)

	t.Run("get defaults", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/reasoning-effort", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var resp ReasoningEffortResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Providers) < 2 {
			t.Fatalf("providers: %+v", resp.Providers)
		}
		foundK3 := false
		for _, p := range resp.Providers {
			for _, m := range p.Models {
				if m.ID == config.ModelKimiK3 {
					foundK3 = true
					if m.Effort != "low" {
						t.Fatalf("k3 effort %q", m.Effort)
					}
				}
			}
		}
		if !foundK3 {
			t.Fatal("missing kimi-k3")
		}
	})

	t.Run("put k3 high", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{config.ModelKimiK3: "high"})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/reasoning-effort", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if live.EffortFor(config.ModelKimiK3) != "high" {
			t.Fatalf("live effort %q", live.EffortFor(config.ModelKimiK3))
		}
		reloaded, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.ReasoningEffort[config.ModelKimiK3] != "high" {
			t.Fatalf("persisted %v", reloaded.ReasoningEffort)
		}
	})

	t.Run("put invalid", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{config.ModelKimiK3: "medium"})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/reasoning-effort", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d", w.Code)
		}
	})
}
