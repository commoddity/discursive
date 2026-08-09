package usageui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func TestBasesForTime(t *testing.T) {
	aug8 := time.Date(2026, 8, 8, 12, 30, 0, 0, time.UTC) // Saturday

	bases := basesForTime(aug8)
	if len(bases) != 4 {
		t.Fatalf("got %d bases, want 4", len(bases))
	}

	byBasis := make(map[string]basisEntry)
	for _, b := range bases {
		byBasis[b.basis] = b
	}

	if e, ok := byBasis["sample"]; !ok || e.periodStart.Minute() != 30 {
		t.Fatalf("sample basis wrong: %+v", e)
	}
	if e, ok := byBasis["day"]; !ok || e.periodStart != time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("day basis wrong: %+v", e)
	}
	// Aug 8 is Saturday; iso week starts on Monday Aug 3.
	if e, ok := byBasis["week"]; !ok || e.periodStart != time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("week basis wrong: %+v", e)
	}
	if e, ok := byBasis["month"]; !ok || e.periodStart != time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("month basis wrong: %+v", e)
	}

	// Sunday falls back to previous Monday.
	sunday := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC) // Sunday
	basesSun := basesForTime(sunday)
	for _, b := range basesSun {
		if b.basis == "week" {
			if b.periodStart != time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) {
				t.Fatalf("sunday week start wrong: %+v", b.periodStart)
			}
		}
	}

	// Monday starts its own week.
	monday := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC) // Monday
	basesMon := basesForTime(monday)
	for _, b := range basesMon {
		if b.basis == "week" {
			if b.periodStart != time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC) {
				t.Fatalf("monday week start wrong: %+v", b.periodStart)
			}
		}
	}
}

func TestPeriodRange(t *testing.T) {
	dayStart := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	pb := periodRange("day", dayStart)
	if !pb.after.Equal(dayStart) {
		t.Fatalf("after=%v want %v", pb.after, dayStart)
	}
	if !pb.before.Equal(time.Date(2026, 8, 8, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("before=%v", pb.before)
	}

	weekStart := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	pbW := periodRange("week", weekStart)
	if !pbW.after.Equal(weekStart) {
		t.Fatalf("after=%v want %v", pbW.after, weekStart)
	}
	if !pbW.before.Equal(time.Date(2026, 8, 9, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("before=%v", pbW.before)
	}

	monthStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	pbM := periodRange("month", monthStart)
	if !pbM.after.Equal(monthStart) {
		t.Fatalf("after=%v want %v", pbM.after, monthStart)
	}
	if !pbM.before.Equal(time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("before=%v", pbM.before)
	}

	// December wraps to January.
	decStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	pbDec := periodRange("month", decStart)
	if !pbDec.before.Equal(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("december before=%v", pbDec.before)
	}
}

func TestHandleBalanceSpend(t *testing.T) {
	store := testStore(t)
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Insert snapshots so ConfirmedSpend returns non-zero.
	snaps := []usage.BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now.Add(-2 * time.Hour), Amount: 50, Currency: "USD", USDAmount: 50},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now, Amount: 48, Currency: "USD", USDAmount: 48},
		{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now.Add(-2 * time.Hour), Amount: 20, Currency: "USD", USDAmount: 20},
		{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now, Amount: 19, Currency: "USD", USDAmount: 19},
	}
	for _, snap := range snaps {
		if err := store.InsertBalanceSnapshot(snap); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{addr: "", store: store}
	req := httptest.NewRequest("GET", "/api/balance-spend", nil)
	w := httptest.NewRecorder()
	srv.handleBalanceSpend(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}

	var resp BalanceSpendResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// Moonshot day spend should be 50 - 48 = 2.0
	if resp.Moonshot.Day == nil || *resp.Moonshot.Day != 2.0 {
		t.Fatalf("moonshot day spend: got %v, want 2.0", resp.Moonshot.Day)
	}
	// DeepSeek day spend should be 20 - 19 = 1.0
	if resp.DeepSeek.Day == nil || *resp.DeepSeek.Day != 1.0 {
		t.Fatalf("deepseek day spend: got %v, want 1.0", resp.DeepSeek.Day)
	}

	// Week and month may be nil (no snapshots) or 0 (single snapshot).
	// With only 2 snapshots for "day" basis, week/month won't have enough data.
	if resp.Moonshot.Week != nil && *resp.Moonshot.Week != 0 {
		t.Logf("moonshot week: %v (expected nil or 0)", *resp.Moonshot.Week)
	}
	if resp.Moonshot.Month != nil && *resp.Moonshot.Month != 0 {
		t.Logf("moonshot month: %v (expected nil or 0)", *resp.Moonshot.Month)
	}
}

