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

func TestVerbosityGetAndPut(t *testing.T) {
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
	mux.HandleFunc("/api/verbosity", srv.handleVerbosity)

	t.Run("get defaults", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/verbosity", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var resp VerbosityResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		foundFlash := false
		for _, m := range resp.Models {
			if m.ID == config.ModelDeepSeekV4Flash {
				foundFlash = true
				if !m.Enabled {
					t.Fatalf("expected flash verbosity on by default, got %+v", m)
				}
			}
			if m.ID == config.ModelDeepSeekV4Pro && m.Enabled {
				t.Fatalf("expected pro verbosity off by default, got %+v", m)
			}
		}
		if !foundFlash {
			t.Fatal("missing deepseek-v4-flash")
		}
	})

	t.Run("put flash off", func(t *testing.T) {
		body, _ := json.Marshal(map[string]bool{config.ModelDeepSeekV4Flash: false})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/verbosity", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if live.VerbosityFor(config.ModelDeepSeekV4Flash) {
			t.Fatal("live flash verbosity should be off")
		}
		reloaded, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Verbosity[config.ModelDeepSeekV4Flash] {
			t.Fatalf("persisted %v", reloaded.Verbosity)
		}
	})

	t.Run("put unknown model rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]bool{"bogus-model": true})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/verbosity", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}
