package usageui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func TestSettingsGetAndPut(t *testing.T) {
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
	mux.HandleFunc("/api/settings", srv.handleSettings)

	t.Run("get defaults", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var dto GatewaySettingsDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatal(err)
		}
		if dto.ToolCompressionEnabled {
			t.Fatalf("expected false defaults, got %+v", dto)
		}
	})

	t.Run("put enables", func(t *testing.T) {
		body := strings.NewReader(`{"toolCompressionEnabled":true}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var dto GatewaySettingsDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatal(err)
		}
		if !dto.ToolCompressionEnabled {
			t.Fatalf("expected compression enabled, got %+v", dto)
		}
		// Persisted into live settings.
		if !live.ToolCompressionEnabled() {
			t.Fatalf("live settings not updated")
		}
	})

	t.Run("persists across reload", func(t *testing.T) {
		reloaded, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reloaded.ToolCompressionEnabled {
			t.Fatalf("settings not persisted to config.json: %+v", reloaded)
		}
	})

	t.Run("put invalid json rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("not-json"))
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestSettingsServiceUnavailableWhenNoLive(t *testing.T) {
	dir := t.TempDir()
	store, err := usage.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv := NewServer("127.0.0.1:0", store) // no SetLive
	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings", srv.handleSettings)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