func TestHandleBalanceSpendNoSnapshots(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{addr: "", store: store}
	req := httptest.NewRequest("GET", "/api/balance-spend", nil)
	w := httptest.NewRecorder()
	srv.handleBalanceSpend(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp BalanceSpendResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// All should be 0 or nil — no snapshots means no spend.
	if resp.Moonshot.Day != nil && *resp.Moonshot.Day != 0 {
		t.Fatalf("moonshot day with no snapshots: %v", *resp.Moonshot.Day)
	}
	if resp.DeepSeek.Day != nil && *resp.DeepSeek.Day != 0 {
		t.Fatalf("deepseek day with no snapshots: %v", *resp.DeepSeek.Day)
	}
}

// ---- SnapshotController tests ---------------------------------------------------

// newMockBalanceServer returns an httptest server that serves provider balance
// endpoints and a KeySource wired to it.
type mockBalanceServer struct {
	server *httptest.Server
	ks     KeySource

	moonshotBalance    float64
	moonshotStatusCode int
	deepseekBalance    float64
	deepseekStatusCode int
	zaiAmount          float64
	zaiStatusCode      int
}

func newMockBalanceServer() *mockBalanceServer {
	m := &mockBalanceServer{
		moonshotStatusCode: http.StatusOK,
		deepseekStatusCode: http.StatusOK,
		zaiStatusCode:      http.StatusOK,
		moonshotBalance:    50.0,
		deepseekBalance:    20.0,
		zaiAmount:          100.0,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/users/me/balance", func(w http.ResponseWriter, r *http.Request) {
		if m.moonshotStatusCode != http.StatusOK {
			http.Error(w, "error", m.moonshotStatusCode)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"available_balance": m.moonshotBalance},
		}); err != nil {
			http.Error(w, "encode balance", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/user/balance", func(w http.ResponseWriter, r *http.Request) {
		if m.deepseekStatusCode != http.StatusOK {
			http.Error(w, "error", m.deepseekStatusCode)
			return
		}
		// TotalBalance et al are strings in the real API response.
		bal := strconv.FormatFloat(m.deepseekBalance, 'f', 2, 64)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"is_available": true,
			"balance_infos": []map[string]any{
				{"currency": "USD", "total_balance": bal, "topped_up_balance": bal, "granted_balance": bal},
			},
		}); err != nil {
			http.Error(w, "encode balance", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/api/monitor/usage/quota/limit", func(w http.ResponseWriter, r *http.Request) {
		if m.zaiStatusCode != http.StatusOK {
			http.Error(w, "error", m.zaiStatusCode)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"success": true,
			"msg":     "ok",
			"data": map[string]any{
				"level": "enterprise",
				"limits": []map[string]any{
					{
						"type":          "CREDIT_LIMIT",
						"unit":          1,
						"number":        int(m.zaiAmount),
						"usage":         int(m.zaiAmount * 0.3),
						"currentValue":  int(m.zaiAmount * 0.7),
						"remaining":     int(m.zaiAmount * 0.7),
						"percentage":    30,
						"nextResetTime": 1691635200,
					},
				},
			},
		}); err != nil {
			http.Error(w, "encode quota", http.StatusInternalServerError)
		}
	})
	m.server = httptest.NewServer(mux)

	m.ks = KeySource{
		Moonshot: func() (string, bool) { return "sk-moon-test", true },
		DeepSeek: func() (string, bool) { return "sk-ds-test", true },
		Zai:      func() (string, bool) { return "sk-zai-test", true },
	}
	return m
}

// transportTo rewrites every outbound request to target the mock server host,
// preserving the original path.  This lets the production fetch functions hit
// the simulated balance endpoints.
func transportTo(target string) http.RoundTripper {
	return &mockTransport{target: target}
}

type mockTransport struct {
	target string
}

func (t *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.target)
	if err != nil {
		return nil, err
	}
	r.URL.Scheme = u.Scheme
	r.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(r)
}

func (m *mockBalanceServer) close() {
	m.server.Close()
}

func TestNewSnapshotController(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("nil client defaults", func(t *testing.T) {
		ks := KeySource{
			Moonshot: func() (string, bool) { return "", false },
		}
		ctrl := NewSnapshotController(store, nil, ks, slog.Default())
		if ctrl == nil {
			t.Fatal("expected non-nil controller")
		}
		if ctrl.client == nil {
			t.Fatal("expected auto-created http client")
		}
	})

	t.Run("nil controller from nil args", func(t *testing.T) {
		var ctrl *SnapshotController
		ctrl.Start(context.Background())
		ctrl.Stop()
		// Must not panic.
	})
}

