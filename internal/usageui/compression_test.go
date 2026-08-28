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

func TestHandleCompressionStats(t *testing.T) {
	store := testStore(t)
	since := time.Now().UTC().Add(-time.Hour)
	if err := store.RecordCompressionRun(usage.CompressionRun{
		ToolResultsCompressed: 1,
		CharsBefore:           50_000,
		CharsAfter:            8_000,
		SummarizerCalls:       1,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := store.Record(usage.Event{
		SessionID:        usage.CompressorWorkerSession,
		Provider:         config.ProviderDeepSeek,
		Model:            "deepseek-v4-flash-vision-exp",
		PromptTokens:     1000,
		CompletionTokens: 200,
	})
	if err != nil {
		t.Fatal(err)
	}

	live := config.NewLiveSettings(t.TempDir(), config.AppSettings{ToolCompressionEnabled: true})
	srv := &Server{store: store, live: live}

	req := httptest.NewRequest(http.MethodGet, "/api/compression-stats?since="+since.Format(time.RFC3339), nil)
	w := httptest.NewRecorder()
	srv.handleCompressionStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var resp CompressionStatsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ToolCompressionEnabled {
		t.Fatal("expected compression enabled")
	}
	if resp.Stats.RunCount != 1 {
		t.Fatalf("run_count=%d", resp.Stats.RunCount)
	}
	if resp.Stats.CharsSaved != 42_000 {
		t.Fatalf("chars_saved=%d", resp.Stats.CharsSaved)
	}
}
