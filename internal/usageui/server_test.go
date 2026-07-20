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
	return store
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store := testStore(t)
	return &Server{addr: "", store: store}
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
