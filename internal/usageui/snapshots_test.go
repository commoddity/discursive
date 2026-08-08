package usageui

import (
	"encoding/json"
	"net/http/httptest"
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
