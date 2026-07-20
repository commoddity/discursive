package usageui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func testStore(t *testing.T) *usage.Store {
	t.Helper()
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Seed some events.
	for i := 0; i < 3; i++ {
		_, _ = store.Record(usage.Event{
			SessionID:        "sess-test",
			Provider:         config.ProviderMoonshot,
			Model:            "kimi-k3",
			PromptTokens:     1000,
			CompletionTokens: 200,
			Timestamp:        time.Now().UTC().Add(-time.Duration(i) * 24 * time.Hour),
		})
	}
	_, _ = store.Record(usage.Event{
		SessionID:        "sess-deep",
		Provider:         config.ProviderDeepSeek,
		Model:            "deepseek-v4-flash",
		PromptTokens:     5000,
		CompletionTokens: 1000,
		Timestamp:        time.Now().UTC(),
	})
	_, _ = store.Record(usage.Event{
		SessionID:        "sess-thaura",
		Provider:         config.ProviderThaura,
		Model:            "thaura",
		PromptTokens:     2000,
		CompletionTokens: 500,
		Timestamp:        time.Now().UTC(),
	})
	return store
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store := testStore(t)
	srv := &Server{addr: "", store: store}
	srv.SetHealth(HealthInfo{
		Version:        "0.0.0-test",
		PID:            12345,
		HasMoonshotKey: true,
		HasDeepSeekKey: true,
		HasThauraKey:   true,
		TunnelMode:     "quick",
		PublicURL:      "https://example.trycloudflare.com/v1",
		LocalPort:      4001,
		GatewayKey:     "sk-test-gateway-key-for-dashboard",
	})
	return srv
}

func doJSON(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/static/", http.NotFound) // not tested here
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/summary", srv.handleSummary)
	mux.HandleFunc("/api/by-day", srv.handleByDay)
	mux.HandleFunc("/api/by-model", srv.handleByModel)
	mux.HandleFunc("/api/by-provider", srv.handleByProvider)
	mux.HandleFunc("/api/sessions", srv.handleSessions)
	mux.HandleFunc("/api/health", srv.handleHealth)

	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func TestIndexPage(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type: %q", ct)
	}
}

func TestAPISummary(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/summary")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var ds usage.DailySummary
	if err := json.Unmarshal(w.Body.Bytes(), &ds); err != nil {
		t.Fatal(err)
	}
	if ds.RequestCount < 1 {
		t.Fatalf("expected at least 1 request, got %d", ds.RequestCount)
	}
}

func TestAPIByDay(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-day")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var days []usage.DailySummary
	if err := json.Unmarshal(w.Body.Bytes(), &days); err != nil {
		t.Fatal(err)
	}
	if len(days) < 1 {
		t.Fatalf("expected at least 1 day, got %d", len(days))
	}
}

func TestAPIByModel(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-model")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var models []usage.ModelBreakdown
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) < 1 {
		t.Fatalf("expected at least 1 model, got %d", len(models))
	}
}

func TestAPIByProvider(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-provider")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var provs []usage.ProviderBreakdown
	if err := json.Unmarshal(w.Body.Bytes(), &provs); err != nil {
		t.Fatal(err)
	}
	if len(provs) < 1 {
		t.Fatalf("expected at least 1 provider, got %d", len(provs))
	}
}

func TestAPISessions(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/sessions")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var sessions []usage.SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 1 {
		t.Fatalf("expected at least 1 session, got %d", len(sessions))
	}
}

func TestAPIHealth(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/health")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var h HealthInfo
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.Version != "0.0.0-test" {
		t.Fatalf("version: %q", h.Version)
	}
	if h.TunnelMode != "quick" {
		t.Fatalf("tunnel_mode: %q", h.TunnelMode)
	}
	if !h.HasMoonshotKey {
		t.Fatal("expected has_moonshot_key")
	}
	if !h.HasThauraKey {
		t.Fatal("expected has_thaura_key")
	}
}

func TestAPISessionDetail(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/sessions?session_id=sess-test")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var ds usage.DailySummary
	if err := json.Unmarshal(w.Body.Bytes(), &ds); err != nil {
		t.Fatal(err)
	}
	if ds.RequestCount < 1 {
		t.Fatalf("expected at least 1 request, got %d", ds.RequestCount)
	}
	if len(ds.ByModel) < 1 {
		t.Fatalf("expected by_model breakdown, got %d", len(ds.ByModel))
	}
}

func TestAPIByDaySince(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-day?since=2025-01-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var days []usage.DailySummary
	if err := json.Unmarshal(w.Body.Bytes(), &days); err != nil {
		t.Fatal(err)
	}
	// All seeded events are after 2025, so should produce at least 1 day.
	if len(days) < 1 {
		t.Fatalf("expected at least 1 day with since filter, got %d", len(days))
	}
}

func TestAPIByModelSince(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-model?since=2025-01-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var models []usage.ModelBreakdown
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) < 1 {
		t.Fatalf("expected at least 1 model, got %d", len(models))
	}
}

func TestAPIByProviderSince(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-provider?since=2025-01-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var provs []usage.ProviderBreakdown
	if err := json.Unmarshal(w.Body.Bytes(), &provs); err != nil {
		t.Fatal(err)
	}
	if len(provs) < 1 {
		t.Fatalf("expected at least 1 provider, got %d", len(provs))
	}
}

func TestAPISessionsSince(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/sessions?since=2025-01-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var sessions []usage.SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 1 {
		t.Fatalf("expected at least 1 session, got %d", len(sessions))
	}
}

func TestAPIBadSince(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, "/api/by-day?since=not-a-date")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad since, got %d", w.Code)
	}
}
