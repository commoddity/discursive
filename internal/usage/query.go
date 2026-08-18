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

// BucketModelBreakdown holds per-model spend within a single time bucket.
type BucketModelBreakdown struct {
	Bucket          string  `json:"bucket"`
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	RequestCount    uint64  `json:"request_count"`
	TokensIn        uint64  `json:"tokens_in"`
	TokensOut       uint64  `json:"tokens_out"`
	CacheHitTokens  uint64  `json:"cache_hit_tokens"`
	CacheMissTokens uint64  `json:"cache_miss_tokens"`
	EstUSD          float64 `json:"est_usd"`
	// ZaiCredits is the coding-plan credit consumption for this bucket,
	// computed per event with peak/off-peak rates. Zero for non-Z.AI rows.
	ZaiCredits float64 `json:"zai_credits,omitempty"`
}

// Shared SQL prefixes. Each query method appends its WHERE / GROUP BY /
// ORDER BY clause. The full SQL emitted (column selection, GROUP BY, ORDER BY)
// is the contract for upstream callers and must not change.
const (
	// eventDetailQuery selects the raw event columns for single-summary queries.
	eventDetailQuery = `SELECT id, session_id, timestamp, provider, model,
	 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
	 est_usd, request_id, latency_ms
	 FROM events`
	// dayAggregateQuery groups rows by calendar day.
	dayAggregateQuery = `SELECT date(timestamp) as day,
	 COUNT(*) as reqs,
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(cache_hit_tokens),0),
	 COALESCE(SUM(cache_miss_tokens),0),
	 COALESCE(SUM(est_usd),0)
	 FROM events`
	// modelBreakdownQuery groups rows by provider and model.
	modelBreakdownQuery = `SELECT provider, model,
	 COUNT(*) as reqs,
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(cache_hit_tokens),0),
	 COALESCE(SUM(cache_miss_tokens),0),
	 COALESCE(SUM(est_usd),0)
	 FROM events`
	// providerBreakdownQuery groups rows by provider.
	providerBreakdownQuery = `SELECT provider,
	 COUNT(*) as reqs,
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(cache_hit_tokens),0),
	 COALESCE(SUM(cache_miss_tokens),0),
	 COALESCE(SUM(est_usd),0)
	 FROM events`
	// sessionsQueryBase aggregates per-session totals; QuerySessionsSince appends
	// an optional WHERE clause and the GROUP BY / ORDER BY.
	sessionsQueryBase = `SELECT session_id,
	 COUNT(*) as reqs,
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(est_usd),0),
	 MIN(timestamp) as first_seen,
	 MAX(timestamp) as last_seen
	 FROM events`
)

// QueryDailyTotals returns a DailySummary for a specific date (YYYY-MM-DD).
func (s *Store) QueryDailyTotals(date string) (DailySummary, error) {
	return queryAndScan(s, "query daily totals",
		eventDetailQuery+` WHERE date(timestamp) = ? ORDER BY timestamp ASC`, []any{date},
		func(rows *sql.Rows) (DailySummary, error) {
			return buildDailySummary(rows, date, "")
		})
}

// QueryLastNDays returns DailySummary entries for the last N calendar days.
func (s *Store) QueryLastNDays(n int) ([]DailySummary, error) {
	return queryAndScan(s, "query last n days",
		dayAggregateQuery+` GROUP BY day ORDER BY day DESC LIMIT ?`, []any{n},
		func(rows *sql.Rows) ([]DailySummary, error) {
			return scanRows(rows, scanDaySummaryRow)
		})
}

