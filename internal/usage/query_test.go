package usage

import (
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
