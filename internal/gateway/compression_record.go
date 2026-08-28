package gateway

import (
	"log/slog"

	"github.com/commoddity/discursive/internal/usage"
)

func (s *Server) recordCompressionRun(chatSessionID, requestID string, stats CompressionStats) {
	if s.store == nil || stats.CharsBefore == 0 {
		return
	}
	err := s.store.RecordCompressionRun(usage.CompressionRun{
		ChatSessionID:         chatSessionID,
		RequestID:             requestID,
		ToolResultsCompressed: stats.ToolResultsCompressed,
		CharsBefore:           stats.CharsBefore,
		CharsAfter:            stats.CharsAfter,
		SummarizerCalls:       stats.SummarizerCalls,
		CacheHits:             stats.CacheHits,
	})
	if err != nil {
		slog.Warn("compression_run_record_failed", "request_id", requestID, "err", err)
	}
}