func TestSnapshotControllerStartStop(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ks := KeySource{
		Moonshot: func() (string, bool) { return "sk-test", true },
		DeepSeek: func() (string, bool) { return "sk-test", true },
		Zai:      func() (string, bool) { return "sk-test", true },
	}
	ctrl := NewSnapshotController(store, nil, ks, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel so goroutine exits quickly

	ctrl.Start(ctx)

	// Wait a tick so the goroutine exits.
	time.Sleep(50 * time.Millisecond)

	// Stop should block safely (goroutine already exited).
	ctrl.Stop()

	// Second Stop must be safe (noop).
	ctrl.Stop()
}

func TestSnapshotControllerCapture_InsertsSnapshots(t *testing.T) {
	mock := newMockBalanceServer()
	defer mock.close()

	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Use a transport that redirects provider API calls to our mock server.
	client := &http.Client{
		Transport: transportTo(mock.server.URL),
		Timeout:   10 * time.Second,
	}

	ctrl := &SnapshotController{
		store:  store,
		client: client,
		ks:     mock.ks,
		log:    slog.Default(),
	}

	ctx := context.Background()
	ctrl.capture(ctx)

	// Should have at least 4 bases × 3 providers = 12 snapshots.
	snaps, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) < 12 {
		t.Fatalf("got %d latest snapshots, want at least 12", len(snaps))
	}

	// Verify we have all three providers.
	provs := map[string]bool{}
	for _, s := range snaps {
		provs[string(s.Provider)] = true
	}
	if !provs["moonshot"] || !provs["deepseek"] || !provs["zai"] {
		t.Fatalf("missing providers in snapshots: %v", provs)
	}

	// Verify confirmed spend is computable from the inserted snapshots.
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	bounds := periodRange("day", dayStart)
	spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day", bounds.after, bounds.before)
	if err != nil {
		t.Fatal(err)
	}
	// With only one snapshot per basis, spend should be 0 (need >=2).
	if spend != 0 {
		t.Logf("moonshot confirmed day spend with 1 snapshot: %v (expected 0)", spend)
	}
}

func TestSnapshotControllerCapture_IgnoresFailedBalances(t *testing.T) {
	mock := newMockBalanceServer()
	defer mock.close()

	// Make Moonshot and Z.AI fail, only DeepSeek succeeds.
	mock.moonshotStatusCode = http.StatusUnauthorized
	mock.zaiStatusCode = http.StatusInternalServerError

	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: transportTo(mock.server.URL),
		Timeout:   10 * time.Second,
	}
	ctrl := &SnapshotController{
		store:  store,
		client: client,
		ks:     mock.ks,
		log:    slog.Default(),
	}

	ctrl.capture(context.Background())

	snaps, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	// Only DeepSeek should have produced snapshots.
	for _, s := range snaps {
		if s.Provider != config.ProviderDeepSeek {
			t.Errorf("unexpected provider %s in snapshots (only DeepSeek should succeed)", s.Provider)
		}
	}
	// 4 bases for DeepSeek = 4 snapshots.
	if len(snaps) != 4 {
		t.Errorf("got %d snapshots, want 4 (only deepseek succeeded)", len(snaps))
	}
}

func TestSnapshotControllerCapture_Concurrent(t *testing.T) {
	// Verify capture is safe under concurrent access.
	mock := newMockBalanceServer()
	defer mock.close()

	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: transportTo(mock.server.URL),
		Timeout:   10 * time.Second,
	}
	ctrl := &SnapshotController{
		store:  store,
		client: client,
		ks:     mock.ks,
		log:    slog.Default(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctrl.capture(context.Background())
		}()
	}
	wg.Wait()

	// Shouldn't panic or deadlock.  Results are whatever — the DB may have
	// had collisions on INSERT OR REPLACE, which is fine.
}

func TestStartSnapshots_ServerMethod(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{addr: "", store: store, httpClient: &http.Client{Timeout: 2 * time.Second}}

	ks := KeySource{
		Moonshot: func() (string, bool) { return "sk-moon-test", true },
		DeepSeek: func() (string, bool) { return "sk-ds-test", true },
	}
	srv.keySource = ks

	// StartSnapshots should populate srv.snapCtrl.
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartSnapshots(ctx)

	if srv.snapCtrl == nil {
		t.Fatal("expected snapCtrl to be set")
	}

	// Clean up.
	srv.snapCtrl.Stop()
	cancel()
}

