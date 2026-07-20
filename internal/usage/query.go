package usage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// DailySummary holds aggregated usage for a single day or session.
type DailySummary struct {
	Date            string           `json:"date"`
	RequestCount    uint64           `json:"request_count"`
	TokensIn        uint64           `json:"tokens_in"`
	TokensOut       uint64           `json:"tokens_out"`
	CacheHitTokens  uint64           `json:"cache_hit_tokens"`
	CacheMissTokens uint64           `json:"cache_miss_tokens"`
	EstUSD          float64          `json:"est_usd"`
	ByModel         []ModelBreakdown `json:"by_model"`
	CursorReference CursorReference  `json:"cursor_reference"`
	SessionID       string           `json:"session_id,omitempty"`
}

// ModelBreakdown summarizes usage for a specific model.
type ModelBreakdown struct {
	Model           string  `json:"model"`
	Provider        string  `json:"provider"`
	RequestCount    uint64  `json:"request_count"`
	TokensIn        uint64  `json:"tokens_in"`
	TokensOut       uint64  `json:"tokens_out"`
	CacheHitTokens  uint64  `json:"cache_hit_tokens"`
	CacheMissTokens uint64  `json:"cache_miss_tokens"`
	EstUSD          float64 `json:"est_usd"`
}

// CursorReference holds reference-only Cursor pricing (not billing).
type CursorReference struct {
	InputPer1M  float64 `json:"input_per_1m"`
	CachePer1M  float64 `json:"cache_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

// QueryDailyTotals returns a DailySummary for a specific date (YYYY-MM-DD).
func (s *Store) QueryDailyTotals(date string) (DailySummary, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms
		 FROM events WHERE date(timestamp) = ? ORDER BY timestamp ASC`, date)
	if err != nil {
		return DailySummary{}, fmt.Errorf("query daily totals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return buildDailySummary(rows, date, "")
}

// QueryLastNDays returns DailySummary entries for the last N calendar days.
func (s *Store) QueryLastNDays(n int) ([]DailySummary, error) {
	rows, err := s.db.Query(
		`SELECT date(timestamp) as day,
		 COUNT(*) as reqs,
		 COALESCE(SUM(prompt_tokens),0),
		 COALESCE(SUM(completion_tokens),0),
		 COALESCE(SUM(cache_hit_tokens),0),
		 COALESCE(SUM(cache_miss_tokens),0),
		 COALESCE(SUM(est_usd),0)
		 FROM events
		 GROUP BY day
		 ORDER BY day DESC
		 LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("query last n days: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DailySummary
	for rows.Next() {
		var day string
		var ds DailySummary
		if err := rows.Scan(&day, &ds.RequestCount, &ds.TokensIn,
			&ds.TokensOut, &ds.CacheHitTokens, &ds.CacheMissTokens, &ds.EstUSD); err != nil {
			return nil, fmt.Errorf("scan day: %w", err)
		}
		ds.Date = day
		ds.EstUSD = RoundUSD(ds.EstUSD)
		ds.CursorReference = cursorRef()
		out = append(out, ds)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// QuerySessionDetail returns a DailySummary for a specific session ID.
func (s *Store) QuerySessionDetail(sessionID string) (DailySummary, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms
		 FROM events WHERE session_id = ? ORDER BY timestamp ASC`, sessionID)
	if err != nil {
		return DailySummary{}, fmt.Errorf("query session: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return buildDailySummary(rows, "", sessionID)
}

// buildDailySummary scans event rows into a DailySummary.
func buildDailySummary(rows *sql.Rows, date, sessionID string) (DailySummary, error) {
	ds := DailySummary{
		Date:            date,
		SessionID:       sessionID,
		CursorReference: cursorRef(),
	}
	byModel := make(map[string]*ModelBreakdown)

	for rows.Next() {
		var ev Event
		var tsStr, provStr string
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &tsStr, &provStr, &ev.Model,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheHitTokens, &ev.CacheMissTokens,
			&ev.EstUSD, &ev.RequestID, &ev.LatencyMS,
		); err != nil {
			return DailySummary{}, fmt.Errorf("scan event: %w", err)
		}
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		ev.Provider = config.Provider(provStr)

		if sessionID != "" && ev.SessionID != sessionID {
			continue
		}

		ds.RequestCount++
		ds.TokensIn += ev.PromptTokens
		ds.TokensOut += ev.CompletionTokens
		ds.CacheHitTokens += ev.CacheHitTokens
		ds.CacheMissTokens += ev.CacheMissTokens
		ds.EstUSD += ev.EstUSD

		mb, ok := byModel[ev.Model]
		if !ok {
			mb = &ModelBreakdown{
				Model:    ev.Model,
				Provider: string(ev.Provider),
			}
			byModel[ev.Model] = mb
		}
		mb.RequestCount++
		mb.TokensIn += ev.PromptTokens
		mb.TokensOut += ev.CompletionTokens
		mb.CacheHitTokens += ev.CacheHitTokens
		mb.CacheMissTokens += ev.CacheMissTokens
		mb.EstUSD += ev.EstUSD
	}

	if err := rows.Err(); err != nil {
		return DailySummary{}, fmt.Errorf("rows: %w", err)
	}

	ds.EstUSD = RoundUSD(ds.EstUSD)
	for _, mb := range byModel {
		mb.EstUSD = RoundUSD(mb.EstUSD)
		ds.ByModel = append(ds.ByModel, *mb)
	}

	return ds, nil
}

func cursorRef() CursorReference {
	in, cache, out := CursorComparisonReference()
	return CursorReference{
		InputPer1M:  in,
		CachePer1M:  cache,
		OutputPer1M: out,
	}
}

// ProviderBreakdown summarizes usage for a single provider.
type ProviderBreakdown struct {
	Provider        string  `json:"provider"`
	RequestCount    uint64  `json:"request_count"`
	TokensIn        uint64  `json:"tokens_in"`
	TokensOut       uint64  `json:"tokens_out"`
	CacheHitTokens  uint64  `json:"cache_hit_tokens"`
	CacheMissTokens uint64  `json:"cache_miss_tokens"`
	EstUSD          float64 `json:"est_usd"`
}

// SessionInfo holds summary for a single session.
type SessionInfo struct {
	SessionID    string  `json:"session_id"`
	RequestCount uint64  `json:"request_count"`
	TokensIn     uint64  `json:"tokens_in"`
	TokensOut    uint64  `json:"tokens_out"`
	EstUSD       float64 `json:"est_usd"`
	FirstSeen    string  `json:"first_seen"`
	LastSeen     string  `json:"last_seen"`
}

// QueryMonthToDate returns a DailySummary for the current month (UTC).
func (s *Store) QueryMonthToDate() (DailySummary, error) {
	now := time.Now().UTC()
	start := now.Format("2006-01-01")
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms
		 FROM events WHERE date(timestamp) >= ? ORDER BY timestamp ASC`, start)
	if err != nil {
		return DailySummary{}, fmt.Errorf("query mtd: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return buildDailySummary(rows, start, "")
}

// QueryByModel returns all-time usage breakdown by model.
func (s *Store) QueryByModel() ([]ModelBreakdown, error) {
	rows, err := s.db.Query(
		`SELECT provider, model,
		 COUNT(*) as reqs,
		 COALESCE(SUM(prompt_tokens),0),
		 COALESCE(SUM(completion_tokens),0),
		 COALESCE(SUM(cache_hit_tokens),0),
		 COALESCE(SUM(cache_miss_tokens),0),
		 COALESCE(SUM(est_usd),0)
		 FROM events
		 GROUP BY provider, model
		 ORDER BY SUM(est_usd) DESC`)
	if err != nil {
		return nil, fmt.Errorf("query by model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ModelBreakdown
	for rows.Next() {
		var mb ModelBreakdown
		if err := rows.Scan(&mb.Provider, &mb.Model,
			&mb.RequestCount, &mb.TokensIn, &mb.TokensOut,
			&mb.CacheHitTokens, &mb.CacheMissTokens, &mb.EstUSD); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		mb.EstUSD = RoundUSD(mb.EstUSD)
		out = append(out, mb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// QueryByProvider returns all-time usage breakdown by provider.
func (s *Store) QueryByProvider() ([]ProviderBreakdown, error) {
	rows, err := s.db.Query(
		`SELECT provider,
		 COUNT(*) as reqs,
		 COALESCE(SUM(prompt_tokens),0),
		 COALESCE(SUM(completion_tokens),0),
		 COALESCE(SUM(cache_hit_tokens),0),
		 COALESCE(SUM(cache_miss_tokens),0),
		 COALESCE(SUM(est_usd),0)
		 FROM events
		 GROUP BY provider
		 ORDER BY SUM(est_usd) DESC`)
	if err != nil {
		return nil, fmt.Errorf("query by provider: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ProviderBreakdown
	for rows.Next() {
		var pb ProviderBreakdown
		if err := rows.Scan(&pb.Provider,
			&pb.RequestCount, &pb.TokensIn, &pb.TokensOut,
			&pb.CacheHitTokens, &pb.CacheMissTokens, &pb.EstUSD); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		pb.EstUSD = RoundUSD(pb.EstUSD)
		out = append(out, pb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// QuerySessions returns a list of all unique sessions with summary info.
func (s *Store) QuerySessions() ([]SessionInfo, error) {
	rows, err := s.db.Query(
		`SELECT session_id,
		 COUNT(*) as reqs,
		 COALESCE(SUM(prompt_tokens),0),
		 COALESCE(SUM(completion_tokens),0),
		 COALESCE(SUM(est_usd),0),
		 MIN(timestamp) as first_seen,
		 MAX(timestamp) as last_seen
		 FROM events
		 GROUP BY session_id
		 ORDER BY MAX(timestamp) DESC`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.SessionID,
			&si.RequestCount, &si.TokensIn, &si.TokensOut, &si.EstUSD,
			&si.FirstSeen, &si.LastSeen); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		si.EstUSD = RoundUSD(si.EstUSD)
		out = append(out, si)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