// QueryByDaySince returns DailySummary entries grouped by day since a given time.
// When window is sub-day (e.g. 1h/3h/12h), groups by a configurable bucket.
// bucketMinutes: 0 means group by day (date). >0 means group by floor(timestamp / bucket).
func (s *Store) QueryByDaySince(since time.Time, bucketMinutes int) ([]DailySummary, error) {
	if bucketMinutes > 0 {
		bucketSecs := bucketMinutes * 60
		q := `SELECT strftime('%Y-%m-%dT%H:%M:00',
		 datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch')) as bucket,
		 COUNT(*) as reqs,
		 COALESCE(SUM(prompt_tokens),0),
		 COALESCE(SUM(completion_tokens),0),
		 COALESCE(SUM(cache_hit_tokens),0),
		 COALESCE(SUM(cache_miss_tokens),0),
		 COALESCE(SUM(est_usd),0)
		 FROM events WHERE timestamp >= ?
		 GROUP BY bucket
		 ORDER BY bucket ASC`
		return queryAndScan(s, "query by day since", q,
			[]any{bucketSecs, bucketSecs, since.UTC().Format(time.RFC3339)},
			func(rows *sql.Rows) ([]DailySummary, error) {
				return scanRows(rows, scanDaySummaryRow)
			})
	}
	return queryAndScan(s, "query by day since",
		dayAggregateQuery+` WHERE timestamp >= ? GROUP BY day ORDER BY day ASC`,
		[]any{since.UTC().Format(time.RFC3339)},
		func(rows *sql.Rows) ([]DailySummary, error) {
			return scanRows(rows, scanDaySummaryRow)
		})
}

// queryAndScan runs q with args, closes the result rows, and hands them to build.
// build owns scanning/consuming rows; any failure there is returned as-is.
func queryAndScan[T any](s *Store, label, q string, args []any, build func(*sql.Rows) (T, error)) (T, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = rows.Close() }()
	return build(rows)
}

