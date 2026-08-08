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
		HasZaiKey:      true,
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
	mux.HandleFunc("/api/balances", srv.handleBalances)

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

// apiTest describes a single table-driven API endpoint test. On success
// (expected status code match), the response body is unmarshaled into a fresh
// T and check is invoked with the decoded value. When check is nil only the
// status code is asserted.
type apiTest[T any] struct {
	name           string
	path           string
	expectedStatus int
	check          func(t *testing.T, got T)
}

// runAPITest executes one apiTest entry against a fresh test server.
func runAPITest[T any](t *testing.T, tc apiTest[T]) {
	t.Helper()
	srv := newTestServer(t)
	w := doJSON(t, srv, tc.path)
	if w.Code != tc.expectedStatus {
		t.Fatalf("status %d, want %d", w.Code, tc.expectedStatus)
	}
	if tc.check == nil {
		return
	}
	var got T
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	tc.check(t, got)
}

func TestAPIEndpoints(t *testing.T) {
	// --- /api/summary ---
	t.Run("TestAPISummary", func(t *testing.T) {
		runAPITest(t, apiTest[usage.DailySummary]{
			name:           "TestAPISummary",
			path:           "/api/summary",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, ds usage.DailySummary) {
				if ds.RequestCount < 1 {
					t.Fatalf("expected at least 1 request, got %d", ds.RequestCount)
				}
			},
		})
	})

	// --- /api/by-day ---
	t.Run("TestAPIByDay", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.DailySummary]{
			name:           "TestAPIByDay",
			path:           "/api/by-day",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, days []usage.DailySummary) {
				if len(days) < 1 {
					t.Fatalf("expected at least 1 day, got %d", len(days))
				}
			},
		})
	})

	// --- /api/by-model ---
	t.Run("TestAPIByModel", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.ModelBreakdown]{
			name:           "TestAPIByModel",
			path:           "/api/by-model",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, models []usage.ModelBreakdown) {
				if len(models) < 1 {
					t.Fatalf("expected at least 1 model, got %d", len(models))
				}
			},
		})
	})

	// --- /api/by-provider ---
	t.Run("TestAPIByProvider", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.ProviderBreakdown]{
			name:           "TestAPIByProvider",
			path:           "/api/by-provider",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, provs []usage.ProviderBreakdown) {
				if len(provs) < 1 {
					t.Fatalf("expected at least 1 provider, got %d", len(provs))
				}
			},
		})
	})

	// --- /api/sessions ---
	t.Run("TestAPISessions", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.SessionInfo]{
			name:           "TestAPISessions",
			path:           "/api/sessions",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, sessions []usage.SessionInfo) {
				if len(sessions) < 1 {
					t.Fatalf("expected at least 1 session, got %d", len(sessions))
				}
			},
		})
	})

	// --- /api/health ---
	t.Run("TestAPIHealth", func(t *testing.T) {
		runAPITest(t, apiTest[HealthInfo]{
			name:           "TestAPIHealth",
			path:           "/api/health",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, h HealthInfo) {
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
				if !h.HasZaiKey {
					t.Fatal("expected has_zai_key")
				}
			},
		})
	})

	// --- /api/sessions?session_id=sess-test ---
	t.Run("TestAPISessionDetail", func(t *testing.T) {
		runAPITest(t, apiTest[usage.DailySummary]{
			name:           "TestAPISessionDetail",
			path:           "/api/sessions?session_id=sess-test",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, ds usage.DailySummary) {
				if ds.RequestCount < 1 {
					t.Fatalf("expected at least 1 request, got %d", ds.RequestCount)
				}
				if len(ds.ByModel) < 1 {
					t.Fatalf("expected by_model breakdown, got %d", len(ds.ByModel))
				}
			},
		})
	})

	// --- /api/by-day?since=2025-01-01T00:00:00Z ---
	t.Run("TestAPIByDaySince", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.DailySummary]{
			name:           "TestAPIByDaySince",
			path:           "/api/by-day?since=2025-01-01T00:00:00Z",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, days []usage.DailySummary) {
				// All seeded events are after 2025, so should produce at least 1 day.
				if len(days) < 1 {
					t.Fatalf("expected at least 1 day with since filter, got %d", len(days))
				}
			},
		})
	})

	// --- /api/by-model?since=2025-01-01T00:00:00Z ---
	t.Run("TestAPIByModelSince", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.ModelBreakdown]{
			name:           "TestAPIByModelSince",
			path:           "/api/by-model?since=2025-01-01T00:00:00Z",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, models []usage.ModelBreakdown) {
				if len(models) < 1 {
					t.Fatalf("expected at least 1 model, got %d", len(models))
				}
			},
		})
	})

	// --- /api/by-provider?since=2025-01-01T00:00:00Z ---
	t.Run("TestAPIByProviderSince", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.ProviderBreakdown]{
			name:           "TestAPIByProviderSince",
			path:           "/api/by-provider?since=2025-01-01T00:00:00Z",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, provs []usage.ProviderBreakdown) {
				if len(provs) < 1 {
					t.Fatalf("expected at least 1 provider, got %d", len(provs))
				}
			},
		})
	})

	// --- /api/sessions?since=2025-01-01T00:00:00Z ---
	t.Run("TestAPISessionsSince", func(t *testing.T) {
		runAPITest(t, apiTest[[]usage.SessionInfo]{
			name:           "TestAPISessionsSince",
			path:           "/api/sessions?since=2025-01-01T00:00:00Z",
			expectedStatus: http.StatusOK,
			check: func(t *testing.T, sessions []usage.SessionInfo) {
				if len(sessions) < 1 {
					t.Fatalf("expected at least 1 session, got %d", len(sessions))
				}
			},
		})
	})

	// --- /api/by-day?since=not-a-date (bad input → 400, no body check) ---
	t.Run("TestAPIBadSince", func(t *testing.T) {
		runAPITest(t, apiTest[json.RawMessage]{
			name:           "TestAPIBadSince",
			path:           "/api/by-day?since=not-a-date",
			expectedStatus: http.StatusBadRequest,
			check:          nil,
		})
	})
}

func TestAPIByDayModelEmptyPadsHourBuckets(t *testing.T) {
	// Empty store — Today-style range must still return hour slots for the empty chart axis.
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{addr: "", store: store}

	since := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 23, 2, 30, 0, 0, time.UTC) // → 3 hour buckets (00, 01, 02)
	path := "/api/by-day-model?since=" + since.Format(time.RFC3339) +
		"&until=" + until.Format(time.RFC3339) + "&bucket=1h"

	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	srv.handleByDayModel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}

	var rows []struct {
		Bucket   string  `json:"bucket"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		EstUSD   float64 `json:"est_usd"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 padded hour buckets, got %d: %+v", len(rows), rows)
	}
	want := []string{
		"2026-07-23T00:00:00",
		"2026-07-23T01:00:00",
		"2026-07-23T02:00:00",
	}
	for i, r := range rows {
		if r.Bucket != want[i] {
			t.Errorf("row %d bucket=%q want %q", i, r.Bucket, want[i])
		}
		if r.Provider != "" || r.Model != "" || r.EstUSD != 0 {
			t.Errorf("row %d should be empty placeholder, got %+v", i, r)
		}
	}
}
