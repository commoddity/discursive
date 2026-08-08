package usage

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestQueryByModel(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 1_000_000, Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		CompletionTokens: 500_000, Timestamp: time.Date(2026, 7, 15, 12, 1, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 2_000_000, CompletionTokens: 1_000_000,
		Timestamp: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
	})

	models, err := store.QueryByModel()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 2 {
		t.Fatalf("expected at least 2 models, got %d", len(models))
	}

	// DeepSeek model should come first (higher est_usd: 0.14+0.28+0.28=0.70 vs 3.00+7.50=10.50)
	// Actually kimi-k3 has 1M input (3.00) + 500k output (7.50) = 10.50 > deepseek 0.14+0.28+0.28=0.70
	for _, m := range models {
		if m.RequestCount == 0 {
			t.Errorf("model %s has 0 requests", m.Model)
		}
		if m.EstUSD <= 0 {
			t.Errorf("model %s has zero est_usd", m.Model)
		}
	}
}

func TestQueryByProvider(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 1_000_000, Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 2_000_000, Timestamp: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
	})

	providers, err := store.QueryByProvider()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	for _, p := range providers {
		if p.RequestCount == 0 {
			t.Errorf("provider %s has 0 requests", p.Provider)
		}
	}
}

func TestQuerySessions(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.Record(Event{
		SessionID: "sess-one", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 100, Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "sess-two", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 200, Timestamp: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
	})

	sessions, err := store.QuerySessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	for _, s := range sessions {
		if s.RequestCount == 0 {
			t.Errorf("session %s has 0 requests", s.SessionID)
		}
		if s.SessionID == "" {
			t.Error("session has empty ID")
		}
	}
}

func TestQueryMonthToDate(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	// Current month event.
	now := time.Now().UTC()
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 500_000, Timestamp: now,
	})

	ds, err := store.QueryMonthToDate()
	if err != nil {
		t.Fatal(err)
	}
	if ds.RequestCount < 1 {
		t.Fatalf("expected at least 1 request for MTD, got %d", ds.RequestCount)
	}
}

func TestQueryEmptyReturns(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("by_model_empty", func(t *testing.T) {
		models, err := store.QueryByModel()
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 0 {
			t.Fatalf("expected empty, got %d", len(models))
		}
	})

	t.Run("by_provider_empty", func(t *testing.T) {
		providers, err := store.QueryByProvider()
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != 0 {
			t.Fatalf("expected empty, got %d", len(providers))
		}
	})

	t.Run("sessions_empty", func(t *testing.T) {
		sessions, err := store.QuerySessions()
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 0 {
			t.Fatalf("expected empty, got %d", len(sessions))
		}
	})

	t.Run("mtd_empty", func(t *testing.T) {
		ds, err := store.QueryMonthToDate()
		if err != nil {
			t.Fatal(err)
		}
		if ds.RequestCount != 0 {
			t.Fatalf("expected 0, got %d", ds.RequestCount)
		}
	})

	t.Run("daily_empty", func(t *testing.T) {
		ds, err := store.QueryDailyTotals("2026-01-01")
		if err != nil {
			t.Fatal(err)
		}
		if ds.RequestCount != 0 {
			t.Fatalf("expected 0, got %d", ds.RequestCount)
		}
	})
}

