package usage

import (
	"database/sql"
	"fmt"
	"time"
)

// CompressorWorkerSession is the fixed session id for tool-compression summarizer calls.
const CompressorWorkerSession = "compressor-worker"

// CompressionRun records one chat request where tool-result compression ran.
type CompressionRun struct {
	ID                    string
	Timestamp             time.Time
	ChatSessionID         string
	RequestID             string
	ToolResultsCompressed int
	CharsBefore           int
	CharsAfter            int
	SummarizerCalls       int
	CacheHits             int
}

// CompressionStatsSummary aggregates compression activity and summarizer worker cost.
type CompressionStatsSummary struct {
	RunCount              int     `json:"run_count"`
	ToolResultsCompressed int     `json:"tool_results_compressed"`
	CharsBefore           int     `json:"chars_before"`
	CharsAfter            int     `json:"chars_after"`
	CharsSaved            int     `json:"chars_saved"`
	SummarizerCalls       int     `json:"summarizer_calls"`
	CacheHits             int     `json:"cache_hits"`
	SummarizerCostUSD     float64 `json:"summarizer_cost_usd"`
	SummarizerTokensIn    uint64  `json:"summarizer_tokens_in"`
	SummarizerTokensOut   uint64  `json:"summarizer_tokens_out"`
	SummarizerRequests    uint64  `json:"summarizer_requests"`
}

// RecordCompressionRun persists one compression batch outcome.
func (s *Store) RecordCompressionRun(run CompressionRun) error {
	if run.Timestamp.IsZero() {
		run.Timestamp = time.Now().UTC()
	}
	if run.ID == "" {
		run.ID = newEventID()
	}
	_, err := s.db.Exec(
		`INSERT INTO compression_runs (
		 id, timestamp, chat_session_id, request_id,
		 tool_results_compressed, chars_before, chars_after,
		 summarizer_calls, cache_hits)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.Timestamp.Format(time.RFC3339Nano),
		run.ChatSessionID,
		run.RequestID,
		run.ToolResultsCompressed,
		run.CharsBefore,
		run.CharsAfter,
		run.SummarizerCalls,
		run.CacheHits,
	)
	if err != nil {
		return fmt.Errorf("insert compression run: %w", err)
	}
	return nil
}

// QueryCompressionStatsSince returns compression-run aggregates and summarizer-worker
// usage cost since the given time.
func (s *Store) QueryCompressionStatsSince(since time.Time) (CompressionStatsSummary, error) {
	var out CompressionStatsSummary
	var charsBefore, charsAfter sql.NullInt64

	err := s.db.QueryRow(
		`SELECT COUNT(*),
		 COALESCE(SUM(tool_results_compressed), 0),
		 COALESCE(SUM(chars_before), 0),
		 COALESCE(SUM(chars_after), 0),
		 COALESCE(SUM(summarizer_calls), 0),
		 COALESCE(SUM(cache_hits), 0)
		 FROM compression_runs WHERE timestamp >= ?`,
		since.UTC().Format(time.RFC3339),
	).Scan(
		&out.RunCount,
		&out.ToolResultsCompressed,
		&charsBefore,
		&charsAfter,
		&out.SummarizerCalls,
		&out.CacheHits,
	)
	if err != nil {
		return CompressionStatsSummary{}, fmt.Errorf("query compression runs: %w", err)
	}
	if charsBefore.Valid {
		out.CharsBefore = int(charsBefore.Int64)
	}
	if charsAfter.Valid {
		out.CharsAfter = int(charsAfter.Int64)
	}
	if out.CharsBefore > out.CharsAfter {
		out.CharsSaved = out.CharsBefore - out.CharsAfter
	}

	err = s.db.QueryRow(
		`SELECT COUNT(*),
		 COALESCE(SUM(prompt_tokens), 0),
		 COALESCE(SUM(completion_tokens), 0),
		 COALESCE(SUM(est_usd), 0)
		 FROM events WHERE session_id = ? AND timestamp >= ?`,
		CompressorWorkerSession,
		since.UTC().Format(time.RFC3339),
	).Scan(
		&out.SummarizerRequests,
		&out.SummarizerTokensIn,
		&out.SummarizerTokensOut,
		&out.SummarizerCostUSD,
	)
	if err != nil {
		return CompressionStatsSummary{}, fmt.Errorf("query compressor worker usage: %w", err)
	}
	out.SummarizerCostUSD = RoundUSD(out.SummarizerCostUSD)
	return out, nil
}