// scanRows walks each row, using perRow to scan a single typed value, and returns
// the collected slice. It surfaces rows.Err() after iteration.
func scanRows[T any](rows *sql.Rows, perRow func(*sql.Rows) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		item, err := perRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// scanDaySummaryRow scans one row of dayAggregateQuery (or the bucket variant).
func scanDaySummaryRow(rows *sql.Rows) (DailySummary, error) {
	var ds DailySummary
	if err := rows.Scan(&ds.Date, &ds.RequestCount, &ds.TokensIn,
		&ds.TokensOut, &ds.CacheHitTokens, &ds.CacheMissTokens, &ds.EstUSD); err != nil {
		return DailySummary{}, fmt.Errorf("scan day/bucket: %w", err)
	}
	ds.EstUSD = RoundUSD(ds.EstUSD)
	ds.CursorReference = cursorRef()
	return ds, nil
}

// QueryByDayModelSince returns per-model breakdown per bucket since a given time.
// This powers the Spend by Period chart split by model instead of cache hit/miss.
func (s *Store) QueryByDayModelSince(since time.Time, bucketMinutes int) ([]BucketModelBreakdown, error) {
	var bucketExpr string
	var args []any
	if bucketMinutes > 0 {
		bucketSecs := bucketMinutes * 60
		bucketExpr = `strftime('%Y-%m-%dT%H:%M:00',
		 datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch'))`
		args = []any{bucketSecs, bucketSecs}
	} else {
		bucketExpr = "date(timestamp)"
	}
	args = append(args, since.UTC().Format(time.RFC3339))
	q := `SELECT ` + bucketExpr + ` as bucket,
	 provider, model,
	 COUNT(*) as reqs,
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(cache_hit_tokens),0),
	 COALESCE(SUM(cache_miss_tokens),0),
	 COALESCE(SUM(est_usd),0)
	 FROM events WHERE timestamp >= ?
	 GROUP BY bucket, provider, model
	 ORDER BY bucket ASC, SUM(est_usd) DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query by day model since: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []BucketModelBreakdown
	for rows.Next() {
		var bm BucketModelBreakdown
		if err := rows.Scan(&bm.Bucket, &bm.Provider, &bm.Model,
			&bm.RequestCount, &bm.TokensIn, &bm.TokensOut,
			&bm.CacheHitTokens, &bm.CacheMissTokens, &bm.EstUSD); err != nil {
			return nil, fmt.Errorf("scan bucket model: %w", err)
		}
		bm.EstUSD = RoundUSD(bm.EstUSD)
		out = append(out, bm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if err := s.annotateZaiCredits(out, since, bucketMinutes); err != nil {
		return nil, err
	}
	return out, nil
}

// annotateZaiCredits fills ZaiCredits on zai rows by replaying the raw events
// through the per-event credit calculation (which needs each event timestamp
// for the peak/off-peak rate). Best-effort per row: unpriceable models keep 0.
func (s *Store) annotateZaiCredits(out []BucketModelBreakdown, since time.Time, bucketMinutes int) error {
	type key struct{ bucket, model string }
	idx := make(map[key]int, len(out))
	for i, bm := range out {
		if bm.Provider == string(config.ProviderZai) {
			idx[key{bm.Bucket, bm.Model}] = i
		}
	}
	if len(idx) == 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT timestamp, model, prompt_tokens, completion_tokens,
	 cache_hit_tokens, cache_miss_tokens FROM events
	 WHERE provider = ? AND timestamp >= ?`,
		string(config.ProviderZai), since.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("query zai events for credits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ts, model string
		var prompt, completion, hit, miss uint64
		if err := rows.Scan(&ts, &model, &prompt, &completion, &hit, &miss); err != nil {
			return fmt.Errorf("scan zai event: %w", err)
		}
		at, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		credits, err := ZaiCreditsAt(model, UsageTokens{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			CacheHitTokens:   hit,
			CacheMissTokens:  miss,
		}, at)
		if err != nil {
			continue // unknown model: leave bucket credits as-is
		}
		bucket := at.UTC().Format("2006-01-02")
		if bucketMinutes > 0 {
			bucketSecs := int64(bucketMinutes) * 60
			epoch := at.UTC().Unix() / bucketSecs * bucketSecs
			bucket = time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05")
		}
		if i, ok := idx[key{bucket, model}]; ok {
			out[i].ZaiCredits += credits
		}
	}
	return rows.Err()
}

// QuerySessionDetail returns a DailySummary for a specific session ID.
func (s *Store) QuerySessionDetail(sessionID string) (DailySummary, error) {
	return queryAndScan(s, "query session",
		eventDetailQuery+` WHERE session_id = ? ORDER BY timestamp ASC`, []any{sessionID},
		func(rows *sql.Rows) (DailySummary, error) {
			return buildDailySummary(rows, "", sessionID)
		})
}

// accumulateDailyEvent folds a single event into ds and its byModel breakdown.
// It is pure: it reads only ev and mutates only ds/byModel, never the DB.
func accumulateDailyEvent(ds *DailySummary, byModel map[string]*ModelBreakdown, ev Event) {
	ds.RequestCount++
	ds.TokensIn += ev.PromptTokens
	ds.TokensOut += ev.CompletionTokens
	ds.CacheHitTokens += ev.CacheHitTokens
	ds.CacheMissTokens += ev.CacheMissTokens
	ds.EstUSD += ev.EstUSD

	mb, ok := byModel[ev.Model]
	if !ok {
		mb = &ModelBreakdown{Model: ev.Model, Provider: string(ev.Provider)}
		byModel[ev.Model] = mb
	}
	mb.RequestCount++
	mb.TokensIn += ev.PromptTokens
	mb.TokensOut += ev.CompletionTokens
	mb.CacheHitTokens += ev.CacheHitTokens
	mb.CacheMissTokens += ev.CacheMissTokens
	mb.EstUSD += ev.EstUSD
}

// finalizeDailySummary rounds estimates and folds the model map into ds.ByModel.
func finalizeDailySummary(ds DailySummary, byModel map[string]*ModelBreakdown) DailySummary {
	ds.EstUSD = RoundUSD(ds.EstUSD)
	for _, mb := range byModel {
		mb.EstUSD = RoundUSD(mb.EstUSD)
		ds.ByModel = append(ds.ByModel, *mb)
	}
	return ds
}

// scanEventRow scans one raw event row into an Event, parsing its provider/timestamp.
func scanEventRow(rows *sql.Rows) (Event, error) {
	var ev Event
	var tsStr, provStr string
	if err := rows.Scan(
		&ev.ID, &ev.SessionID, &tsStr, &provStr, &ev.Model,
		&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheHitTokens, &ev.CacheMissTokens,
		&ev.EstUSD, &ev.RequestID, &ev.LatencyMS,
	); err != nil {
		return Event{}, fmt.Errorf("scan event: %w", err)
	}
	ev.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
	ev.Provider = config.Provider(provStr)
	return ev, nil
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
		ev, err := scanEventRow(rows)
		if err != nil {
			return DailySummary{}, err
		}
		if sessionID != "" && ev.SessionID != sessionID {
			continue
		}
		accumulateDailyEvent(&ds, byModel, ev)
	}

	if err := rows.Err(); err != nil {
		return DailySummary{}, fmt.Errorf("rows: %w", err)
	}

	return finalizeDailySummary(ds, byModel), nil
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
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	return queryAndScan(s, "query mtd",
		eventDetailQuery+` WHERE date(timestamp) >= ? ORDER BY timestamp ASC`, []any{start},
		func(rows *sql.Rows) (DailySummary, error) {
			return buildDailySummary(rows, start, "")
		})
}

