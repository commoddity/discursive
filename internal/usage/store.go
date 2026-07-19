package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"discursive/internal/config"
)

const eventsFileName = "events.jsonl"

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

// Store persists usage JSONL under {dataRoot}/usage/.
type Store struct {
	dir        string
	eventsPath string
}

// NewStore creates the usage directory under dataRoot.
func NewStore(dataRoot string) (*Store, error) {
	dir := filepath.Join(dataRoot, "usage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create usage dir: %w", err)
	}
	return &Store{dir: dir, eventsPath: filepath.Join(dir, eventsFileName)}, nil
}

// Record appends an event with computed est_usd and returns the stored event.
func (s *Store) Record(ev Event) (Event, error) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
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

	raw, err := json.Marshal(ev)
	if err != nil {
		return Event{}, fmt.Errorf("encode event: %w", err)
	}
	f, err := os.OpenFile(s.eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open events: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	return ev, nil
}

// RecordAndObserve persists the event then feeds the idle Aggregator (DEBUG + INFO summary).
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

// LoadEvents reads all events from JSONL.
func (s *Store) LoadEvents() ([]Event, error) {
	f, err := os.Open(s.eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events: %w", err)
	}
	defer func() { _ = f.Close() }()

	var out []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("parse event: %w", err)
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return out, nil
}

// SessionSummary aggregates events for sessionID.
func (s *Store) SessionSummary(sessionID string) (SessionSummary, error) {
	events, err := s.LoadEvents()
	if err != nil {
		return SessionSummary{}, err
	}
	sum := SessionSummary{
		SessionID: sessionID,
		ByModel:   make(map[string]ModelTotals),
	}
	for _, ev := range events {
		if ev.SessionID != sessionID {
			continue
		}
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
	return sum, nil
}
