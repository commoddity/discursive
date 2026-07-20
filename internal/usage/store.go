package usage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; loaded via database/sql

	"github.com/commoddity/discursive/internal/config"
)

// Event is one usage record (real model id, post-alias).
type Event struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"sessionId"`
	Timestamp        time.Time       `json:"timestamp"`
	Provider         config.Provider `json:"provider"`
	Model            string          `json:"model"`
	PromptTokens     uint64          `json:"promptTokens"`
	CompletionTokens uint64          `json:"completionTokens"`
	CacheHitTokens   uint64          `json:"cacheHitTokens"`
	CacheMissTokens  uint64          `json:"cacheMissTokens"`
	EstUSD           float64         `json:"estUsd"`
	RequestID        string          `json:"requestId"`
	LatencyMS        uint64          `json:"latencyMs"`
}

// SessionSummary aggregates events for one session.
type SessionSummary struct {
	SessionID        string
	RequestCount     uint64
	PromptTokens     uint64
	CompletionTokens uint64
	CacheHitTokens   uint64
	CacheMissTokens  uint64
	EstUSD           float64
	ByModel          map[string]ModelTotals
}

// ModelTotals per real model id within a session.
type ModelTotals struct {
	Provider         config.Provider
	RequestCount     uint64
	PromptTokens     uint64
	CompletionTokens uint64
	CacheHitTokens   uint64
	CacheMissTokens  uint64
	EstUSD           float64
}

// Store persists usage events in SQLite under {dataRoot}/usage/.
type Store struct {
	db     *sql.DB
	dbPath string
}

// DBPath returns the filesystem path to the SQLite database.
func (s *Store) DBPath() string { return s.dbPath }

// NewStore creates the usage directory and opens/creates the SQLite database.
func NewStore(dataRoot string) (*Store, error) {
	dir := filepath.Join(dataRoot, "usage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create usage dir: %w", err)
	}

	dbPath := filepath.Join(dir, "usage.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open usage db: %w", err)
	}

	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, dbPath: dbPath}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func initSchema(db *sql.DB) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS events (
		id              TEXT PRIMARY KEY,
		session_id      TEXT    NOT NULL,
		timestamp       TEXT    NOT NULL,
		provider        TEXT    NOT NULL,
		model           TEXT    NOT NULL,
		prompt_tokens   INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		cache_hit_tokens  INTEGER NOT NULL DEFAULT 0,
		cache_miss_tokens INTEGER NOT NULL DEFAULT 0,
		est_usd         REAL    NOT NULL DEFAULT 0,
		request_id      TEXT    NOT NULL DEFAULT '',
		latency_ms      INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
	CREATE INDEX IF NOT EXISTS idx_events_session  ON events(session_id);
	CREATE INDEX IF NOT EXISTS idx_events_prov_model ON events(provider, model);
	`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func newEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "evt_" + hex.EncodeToString(b[:])
}

// Record inserts an event with computed est_usd and returns the stored event.
func (s *Store) Record(ev Event) (Event, error) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.ID == "" {
		ev.ID = newEventID()
	}

	tokens := UsageTokens{
		PromptTokens:     ev.PromptTokens,
		CompletionTokens: ev.CompletionTokens,
		CacheHitTokens:   ev.CacheHitTokens,
		CacheMissTokens:  ev.CacheMissTokens,
	}
	est, err := EstimateUSD(ev.Provider, ev.Model, tokens)
	if err != nil {
		return Event{}, err
	}
	ev.EstUSD = est

	_, err = s.db.Exec(
		`INSERT INTO events (id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.SessionID, ev.Timestamp.Format(time.RFC3339Nano),
		string(ev.Provider), ev.Model,
		ev.PromptTokens, ev.CompletionTokens, ev.CacheHitTokens, ev.CacheMissTokens,
		ev.EstUSD, ev.RequestID, ev.LatencyMS,
	)
	if err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	return ev, nil
}

// RecordAndObserve persists the event then feeds the idle Aggregator.
func (s *Store) RecordAndObserve(agg *Aggregator, ev Event) error {
	stored, err := s.Record(ev)
	if err != nil {
		return err
	}
	if agg != nil {
		agg.Observe(stored)
	}
	return nil
}

// LoadEvents reads all events from SQLite ordered by timestamp.
func (s *Store) LoadEvents() ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms
		 FROM events ORDER BY timestamp ASC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Event
	for rows.Next() {
		var ev Event
		var tsStr, provStr string
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &tsStr, &provStr, &ev.Model,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheHitTokens, &ev.CacheMissTokens,
			&ev.EstUSD, &ev.RequestID, &ev.LatencyMS,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		ev.Provider = config.Provider(provStr)
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// SessionSummary aggregates events for a sessionID from SQLite.
func (s *Store) SessionSummary(sessionID string) (SessionSummary, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, provider, model,
		 prompt_tokens, completion_tokens, cache_hit_tokens, cache_miss_tokens,
		 est_usd, request_id, latency_ms
		 FROM events WHERE session_id = ? ORDER BY timestamp ASC`,
		sessionID)
	if err != nil {
		return SessionSummary{}, fmt.Errorf("query session: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sum := SessionSummary{
		SessionID: sessionID,
		ByModel:   make(map[string]ModelTotals),
	}
	for rows.Next() {
		var ev Event
		var tsStr, provStr string
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &tsStr, &provStr, &ev.Model,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheHitTokens, &ev.CacheMissTokens,
			&ev.EstUSD, &ev.RequestID, &ev.LatencyMS,
		); err != nil {
			return SessionSummary{}, fmt.Errorf("scan event: %w", err)
		}
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		ev.Provider = config.Provider(provStr)

		sum.RequestCount++
		sum.PromptTokens += ev.PromptTokens
		sum.CompletionTokens += ev.CompletionTokens
		sum.CacheHitTokens += ev.CacheHitTokens
		sum.CacheMissTokens += ev.CacheMissTokens
		sum.EstUSD += ev.EstUSD

		mt := sum.ByModel[ev.Model]
		mt.Provider = ev.Provider
		mt.RequestCount++
		mt.PromptTokens += ev.PromptTokens
		mt.CompletionTokens += ev.CompletionTokens
		mt.CacheHitTokens += ev.CacheHitTokens
		mt.CacheMissTokens += ev.CacheMissTokens
		mt.EstUSD += ev.EstUSD
		sum.ByModel[ev.Model] = mt
	}
	if err := rows.Err(); err != nil {
		return SessionSummary{}, fmt.Errorf("rows: %w", err)
	}
	return sum, nil
}

// DeleteEventsBefore deletes all events with timestamp before the given cutoff.
// Returns the number of deleted rows.
func (s *Store) DeleteEventsBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM events WHERE timestamp < ?`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("delete events before %v: %w", cutoff, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