func TestAccumulateDailyEvent(t *testing.T) {
	existing := &ModelBreakdown{
		Model: "kimi-k3", Provider: string(config.ProviderMoonshot),
		RequestCount: 1, TokensIn: 100, TokensOut: 50,
		CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25,
	}

	tests := []struct {
		name       string
		ev         Event
		seedDS     *DailySummary
		seedModels map[string]ModelBreakdown
		wantDS     DailySummary
		wantModels map[string]ModelBreakdown
	}{
		{
			name: "first event into empty summary",
			ev: Event{Model: "kimi-k3", Provider: config.ProviderMoonshot,
				PromptTokens: 100, CompletionTokens: 50,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25},
			wantDS: DailySummary{RequestCount: 1, TokensIn: 100, TokensOut: 50,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25},
			wantModels: map[string]ModelBreakdown{
				"kimi-k3": {Model: "kimi-k3", Provider: string(config.ProviderMoonshot),
					RequestCount: 1, TokensIn: 100, TokensOut: 50,
					CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25},
			},
		},
		{
			name: "second event into existing model",
			ev: Event{Model: "kimi-k3", Provider: config.ProviderMoonshot,
				PromptTokens: 50, CompletionTokens: 25, EstUSD: 0.75},
			seedDS: &DailySummary{RequestCount: 1, TokensIn: 100, TokensOut: 50,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25},
			seedModels: map[string]ModelBreakdown{"kimi-k3": *existing},
			wantDS: DailySummary{RequestCount: 2, TokensIn: 150, TokensOut: 75,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 2.0},
			wantModels: map[string]ModelBreakdown{
				"kimi-k3": {Model: "kimi-k3", Provider: string(config.ProviderMoonshot),
					RequestCount: 2, TokensIn: 150, TokensOut: 75,
					CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 2.0},
			},
		},
		{
			name: "event starts a distinct model breakdown",
			ev: Event{Model: "deepseek-v4-flash", Provider: config.ProviderDeepSeek,
				PromptTokens: 5, EstUSD: 0.5},
			seedDS: &DailySummary{RequestCount: 1, TokensIn: 100, TokensOut: 50,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.25},
			seedModels: map[string]ModelBreakdown{"kimi-k3": *existing},
			wantDS: DailySummary{RequestCount: 2, TokensIn: 105, TokensOut: 50,
				CacheHitTokens: 10, CacheMissTokens: 90, EstUSD: 1.75},
			wantModels: map[string]ModelBreakdown{
				"kimi-k3": *existing,
				"deepseek-v4-flash": {Model: "deepseek-v4-flash", Provider: string(config.ProviderDeepSeek),
					RequestCount: 1, TokensIn: 5, TokensOut: 0, EstUSD: 0.5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := DailySummary{}
			if tt.seedDS != nil {
				ds = *tt.seedDS
			}
			byModel := make(map[string]*ModelBreakdown)
			for k, v := range tt.seedModels {
				mb := v
				byModel[k] = &mb
			}

			accumulateDailyEvent(&ds, byModel, tt.ev)

			// ByModel is never set by the accumulator; normalize to nil and
			// compare deeply.
			ds.ByModel = nil
			if !reflect.DeepEqual(ds, tt.wantDS) {
				t.Fatalf("summary mismatch:\n got %+v\nwant %+v", ds, tt.wantDS)
			}
			got := make(map[string]ModelBreakdown, len(byModel))
			for k, mb := range byModel {
				got[k] = *mb
			}
			if !reflect.DeepEqual(got, tt.wantModels) {
				t.Fatalf("model breakdowns mismatch:\n got %+v\nwant %+v", got, tt.wantModels)
			}
		})
	}
}

func TestFinalizeDailySummary(t *testing.T) {
	tests := []struct {
		name   string
		ds     DailySummary
		models map[string]ModelBreakdown
		want   DailySummary
	}{
		{
			name: "rounds totals and folds model map",
			ds:   DailySummary{EstUSD: 0.123456},
			models: map[string]ModelBreakdown{
				"a": {Model: "a", EstUSD: 0.9876},
			},
			want: DailySummary{EstUSD: RoundUSD(0.123456),
				ByModel: []ModelBreakdown{{Model: "a", EstUSD: 0.988}}},
		},
		{
			name: "rounds floats to three decimals",
			ds:   DailySummary{EstUSD: 1.00005},
			models: map[string]ModelBreakdown{
				"b": {Model: "b", EstUSD: 2.34567},
				"c": {Model: "c", EstUSD: 0.0001},
			},
			want: DailySummary{EstUSD: RoundUSD(1.00005),
				ByModel: []ModelBreakdown{
					{Model: "b", EstUSD: RoundUSD(2.34567)},
					{Model: "c", EstUSD: 0.0},
				}},
		},
		{
			name: "no models yields empty ByModel",
			ds:   DailySummary{EstUSD: 2.5},
			want: DailySummary{EstUSD: 2.5, ByModel: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byModel := make(map[string]*ModelBreakdown, len(tt.models))
			for k, v := range tt.models {
				mb := v
				byModel[k] = &mb
			}
			got := finalizeDailySummary(tt.ds, byModel)

			if got.EstUSD != tt.want.EstUSD {
				t.Fatalf("EstUSD mismatch: got %v, want %v", got.EstUSD, tt.want.EstUSD)
			}
			gotModels := make(map[string]ModelBreakdown, len(got.ByModel))
			for _, mb := range got.ByModel {
				gotModels[mb.Model] = mb
			}
			wantModels := make(map[string]ModelBreakdown, len(tt.want.ByModel))
			for _, mb := range tt.want.ByModel {
				wantModels[mb.Model] = mb
			}
			if !reflect.DeepEqual(gotModels, wantModels) {
				t.Fatalf("ByModel mismatch:\n got %+v\nwant %+v", gotModels, wantModels)
			}
		})
	}
}

func TestScanRows(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 1000, CompletionTokens: 500, CacheHitTokens: 100,
		Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 2000, CompletionTokens: 1000, CacheHitTokens: 200,
		Timestamp: time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC),
	})
	_, _ = store.Record(Event{
		SessionID: "s2", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 3000, Timestamp: time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC),
	})

	t.Run("day summaries grouped by day", func(t *testing.T) {
		want := map[string]DailySummary{
			"2026-07-15": {Date: "2026-07-15", RequestCount: 2, TokensIn: 3000, TokensOut: 1500, CacheHitTokens: 300},
			"2026-07-16": {Date: "2026-07-16", RequestCount: 1, TokensIn: 3000, TokensOut: 0, CacheHitTokens: 0},
		}
		rows := queryRows(t, store, dayAggregateQuery+" GROUP BY day ORDER BY day ASC", nil)
		got, err := scanRows(rows, scanDaySummaryRow)
		if err != nil {
			t.Fatal(err)
		}
		assertDailyMap(t, got, want)
	})

	t.Run("model breakdowns grouped by provider,model", func(t *testing.T) {
		want := map[string]ModelBreakdown{
			"kimi-k3":           {Model: "kimi-k3", Provider: string(config.ProviderMoonshot), RequestCount: 2, TokensIn: 3000, TokensOut: 1500, CacheHitTokens: 300},
			"deepseek-v4-flash": {Model: "deepseek-v4-flash", Provider: string(config.ProviderDeepSeek), RequestCount: 1, TokensIn: 3000},
		}
		rows := queryRows(t, store, modelBreakdownQuery+" GROUP BY provider, model ORDER BY SUM(est_usd) DESC", nil)
		got, err := scanRows(rows, scanModelBreakdownRow)
		if err != nil {
			t.Fatal(err)
		}
		assertModelMap(t, got, want)
	})

	t.Run("provider breakdowns grouped by provider", func(t *testing.T) {
		want := map[string]ProviderBreakdown{
			string(config.ProviderMoonshot): {Provider: string(config.ProviderMoonshot), RequestCount: 2, TokensIn: 3000, TokensOut: 1500, CacheHitTokens: 300},
			string(config.ProviderDeepSeek): {Provider: string(config.ProviderDeepSeek), RequestCount: 1, TokensIn: 3000},
		}
		rows := queryRows(t, store, providerBreakdownQuery+" GROUP BY provider ORDER BY SUM(est_usd) DESC", nil)
		got, err := scanRows(rows, scanProviderBreakdownRow)
		if err != nil {
			t.Fatal(err)
		}
		assertProviderMap(t, got, want)
	})

	t.Run("session info grouped by session", func(t *testing.T) {
		want := map[string]SessionInfo{
			"s1": {SessionID: "s1", RequestCount: 2, TokensIn: 3000, TokensOut: 1500, FirstSeen: "2026-07-15T12:00:00Z", LastSeen: "2026-07-15T13:00:00Z"},
			"s2": {SessionID: "s2", RequestCount: 1, TokensIn: 3000, FirstSeen: "2026-07-16T09:00:00Z", LastSeen: "2026-07-16T09:00:00Z"},
		}
		rows := queryRows(t, store, sessionsQueryBase+" GROUP BY session_id ORDER BY MAX(timestamp) DESC", nil)
		got, err := scanRows(rows, scanSessionInfoRow)
		if err != nil {
			t.Fatal(err)
		}
		assertSessionMap(t, got, want)
	})

	t.Run("empty result yields nil slice", func(t *testing.T) {
		rows := queryRows(t, store, dayAggregateQuery+" WHERE date(timestamp) >= ? GROUP BY day ORDER BY day ASC", []any{"2099-01-01"})
		got, err := scanRows(rows, scanDaySummaryRow)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("expected nil slice, got %+v", got)
		}
	})
}