// QueryByModelSince returns usage breakdown by model since a given time.
func (s *Store) QueryByModelSince(since time.Time) ([]ModelBreakdown, error) {
	return queryAndScan(s, "query by model since",
		modelBreakdownQuery+` WHERE timestamp >= ? GROUP BY provider, model ORDER BY SUM(est_usd) DESC`,
		[]any{since.UTC().Format(time.RFC3339)},
		func(rows *sql.Rows) ([]ModelBreakdown, error) {
			return scanRows(rows, scanModelBreakdownRow)
		})
}

// QueryByModel returns all-time usage breakdown by model.
func (s *Store) QueryByModel() ([]ModelBreakdown, error) {
	return queryAndScan(s, "query by model",
		modelBreakdownQuery+` GROUP BY provider, model ORDER BY SUM(est_usd) DESC`, nil,
		func(rows *sql.Rows) ([]ModelBreakdown, error) {
			return scanRows(rows, scanModelBreakdownRow)
		})
}

// scanModelBreakdownRow scans one row of modelBreakdownQuery.
func scanModelBreakdownRow(rows *sql.Rows) (ModelBreakdown, error) {
	var mb ModelBreakdown
	if err := rows.Scan(&mb.Provider, &mb.Model,
		&mb.RequestCount, &mb.TokensIn, &mb.TokensOut,
		&mb.CacheHitTokens, &mb.CacheMissTokens, &mb.EstUSD); err != nil {
		return ModelBreakdown{}, fmt.Errorf("scan model: %w", err)
	}
	mb.EstUSD = RoundUSD(mb.EstUSD)
	return mb, nil
}

// QueryByProviderSince returns usage breakdown by provider since a given time.
func (s *Store) QueryByProviderSince(since time.Time) ([]ProviderBreakdown, error) {
	return queryAndScan(s, "query by provider since",
		providerBreakdownQuery+` WHERE timestamp >= ? GROUP BY provider ORDER BY SUM(est_usd) DESC`,
		[]any{since.UTC().Format(time.RFC3339)},
		func(rows *sql.Rows) ([]ProviderBreakdown, error) {
			return scanRows(rows, scanProviderBreakdownRow)
		})
}

// QueryByProvider returns all-time usage breakdown by provider.
func (s *Store) QueryByProvider() ([]ProviderBreakdown, error) {
	return queryAndScan(s, "query by provider",
		providerBreakdownQuery+` GROUP BY provider ORDER BY SUM(est_usd) DESC`, nil,
		func(rows *sql.Rows) ([]ProviderBreakdown, error) {
			return scanRows(rows, scanProviderBreakdownRow)
		})
}

// scanProviderBreakdownRow scans one row of providerBreakdownQuery.
func scanProviderBreakdownRow(rows *sql.Rows) (ProviderBreakdown, error) {
	var pb ProviderBreakdown
	if err := rows.Scan(&pb.Provider,
		&pb.RequestCount, &pb.TokensIn, &pb.TokensOut,
		&pb.CacheHitTokens, &pb.CacheMissTokens, &pb.EstUSD); err != nil {
		return ProviderBreakdown{}, fmt.Errorf("scan provider: %w", err)
	}
	pb.EstUSD = RoundUSD(pb.EstUSD)
	return pb, nil
}

// QuerySessions returns a list of all unique sessions with summary info.
func (s *Store) QuerySessions() ([]SessionInfo, error) {
	return s.QuerySessionsSince(time.Time{})
}

