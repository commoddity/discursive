package usage

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestLocalDayStart(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		loc  *time.Location
		want time.Time
	}{
		{
			name: "UTC noon",
			in:   time.Date(2026, 8, 14, 12, 30, 45, 123, time.UTC),
			loc:  time.UTC,
			want: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "UTC+1 midnight rolled over calendar day",
			in:   time.Date(2026, 8, 13, 23, 30, 0, 0, time.UTC),
			loc:  time.FixedZone("UTC+1", 3600),
			want: time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("UTC+1", 3600)),
		},
		{
			name: "UTC+1 late evening stays same day",
			in:   time.Date(2026, 8, 14, 22, 10, 0, 0, time.UTC),
			loc:  time.FixedZone("UTC+1", 3600),
			want: time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("UTC+1", 3600)),
		},
		{
			name: "nil loc uses local zone",
			in:   time.Date(2026, 8, 14, 9, 0, 0, 0, time.Local),
			loc:  nil,
			want: time.Date(2026, 8, 14, 0, 0, 0, 0, time.Local),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocalDayStart(tt.in, tt.loc)
			if !got.Equal(tt.want) || got.Location().String() != tt.want.Location().String() {
				t.Fatalf("LocalDayStart(%v, %v) = %v, want %v", tt.in, tt.loc, got, tt.want)
			}
		})
	}
}

func TestComputeSpend(t *testing.T) {
	t0 := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)

	tests := []struct {
		name   string
		points []SpendPoint
		fx     float64
		want   []SpendDelta
	}{
		{
			name: "plain decreases",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
				{CapturedAt: t1, Amount: 48, Currency: "USD", USDAmount: 48},
				{CapturedAt: t2, Amount: 45, Currency: "USD", USDAmount: 45},
			},
			want: []SpendDelta{
				{At: t1, USD: 2},
				{At: t2, USD: 3},
			},
		},
		{
			name: "top-up mid-window ignored",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
				{CapturedAt: t1, Amount: 65, Currency: "USD", USDAmount: 65, ToppedUp: 15},
				{CapturedAt: t2, Amount: 60, Currency: "USD", USDAmount: 60, ToppedUp: 15},
			},
			want: []SpendDelta{
				{At: t1, USD: 0},
				{At: t2, USD: 5},
			},
		},
		{
			name: "deepseek top-up then spend (total-balance deltas only)",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "CNY", USDAmount: 7.14, ToppedUp: 10},
				{CapturedAt: t1, Amount: 60, Currency: "CNY", USDAmount: 7.86, ToppedUp: 20},
				{CapturedAt: t2, Amount: 53, Currency: "CNY", USDAmount: 7.57, ToppedUp: 20},
			},
			fx: 7.0,
			want: []SpendDelta{
				{At: t1, USD: 0},                   // top-up: total rose 50→60, ignored
				{At: t2, USD: (60.0 - 53.0) / 7.0}, // spend: total fell 60→53 (USD via fx=7.0)
			},
		},
		{
			name: "only increases",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
				{CapturedAt: t1, Amount: 60, Currency: "USD", USDAmount: 60},
				{CapturedAt: t2, Amount: 70, Currency: "USD", USDAmount: 70},
			},
			want: []SpendDelta{
				{At: t1, USD: 0},
				{At: t2, USD: 0},
			},
		},
		{
			name: "single point",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
			},
			want: []SpendDelta{},
		},
		{
			name: "duplicate timestamps",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
				{CapturedAt: t0, Amount: 50, Currency: "USD", USDAmount: 50},
				{CapturedAt: t1, Amount: 48, Currency: "USD", USDAmount: 48},
			},
			want: []SpendDelta{
				{At: t1, USD: 2},
			},
		},
		{
			name: "cny with fx",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 700, Currency: "CNY", USDAmount: 100},
				{CapturedAt: t1, Amount: 630, Currency: "CNY", USDAmount: 90},
			},
			fx: 7.0,
			want: []SpendDelta{
				{At: t1, USD: 10},
			},
		},
		{
			name: "cny without fx falls back to usd_amount",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 700, Currency: "CNY", USDAmount: 100},
				{CapturedAt: t1, Amount: 630, Currency: "CNY", USDAmount: 90},
			},
			fx: 0,
			want: []SpendDelta{
				{At: t1, USD: 10},
			},
		},
		{
			name: "credits skipped",
			points: []SpendPoint{
				{CapturedAt: t0, Amount: 100, Currency: "credits", USDAmount: 0},
				{CapturedAt: t1, Amount: 90, Currency: "credits", USDAmount: 0},
			},
			want: []SpendDelta{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeSpend(tt.points, tt.fx)
			if len(got) != len(tt.want) {
				t.Fatalf("ComputeSpend returned %d deltas, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				approxEqual(t, got[i].USD, tt.want[i].USD, 1e-9)
				if !got[i].At.Equal(tt.want[i].At) {
					t.Errorf("delta %d At = %v, want %v", i, got[i].At, tt.want[i].At)
				}
			}
		})
	}
}

func TestConfirmedSpendBetween(t *testing.T) {
	prov := config.Provider("deepseek")
	loc := time.Local
	today := LocalDayStart(time.Now(), loc)
	tomorrow := today.Add(24 * time.Hour)

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Older point before `today` (anchored by the -24h load), newer point
	// inside the window → pair must still count toward today's spend.
	older := today.Add(-2 * time.Hour)
	inside := today.Add(2 * time.Hour)
	beforeTomorrow := tomorrow.Add(-1 * time.Hour)

	snaps := []BalanceSnapshot{
		{Provider: prov, Basis: "sample", CapturedAt: older, Amount: 50, Currency: "CNY", USDAmount: 7.14},
		{Provider: prov, Basis: "sample", CapturedAt: inside, Amount: 43, Currency: "CNY", USDAmount: 6.14},
		{Provider: prov, Basis: "sample", CapturedAt: beforeTomorrow, Amount: 43, Currency: "CNY", USDAmount: 6.14},
	}
	for _, snap := range snaps {
		if err := store.InsertBalanceSnapshot(snap); err != nil {
			t.Fatalf("InsertBalanceSnapshot: %v", err)
		}
	}

	got, err := store.ConfirmedSpendBetween(prov, today, tomorrow, 7.0)
	if err != nil {
		t.Fatalf("ConfirmedSpendBetween: %v", err)
	}
	// Pair(older, inside): (50-43)/7 = 1.0 inside window.
	// Pair(inside, beforeTomorrow): no decrease → 0, also inside window.
	want := 1.0
	approxEqual(t, got, want, 1e-9)
}

func approxEqual(t *testing.T, got, want, tol float64) {
	t.Helper()
	if diff := got - want; diff < -tol || diff > tol {
		t.Errorf("value = %v, want %v (tol %v)", got, want, tol)
	}
}
