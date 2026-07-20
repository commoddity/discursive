package usage

import (
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
					ID: "e3", SessionID: "sess-b", Provider: config.ProviderMoonshot, Model: "kimi-k2.7-code",
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
