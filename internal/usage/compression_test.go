package usage

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestRecordAndQueryCompressionStats(t *testing.T) {
	store := testCompressionStore(t)

	since := time.Now().UTC().Add(-time.Hour)
	if err := store.RecordCompressionRun(CompressionRun{
		ChatSessionID:         "sess_chat",
		RequestID:             "req_1",
		ToolResultsCompressed: 2,
		CharsBefore:           100_000,
		CharsAfter:            12_000,
		SummarizerCalls:       2,
		CacheHits:             1,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.Record(Event{
		SessionID:        CompressorWorkerSession,
		Provider:         config.ProviderDeepSeek,
		Model:            "deepseek-v4-flash-vision-exp",
		PromptTokens:     5000,
		CompletionTokens: 800,
		RequestID:        "compressor",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.QueryCompressionStatsSince(since)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunCount != 1 {
		t.Fatalf("run_count=%d want 1", got.RunCount)
	}
	if got.ToolResultsCompressed != 2 {
		t.Fatalf("tool_results=%d want 2", got.ToolResultsCompressed)
	}
	if got.CharsSaved != 88_000 {
		t.Fatalf("chars_saved=%d want 88000", got.CharsSaved)
	}
	if got.SummarizerCalls != 2 || got.CacheHits != 1 {
		t.Fatalf("calls=%d cache_hits=%d", got.SummarizerCalls, got.CacheHits)
	}
	if got.SummarizerCostUSD <= 0 {
		t.Fatalf("summarizer_cost_usd=%v want >0", got.SummarizerCostUSD)
	}
	if got.SummarizerTokensIn != 5000 || got.SummarizerTokensOut != 800 {
		t.Fatalf("tokens in/out=%d/%d", got.SummarizerTokensIn, got.SummarizerTokensOut)
	}
}

func testCompressionStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
