package usage

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
	usagepkg "github.com/commoddity/discursive/internal/usage"
)

// estEventFor is a helper: an event used to seed estimated spend. The expected
// value is whatever EstimateUSD computed when stored, which we capture live so
// the test stays correct if pricing changes.
type estEventFor struct {
	ts       time.Time
	tokensIn uint64
	model    string
}

func TestConfirmedSpendReport(t *testing.T) {
	t.Setenv("TZ", "UTC")
	loc := time.UTC
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	type want struct {
		mtd, today float64
	}
	tests := []struct {
		name           string
		snapshotsFor   map[config.Provider][]usagepkg.BalanceSnapshot
		estEvents      map[config.Provider][]estEventFor
		want           map[config.Provider]want
		wantTotalMTD   float64
		wantEstMTD     float64
		wantTotalToday float64
	}{
		{
			name: "confirmed only",
			snapshotsFor: map[config.Provider][]usagepkg.BalanceSnapshot{
				config.ProviderMoonshot: {
					{Basis: "sample", CapturedAt: time.Date(2026, 7, 31, 23, 0, 0, 0, time.UTC), Amount: 100, Currency: "USD", USDAmount: 100},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC), Amount: 90, Currency: "USD", USDAmount: 90},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC), Amount: 85, Currency: "USD", USDAmount: 85},
				},
				config.ProviderDeepSeek: {
					{Basis: "sample", CapturedAt: time.Date(2026, 7, 31, 23, 0, 0, 0, time.UTC), Amount: 50, Currency: "USD", USDAmount: 50},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC), Amount: 50, Currency: "USD", USDAmount: 50},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), Amount: 47, Currency: "USD", USDAmount: 47},
				},
			},
			want: map[config.Provider]want{
				config.ProviderMoonshot: {mtd: 15, today: 5},
				config.ProviderDeepSeek: {mtd: 3, today: 3},
				config.ProviderZai:      {mtd: 0, today: 0},
				config.ProviderThaura:   {mtd: 0, today: 0},
			},
			wantTotalMTD:   18,
			wantEstMTD:     0,
			wantTotalToday: 8,
		},
		{
			name: "confirmed plus estimated",
			snapshotsFor: map[config.Provider][]usagepkg.BalanceSnapshot{
				config.ProviderMoonshot: {
					{Basis: "sample", CapturedAt: time.Date(2026, 7, 31, 23, 0, 0, 0, time.UTC), Amount: 100, Currency: "USD", USDAmount: 100},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC), Amount: 100, Currency: "USD", USDAmount: 100},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC), Amount: 90, Currency: "USD", USDAmount: 90},
				},
				config.ProviderDeepSeek: {
					{Basis: "sample", CapturedAt: time.Date(2026, 7, 31, 23, 0, 0, 0, time.UTC), Amount: 50, Currency: "USD", USDAmount: 50},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC), Amount: 50, Currency: "USD", USDAmount: 50},
					{Basis: "sample", CapturedAt: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), Amount: 48, Currency: "USD", USDAmount: 48},
				},
			},
			estEvents: map[config.Provider][]estEventFor{
				config.ProviderZai: {
					{ts: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), tokensIn: 1_000_000, model: "glm-5.2"},
					{ts: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), tokensIn: 0, model: "glm-5.2"},
				},
				config.ProviderThaura: {
					{ts: time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC), tokensIn: 1_000_000, model: "thaura"},
				},
			},
			want: map[config.Provider]want{
				config.ProviderMoonshot: {mtd: 10, today: 10},
				config.ProviderDeepSeek: {mtd: 2, today: 2},
			},
			wantTotalMTD:   12.5,
			wantEstMTD:     0.5,
			wantTotalToday: 12.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := usagepkg.NewStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			defer func() { _ = store.Close() }()

			for prov, snaps := range tt.snapshotsFor {
				for _, s := range snaps {
					s.Provider = prov
					if err := store.InsertBalanceSnapshot(s); err != nil {
						t.Fatalf("InsertBalanceSnapshot: %v", err)
					}
				}
			}

			// expect begins with the declared confirmed expectations; any estimated
			// expectations are derived from the exact EstUSD the store records.
			expect := map[config.Provider]want{}
			for prov, w := range tt.want {
				expect[prov] = w
			}
			for prov, evs := range tt.estEvents {
				var mtd, today float64
				for _, e := range evs {
					stored, err := store.Record(usagepkg.Event{
						SessionID:    "est",
						Timestamp:    e.ts,
						Provider:     prov,
						Model:        e.model,
						PromptTokens: e.tokensIn,
					})
					if err != nil {
						t.Fatalf("Record(%s): %v", prov, err)
					}
					mtd += stored.EstUSD
					if time.Date(e.ts.Year(), e.ts.Month(), e.ts.Day(), 0, 0, 0, 0, time.UTC).Equal(time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)) {
						today += stored.EstUSD
					}
				}
				expect[prov] = want{mtd: mtd, today: today}
			}

			rows, totalMTD, estMTD, totalToday, err := confirmedSpendReport(store, now, loc)
			if err != nil {
				t.Fatalf("confirmedSpendReport: %v", err)
			}
			if totalMTD != tt.wantTotalMTD {
				t.Errorf("totalMTD = %.2f, want %.2f", totalMTD, tt.wantTotalMTD)
			}
			if totalToday != tt.wantTotalToday {
				t.Errorf("totalToday = %.2f, want %.2f", totalToday, tt.wantTotalToday)
			}
			if estMTD != tt.wantEstMTD {
				t.Errorf("estMTD = %.2f, want %.2f", estMTD, tt.wantEstMTD)
			}

			for _, r := range rows {
				var prov config.Provider
				switch r.Provider {
				case "Moonshot":
					prov = config.ProviderMoonshot
				case "DeepSeek":
					prov = config.ProviderDeepSeek
				case "Z.AI":
					prov = config.ProviderZai
				case "Thaura":
					prov = config.ProviderThaura
				}
				w := expect[prov]
				if r.MTD != w.mtd {
					t.Errorf("%s MTD = %.2f, want %.2f", r.Provider, r.MTD, w.mtd)
				}
				if r.Today != w.today {
					t.Errorf("%s today = %.2f, want %.2f", r.Provider, r.Today, w.today)
				}
				if r.Basis != "confirmed" && r.Basis != "estimated" {
					t.Errorf("unexpected basis %q for %s", r.Basis, r.Provider)
				}
			}

			var sum float64
			for _, r := range rows {
				if r.Provider == "Z.AI" {
					continue // Z.AI excluded from headline totals
				}
				sum += r.MTD
			}
			if sum != totalMTD {
				t.Errorf("rows MTD sum %.2f != reported total %.2f", sum, totalMTD)
			}
		})
	}
}

