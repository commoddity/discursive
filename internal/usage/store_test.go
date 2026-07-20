package usage

import (
	"os"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestStoreRecordAndSessionSummary(t *testing.T) {
	tests := []struct {
		name       string
		events     []Event
		sessionID  string
		wantReqs   uint64
		wantUSD    float64
		wantModels int
	}{
		{
			name: "moonshot_and_deepseek_same_session",
			events: []Event{
				{
					ID: "e1", SessionID: "sess-a", Provider: config.ProviderMoonshot, Model: "kimi-k3",
					PromptTokens: 1_000_000, RequestID: "r1",
				},
				{
					ID: "e2", SessionID: "sess-a", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
					PromptTokens: 1_000_000, CompletionTokens: 1_000_000, RequestID: "r2",
				},
				{
					ID: "e3", SessionID: "sess-b", Provider: config.ProviderMoonshot, Model: "kimi-k2.6",
					PromptTokens: 100_000, RequestID: "r3",
				},
			},
			sessionID:  "sess-a",
			wantReqs:   2,
			wantUSD:    3.00 + 0.14 + 0.28,
			wantModels: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range tt.events {
				ev.Timestamp = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
				if _, err := store.Record(ev); err != nil {
					t.Fatal(err)
				}
			}

			loaded, err := store.LoadEvents()
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded) != len(tt.events) {
				t.Fatalf("loaded %d want %d", len(loaded), len(tt.events))
			}
			if loaded[0].Provider != tt.events[0].Provider {
				t.Fatalf("provider not persisted: %q", loaded[0].Provider)
			}

			sum, err := store.SessionSummary(tt.sessionID)
			if err != nil {
				t.Fatal(err)
			}
			if sum.RequestCount != tt.wantReqs {
				t.Fatalf("requests %d want %d", sum.RequestCount, tt.wantReqs)
			}
			if len(sum.ByModel) != tt.wantModels {
				t.Fatalf("models %d want %d", len(sum.ByModel), tt.wantModels)
			}
			const eps = 1e-6
			if diff := sum.EstUSD - tt.wantUSD; diff < -eps || diff > eps {
				t.Fatalf("est_usd %v want %v", sum.EstUSD, tt.wantUSD)
			}
		})
	}
}

func TestStoreAutoID(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	ev, err := store.Record(Event{
		SessionID: "sess-x", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	if len(ev.ID) <= 4 {
		t.Fatalf("ID too short: %q", ev.ID)
	}
	if ev.ID[:4] != "evt_" {
		t.Fatalf("ID missing evt_ prefix: %q", ev.ID)
	}
}

func TestStoreEmptyLoad(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected empty, got %d events", len(events))
	}
}

func TestStoreSchemaExists(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	var tableName string
	err = store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='events'").Scan(&tableName)
	if err != nil {
		t.Fatal(err)
	}
	if tableName != "events" {
		t.Fatalf("expected events table, got %q", tableName)
	}

	// Verify indexes.
	for _, wantIdx := range []string{"idx_events_timestamp", "idx_events_session", "idx_events_prov_model"} {
		var idxName string
		err = store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", wantIdx).Scan(&idxName)
		if err != nil {
			t.Fatalf("index %q not found: %v", wantIdx, err)
		}
	}
}

func TestStoreRecordLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 7, 20, 15, 30, 0, 0, time.UTC)
	ev, err := store.Record(Event{
		SessionID:        "sess-1",
		Timestamp:        ts,
		Provider:         config.ProviderMoonshot,
		Model:            "kimi-k3",
		PromptTokens:     1000,
		CompletionTokens: 500,
		CacheHitTokens:   200,
		CacheMissTokens:  300,
		RequestID:        "req-abc",
		LatencyMS:        1234,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Re-open to test persistence.
	_ = store.Close()
	store2, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	events, err := store2.LoadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.ID != ev.ID {
		t.Fatalf("ID mismatch: got %q want %q", got.ID, ev.ID)
	}
	if got.SessionID != "sess-1" {
		t.Fatalf("SessionID: got %q want sess-1", got.SessionID)
	}
	if got.Provider != config.ProviderMoonshot {
		t.Fatalf("Provider: got %q", got.Provider)
	}
	if got.Model != "kimi-k3" {
		t.Fatalf("Model: got %q", got.Model)
	}
	if got.PromptTokens != 1000 {
		t.Fatalf("PromptTokens: got %d", got.PromptTokens)
	}
	if got.CompletionTokens != 500 {
		t.Fatalf("CompletionTokens: got %d", got.CompletionTokens)
	}
	if got.CacheHitTokens != 200 {
		t.Fatalf("CacheHitTokens: got %d", got.CacheHitTokens)
	}
	if got.CacheMissTokens != 300 {
		t.Fatalf("CacheMissTokens: got %d", got.CacheMissTokens)
	}
	if got.RequestID != "req-abc" {
		t.Fatalf("RequestID: got %q", got.RequestID)
	}
	if got.LatencyMS != 1234 {
		t.Fatalf("LatencyMS: got %d", got.LatencyMS)
	}
}

func TestStoreEmptySessionSummary(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	sum, err := store.SessionSummary("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if sum.RequestCount != 0 {
		t.Fatalf("expected 0 requests, got %d", sum.RequestCount)
	}
	if sum.EstUSD != 0 {
		t.Fatalf("expected 0 USD, got %v", sum.EstUSD)
	}
}

func TestStoreDBFileCreated(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	dbPath := root + "/usage/usage.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected usage.db at %s", dbPath)
	}
}
