package usage

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"discursive/internal/config"
)

func TestLogEventIsDebug(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	LogEvent(Event{
		SessionID: "sess-1", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 100, EstUSD: 0.0001, RequestID: "req-1",
	})
	out := buf.String()
	if !strings.Contains(out, `"level":"DEBUG"`) && !strings.Contains(out, `"level":"debug"`) {
		// slog JSON uses "DEBUG" for LevelDebug in Go 1.21+
		if !strings.Contains(out, "DEBUG") && !strings.Contains(out, "debug") {
			t.Fatalf("expected DEBUG level in %s", out)
		}
	}
	if !strings.Contains(out, `"msg":"usage"`) {
		t.Fatalf("missing usage msg: %s", out)
	}
	if strings.Contains(out, "sk-super-secret") {
		t.Fatal("secret in log")
	}
}

func TestLogEventHiddenAtInfo(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	LogEvent(Event{SessionID: "s", Provider: config.ProviderMoonshot, Model: "kimi-k3"})
	if buf.Len() != 0 {
		t.Fatalf("DEBUG usage should be hidden at INFO: %s", buf.String())
	}
}

func TestLogSummaryIsInfo(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	LogSummary(SessionSummary{
		SessionID: "sess-1", RequestCount: 2, PromptTokens: 100, EstUSD: 0.5,
		ByModel: map[string]ModelTotals{"kimi-k3": {EstUSD: 0.5}},
	})
	out := buf.String()
	if !strings.Contains(out, "usage_summary") {
		t.Fatalf("missing usage_summary: %s", out)
	}
	if !strings.Contains(out, "request_count") {
		t.Fatalf("missing request_count: %s", out)
	}
}

func TestAggregatorIdleFlush(t *testing.T) {
	var (
		mu      sync.Mutex
		flushed []SessionSummary
	)
	agg := NewAggregator(50 * time.Millisecond)
	agg.onFlush = func(s SessionSummary) {
		mu.Lock()
		flushed = append(flushed, s)
		mu.Unlock()
	}

	agg.Observe(Event{
		SessionID: "sess-a", Provider: config.ProviderMoonshot, Model: "kimi-k3",
		PromptTokens: 1_000_000, EstUSD: 3.0, RequestID: "r1",
	})
	agg.Observe(Event{
		SessionID: "sess-a", Provider: config.ProviderDeepSeek, Model: "deepseek-v4-flash",
		PromptTokens: 1_000_000, CompletionTokens: 1_000_000, EstUSD: 0.42, RequestID: "r2",
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(flushed)
		mu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle flush did not fire")
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	sum := flushed[0]
	mu.Unlock()
	if sum.RequestCount != 2 {
		t.Fatalf("request_count=%d", sum.RequestCount)
	}
	if sum.EstUSD < 3.4 || sum.EstUSD > 3.5 {
		t.Fatalf("est_usd=%v", sum.EstUSD)
	}
	agg.Stop()
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		if got := ParseLogLevel(tt.in); got != tt.want {
			t.Fatalf("%q: got %v want %v", tt.in, got, tt.want)
		}
	}
}