func TestSumEstWindow(t *testing.T) {
	loc := time.UTC
	monthStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	rows := []usagepkg.ProviderEstBucket{
		{Bucket: "2026-07-20", Provider: string(config.ProviderZai), EstUSD: 9},
		{Bucket: "2026-08-05", Provider: string(config.ProviderZai), EstUSD: 2},
		{Bucket: "2026-08-14", Provider: string(config.ProviderZai), EstUSD: 1},
		{Bucket: "2026-08-15", Provider: string(config.ProviderZai), EstUSD: 4},
		{Bucket: "2026-08-05", Provider: string(config.ProviderThaura), EstUSD: 3},
		{Bucket: "2026-08-14", Provider: string(config.ProviderThaura), EstUSD: 5},
	}

	zaiMtd, zaiToday := sumEstWindow(rows, config.ProviderZai, monthStart, dayStart, loc)
	if zaiMtd != 3 || zaiToday != 1 {
		t.Errorf("zai mtd/today = %.0f/%.0f, want 3/1", zaiMtd, zaiToday)
	}
	thauraMtd, thauraToday := sumEstWindow(rows, config.ProviderThaura, monthStart, dayStart, loc)
	if thauraMtd != 8 || thauraToday != 5 {
		t.Errorf("thaura mtd/today = %.0f/%.0f, want 8/5", thauraMtd, thauraToday)
	}
}