func TestQueryAndScan(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Record(Event{
		SessionID: "s1", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 1000, Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})

	tests := []struct {
		name  string
		probe func() (int, error)
		want  int
	}{
		{
			name: "queries and scans rows",
			probe: func() (int, error) {
				return queryAndScan(store, "test query", modelBreakdownQuery, nil, func(rows *sql.Rows) (int, error) {
					got, err := scanRows(rows, scanModelBreakdownRow)
					return len(got), err
				})
			},
			want: 1,
		},
		{
			name: "returns empty count for no rows",
			probe: func() (int, error) {
				return queryAndScan(store, "test query", modelBreakdownQuery+
					" WHERE timestamp >= ? GROUP BY provider, model", []any{"2099-01-01"},
					func(rows *sql.Rows) (int, error) {
						got, err := scanRows(rows, scanModelBreakdownRow)
						return len(got), err
					})
			},
			want: 0,
		},
		{
			name: "propagates query errors",
			probe: func() (int, error) {
				return queryAndScan(store, "test query", "SELECT * FROM no_such_table", nil,
					func(rows *sql.Rows) (int, error) { return 0, nil })
			},
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.probe()
			if tt.want == -1 {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// queryRows runs q on the store and returns the rows for scanning.
func queryRows(t *testing.T, store *Store, q string, args []any) *sql.Rows {
	t.Helper()
	rows, err := store.db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rows.Close() })
	return rows
}

func assertDailyMap(t *testing.T, got []DailySummary, want map[string]DailySummary) {
	t.Helper()
	gotMap := make(map[string]DailySummary, len(got))
	for _, d := range got {
		gotMap[d.Date] = d
	}
	if len(gotMap) != len(want) {
		t.Fatalf("day count mismatch: got %v, want %v", keysOf(gotMap), keysOf(want))
	}
	for k, w := range want {
		g, ok := gotMap[k]
		if !ok {
			t.Fatalf("missing day %q", k)
		}
		g.EstUSD, w.EstUSD = 0, 0
		g.CursorReference, w.CursorReference = CursorReference{}, CursorReference{}
		g.ByModel, w.ByModel = nil, nil
		if !reflect.DeepEqual(g, w) {
			t.Errorf("day %q mismatch:\n got %+v\nwant %+v", k, g, w)
		}
	}
}

func assertModelMap(t *testing.T, got []ModelBreakdown, want map[string]ModelBreakdown) {
	t.Helper()
	gotMap := make(map[string]ModelBreakdown, len(got))
	for _, m := range got {
		gotMap[m.Model] = m
	}
	for k, w := range want {
		g, ok := gotMap[k]
		if !ok {
			t.Fatalf("missing model %q", k)
		}
		g.EstUSD, w.EstUSD = 0, 0
		if g != w {
			t.Errorf("model %q mismatch:\n got %+v\nwant %+v", k, g, w)
		}
	}
}

func assertProviderMap(t *testing.T, got []ProviderBreakdown, want map[string]ProviderBreakdown) {
	t.Helper()
	gotMap := make(map[string]ProviderBreakdown, len(got))
	for _, p := range got {
		gotMap[p.Provider] = p
	}
	for k, w := range want {
		g, ok := gotMap[k]
		if !ok {
			t.Fatalf("missing provider %q", k)
		}
		g.EstUSD, w.EstUSD = 0, 0
		if g != w {
			t.Errorf("provider %q mismatch:\n got %+v\nwant %+v", k, g, w)
		}
	}
}

func assertSessionMap(t *testing.T, got []SessionInfo, want map[string]SessionInfo) {
	t.Helper()
	gotMap := make(map[string]SessionInfo, len(got))
	for _, si := range got {
		gotMap[si.SessionID] = si
	}
	for k, w := range want {
		g, ok := gotMap[k]
		if !ok {
			t.Fatalf("missing session %q", k)
		}
		g.EstUSD, w.EstUSD = 0, 0
		if g != w {
			t.Errorf("session %q mismatch:\n got %+v\nwant %+v", k, g, w)
		}
	}
}

func keysOf[K comparable](m map[K]DailySummary) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