// QuerySessionsSince returns sessions whose last event is since a given time.
func (s *Store) QuerySessionsSince(since time.Time) ([]SessionInfo, error) {
	q := sessionsQueryBase
	var args []any
	if !since.IsZero() {
		q += " WHERE timestamp >= ?"
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	q += " GROUP BY session_id ORDER BY MAX(timestamp) DESC"

	sessions, err := queryAndScan(s, "query sessions", q, args, func(rows *sql.Rows) ([]SessionInfo, error) {
		return scanRows(rows, scanSessionInfoRow)
	})
	if sessions == nil {
		sessions = []SessionInfo{}
	}
	return sessions, err
}

// scanSessionInfoRow scans one row of sessionsQueryBase (+ WHERE/GROUP BY/ORDER BY).
func scanSessionInfoRow(rows *sql.Rows) (SessionInfo, error) {
	var si SessionInfo
	if err := rows.Scan(&si.SessionID,
		&si.RequestCount, &si.TokensIn, &si.TokensOut, &si.EstUSD,
		&si.FirstSeen, &si.LastSeen); err != nil {
		return SessionInfo{}, fmt.Errorf("scan session: %w", err)
	}
	si.EstUSD = RoundUSD(si.EstUSD)
	return si, nil
}

// ProviderEstBucket aggregates estimated usage per provider in a time bucket.
type ProviderEstBucket struct {
	Bucket       string
	Provider     string
	EstUSD       float64
	RequestCount uint64
	TokensIn     uint64
	TokensOut    uint64
}

// QueryProviderEstSince returns estimated spend grouped by provider in buckets
// since a given time. bucketMinutes > 0 buckets by that interval; 0 buckets by
// calendar day (local time).
func (s *Store) QueryProviderEstSince(since time.Time, bucketMinutes int) ([]ProviderEstBucket, error) {
	var bucketExpr string
	var args []any
	if bucketMinutes > 0 {
		bucketSecs := bucketMinutes * 60
		bucketExpr = `strftime('%Y-%m-%dT%H:%M:00',
		 datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch'))`
		args = []any{bucketSecs, bucketSecs}
	} else {
		bucketExpr = "date(timestamp, 'localtime')"
	}
	args = append(args, since.Format(time.RFC3339))
	q := `SELECT ` + bucketExpr + ` as bucket, provider,
	 COUNT(*),
	 COALESCE(SUM(prompt_tokens),0),
	 COALESCE(SUM(completion_tokens),0),
	 COALESCE(SUM(est_usd),0)
	 FROM events WHERE timestamp >= ?
	 GROUP BY bucket, provider
	 ORDER BY bucket ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query provider est since: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ProviderEstBucket
	for rows.Next() {
		var b ProviderEstBucket
		if err := rows.Scan(&b.Bucket, &b.Provider, &b.RequestCount,
			&b.TokensIn, &b.TokensOut, &b.EstUSD); err != nil {
			return nil, fmt.Errorf("scan provider est bucket: %w", err)
		}
		b.EstUSD = RoundUSD(b.EstUSD)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// QueryFirstEventSince returns the timestamp of the earliest usage event for
// the given provider at or after since. ok is false when no such event exists.
// Used to estimate rolling-window quota resets (Z.AI 5-hour credits) that the
// provider API does not expose.
func (s *Store) QueryFirstEventSince(provider string, since time.Time) (t time.Time, ok bool, err error) {
	q := `SELECT MIN(timestamp) FROM events WHERE provider = ? AND timestamp >= ?`
	var ts sql.NullString
	if err := s.db.QueryRow(q, provider, since.UTC().Format(time.RFC3339)).Scan(&ts); err != nil {
		return time.Time{}, false, fmt.Errorf("query first event since: %w", err)
	}
	if !ts.Valid || ts.String == "" {
		return time.Time{}, false, nil
	}
	t, err = time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse first event timestamp: %w", err)
	}
	return t.UTC(), true, nil
}

// DBStats holds database-level statistics.
type DBStats struct {
	EventCount   int64  `json:"event_count"`
	SessionCount int64  `json:"session_count"`
	DBFileSize   int64  `json:"db_file_size"`
	FirstEvent   string `json:"first_event"`
	LastEvent    string `json:"last_event"`
}

// QueryStats returns aggregated database statistics.
func (s *Store) QueryStats() (DBStats, error) {
	var stats DBStats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&stats.EventCount); err != nil {
		return stats, fmt.Errorf("count events: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT session_id) FROM events`).Scan(&stats.SessionCount); err != nil {
		return stats, fmt.Errorf("count sessions: %w", err)
	}
	_ = s.db.QueryRow(`SELECT COALESCE(MIN(timestamp),''), COALESCE(MAX(timestamp),'') FROM events`).Scan(&stats.FirstEvent, &stats.LastEvent)
	return stats, nil
}