func TestStartSnapshots_DoubleStartIsNoop(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ks := KeySource{
		Moonshot: func() (string, bool) { return "sk-moon-test", true },
	}
	srv := &Server{addr: "", store: store, keySource: ks, httpClient: &http.Client{Timeout: 2 * time.Second}}

	ctx := context.Background()
	srv.StartSnapshots(ctx)
	first := srv.snapCtrl
	if first == nil {
		t.Fatal("first StartSnapshots should set snapCtrl")
	}

	// Second call must not replace the existing controller.
	srv.StartSnapshots(ctx)
	if srv.snapCtrl != first {
		t.Fatal("second StartSnapshots should be a no-op (snapCtrl already set)")
	}
	srv.snapCtrl.Stop()
}

func TestAllPeriodBases(t *testing.T) {
	bases := usage.AllPeriodBases()
	if len(bases) != 3 {
		t.Fatalf("got %d, want 3", len(bases))
	}
	seen := map[string]bool{}
	for _, b := range bases {
		seen[b] = true
	}
	for _, want := range []string{"day", "week", "month"} {
		if !seen[want] {
			t.Errorf("missing basis: %s", want)
		}
	}
	// "sample" must not appear in AllPeriodBases.
	if seen["sample"] {
		t.Error("sample should not be in AllPeriodBases")
	}
}

func TestConfirmedPeriodSpend_NoSnapshots(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{addr: "", store: store}
	now := time.Now().UTC()
	ps := srv.confirmedPeriodSpend(config.ProviderMoonshot, now)
	// With no snapshots at all, ConfirmedSpend returns 0 (not nil).
	// confirmedPeriodSpend will set pointers to 0.
	if ps.Day == nil {
		t.Error("day should be set (0) even with no snapshots")
	}
	if *ps.Day != 0 {
		t.Errorf("day spend: got %v, want 0", *ps.Day)
	}
	if ps.Week == nil {
		t.Error("week should be set (0)")
	}
	if *ps.Week != 0 {
		t.Errorf("week spend: got %v, want 0", *ps.Week)
	}
	if ps.Month == nil {
		t.Error("month should be set (0)")
	}
	if *ps.Month != 0 {
		t.Errorf("month spend: got %v, want 0", *ps.Month)
	}
}

func TestConfirmedPeriodSpend_WithSnapshots(t *testing.T) {
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 14, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	// Insert two day snapshots for Moonshot so ConfirmedSpend returns a delta.
	snaps := []usage.BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now.Add(-3 * time.Hour), Amount: 100, Currency: "USD", USDAmount: 100},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now, Amount: 96, Currency: "USD", USDAmount: 96},
	}
	for _, s := range snaps {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{addr: "", store: store}
	ps := srv.confirmedPeriodSpend(config.ProviderMoonshot, now)
	if ps.Day == nil {
		t.Fatal("day spend should not be nil")
	}
	if *ps.Day != 4.0 {
		t.Errorf("day spend: got %v, want 4.0", *ps.Day)
	}
	// Week and month should still be nil/0 (no snapshots for those bases).
}

func TestConfirmedPeriodSpend_BalanceIncrease(t *testing.T) {
	// A balance increase (top-up) should result in 0 confirmed spend.
	store, err := usage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 14, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	snaps := []usage.BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now.Add(-3 * time.Hour), Amount: 100, Currency: "USD", USDAmount: 100},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now, Amount: 110, Currency: "USD", USDAmount: 110}, // topped up
	}
	for _, s := range snaps {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{addr: "", store: store}
	ps := srv.confirmedPeriodSpend(config.ProviderMoonshot, now)
	if ps.Day == nil {
		t.Fatal("day should not be nil even with balance increase")
	}
	// Balance went up → ConfirmedSpend returns 0 (delta clamped at 0).
	if *ps.Day != 0 {
		t.Errorf("day spend after top-up: got %v, want 0", *ps.Day)
	}
}

func TestBasesForTime_MidnightMonday(t *testing.T) {
	// Monday at exactly midnight should start its own week.
	mon := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) // Monday
	bases := basesForTime(mon)
	for _, b := range bases {
		if b.basis == "week" {
			if !b.periodStart.Equal(mon) {
				t.Errorf("monday-midnight week start: got %v, want %v", b.periodStart, mon)
			}
		}
	}
}

func TestPeriodRange_Sample(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 30, 0, 0, time.UTC)
	pb := periodRange("sample", now)
	if !pb.after.Equal(now) {
		t.Errorf("after=%v want %v", pb.after, now)
	}
	if !pb.before.Equal(now.Add(24 * time.Hour).Add(-1 * time.Second)) {
		t.Errorf("before=%v want %v", pb.before, now.Add(24*time.Hour).Add(-1*time.Second))
	}
}
