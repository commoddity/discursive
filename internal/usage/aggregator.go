package usage

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultIdleTimeout is how long after the last API call before emitting an
// INFO usage_summary. Override with DISCURSIVE_USAGE_IDLE (e.g. "2m", "90s").
const DefaultIdleTimeout = 30 * time.Second

// EnvUsageIdleTimeout overrides DefaultIdleTimeout (Go duration, e.g. "10m", "90s").
const EnvUsageIdleTimeout = "DISCURSIVE_USAGE_IDLE"

// EnvLogLevel selects slog level: debug, info (default), warn, error.
const EnvLogLevel = "DISCURSIVE_LOG_LEVEL"

// Aggregator groups consecutive usage Events into one "Agent task" window,
// flushing an INFO summary after idle (no new events for IdleTimeout).
type Aggregator struct {
	mu        sync.Mutex
	idle      time.Duration
	events    []Event
	timer     *time.Timer
	afterFunc func(time.Duration, func()) *time.Timer
	onFlush   func(SessionSummary) // tests; default LogSummary
}

// NewAggregator builds an idle-flush aggregator. idle <= 0 uses DefaultIdleTimeout
// (or EnvUsageIdleTimeout when set).
func NewAggregator(idle time.Duration) *Aggregator {
	if idle <= 0 {
		idle = IdleTimeoutFromEnv()
	}
	return &Aggregator{
		idle:      idle,
		afterFunc: time.AfterFunc,
		onFlush:   LogSummary,
	}
}

// IdleTimeoutFromEnv returns DefaultIdleTimeout or a valid parse of EnvUsageIdleTimeout.
func IdleTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvUsageIdleTimeout))
	if raw == "" {
		return DefaultIdleTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return DefaultIdleTimeout
	}
	return d
}

// Observe records one API-call event: DEBUG log + accumulate; resets idle timer.
func (a *Aggregator) Observe(ev Event) {
	LogEvent(ev)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.events = append(a.events, ev)
	a.resetTimerLocked()
}

// Flush emits INFO usage_summary for the current window (if any) and clears it.
func (a *Aggregator) Flush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushLocked()
}

// Stop cancels the idle timer and flushes any pending summary.
func (a *Aggregator) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	a.flushLocked()
}

func (a *Aggregator) resetTimerLocked() {
	if a.timer != nil {
		a.timer.Stop()
	}
	a.timer = a.afterFunc(a.idle, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.flushLocked()
	})
}

func (a *Aggregator) flushLocked() {
	if len(a.events) == 0 {
		return
	}
	sum := summarizeEvents(a.events)
	a.events = nil
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	cb := a.onFlush
	if cb == nil {
		cb = LogSummary
	}
	cb(sum)
}

func summarizeEvents(events []Event) SessionSummary {
	sessionID := events[0].SessionID
	sum := SessionSummary{
		SessionID: sessionID,
		ByModel:   make(map[string]ModelTotals),
	}
	for _, ev := range events {
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
	return sum
}

// FormatByModel returns a stable "model=est,..." string for slog.
func FormatByModel(by map[string]ModelTotals) string {
	if len(by) == 0 {
		return ""
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.3f", k, by[k].EstUSD))
	}
	return strings.Join(parts, ",")
}

// LogEvent emits one API-call usage line at DEBUG (no secrets).
func LogEvent(ev Event) {
	slog.Debug("usage",
		"session_id", ev.SessionID,
		"provider", string(ev.Provider),
		"model", ev.Model,
		"tokens_in", ev.PromptTokens,
		"tokens_out", ev.CompletionTokens,
		"cache_hit_tokens", ev.CacheHitTokens,
		"cache_miss_tokens", ev.CacheMissTokens,
		"est_usd", RoundUSD(ev.EstUSD),
		"request_id", ev.RequestID,
		"latency_ms", ev.LatencyMS,
	)
}

// LogSummary emits an Agent-task (idle window) aggregate at INFO (no secrets).
func LogSummary(sum SessionSummary) {
	slog.Info("usage_summary",
		"session_id", sum.SessionID,
		"request_count", sum.RequestCount,
		"tokens_in", sum.PromptTokens,
		"tokens_out", sum.CompletionTokens,
		"cache_hit_tokens", sum.CacheHitTokens,
		"cache_miss_tokens", sum.CacheMissTokens,
		"est_usd", RoundUSD(sum.EstUSD),
		"by_model", FormatByModel(sum.ByModel),
	)
}

// RoundUSD rounds est_usd to 3 decimal places for slog output.
func RoundUSD(v float64) float64 {
	return math.Round(v*1000) / 1000
}
