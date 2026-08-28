package usageui

import (
	"net/http"
	"time"

	"github.com/commoddity/discursive/internal/usage"
)

// CompressionStatsResponse is the /api/compression-stats payload.
type CompressionStatsResponse struct {
	ToolCompressionEnabled bool                          `json:"tool_compression_enabled"`
	Since                  string                        `json:"since"`
	Stats                  usage.CompressionStatsSummary `json:"stats"`
}

func (s *Server) handleCompressionStats(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store not available", http.StatusServiceUnavailable)
		return
	}
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	if since.IsZero() {
		since = usage.LocalDayStart(time.Now().AddDate(0, 0, -13), time.Local)
	}

	stats, err := s.store.QueryCompressionStatsSince(since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enabled := false
	if s.live != nil {
		enabled = s.live.ToolCompressionEnabled()
	}

	writeJSON(w, CompressionStatsResponse{
		ToolCompressionEnabled: enabled,
		Since:                  since.UTC().Format(time.RFC3339),
		Stats:                  stats,
	})
}
