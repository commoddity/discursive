package usage

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIdleTimeoutFromEnv_Default(t *testing.T) {
	os.Unsetenv(EnvUsageIdleTimeout)
	t.Cleanup(func() { os.Unsetenv(EnvUsageIdleTimeout) })

	if got := IdleTimeoutFromEnv(); got != DefaultIdleTimeout {
		t.Fatalf("got %v want %v", got, DefaultIdleTimeout)
	}
}

func TestIdleTimeoutFromEnv_Custom(t *testing.T) {
	t.Setenv(EnvUsageIdleTimeout, "10s")
	if got := IdleTimeoutFromEnv(); got != 10*time.Second {
		t.Fatalf("got %v want 10s", got)
	}
}

func TestIdleTimeoutFromEnv_Invalid(t *testing.T) {
	tests := []string{"not-a-duration", "0s", "-5s"}
	for _, val := range tests {
		t.Run(val, func(t *testing.T) {
			t.Setenv(EnvUsageIdleTimeout, val)
			if got := IdleTimeoutFromEnv(); got != DefaultIdleTimeout {
				t.Fatalf("%q: got %v want %v", val, got, DefaultIdleTimeout)
			}
		})
	}
}

func TestIdleTimeoutFromEnv_Empty(t *testing.T) {
	t.Setenv(EnvUsageIdleTimeout, "")
	if got := IdleTimeoutFromEnv(); got != DefaultIdleTimeout {
		t.Fatalf("got %v want %v", got, DefaultIdleTimeout)
	}
}

func TestLogLevelFromEnv_Default(t *testing.T) {
	os.Unsetenv(EnvLogLevel)
	t.Cleanup(func() { os.Unsetenv(EnvLogLevel) })

	if got := LogLevelFromEnv(); got != slog.LevelInfo {
		t.Fatalf("got %v want LevelInfo", got)
	}
}

func TestLogLevelFromEnv_Debug(t *testing.T) {
	t.Setenv(EnvLogLevel, "debug")
	if got := LogLevelFromEnv(); got != slog.LevelDebug {
		t.Fatalf("got %v want LevelDebug", got)
	}
}

func TestLogLevelFromEnv_Warn(t *testing.T) {
	t.Setenv(EnvLogLevel, "warn")
	if got := LogLevelFromEnv(); got != slog.LevelWarn {
		t.Fatalf("got %v want LevelWarn", got)
	}
}

func TestLogLevelFromEnv_Error(t *testing.T) {
	t.Setenv(EnvLogLevel, "error")
	if got := LogLevelFromEnv(); got != slog.LevelError {
		t.Fatalf("got %v want LevelError", got)
	}
}

func TestLogLevelFromEnv_CaseInsensitive(t *testing.T) {
	t.Setenv(EnvLogLevel, "DEBUG")
	if got := LogLevelFromEnv(); got != slog.LevelDebug {
		t.Fatalf("got %v want LevelDebug", got)
	}
}

func TestLogLevelFromEnv_WarningAlias(t *testing.T) {
	t.Setenv(EnvLogLevel, "warning")
	if got := LogLevelFromEnv(); got != slog.LevelWarn {
		t.Fatalf("got %v want LevelWarn", got)
	}
}

func TestRoundUSD(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0.1234, 0.123},
		{0.1235, 0.124}, // rounds up
		{0.9999, 1.0},
		{0.0, 0.0},
		{3.421, 3.421},
		{3.4219, 3.422},
	}
	for _, tt := range tests {
		got := RoundUSD(tt.in)
		if got != tt.want {
			t.Fatalf("RoundUSD(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatByModel_Empty(t *testing.T) {
	if got := FormatByModel(nil); got != "" {
		t.Fatalf("got %q want empty", got)
	}
	if got := FormatByModel(map[string]ModelTotals{}); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestFormatByModel_StableOrder(t *testing.T) {
	by := map[string]ModelTotals{
		"kimi-k3":           {EstUSD: 0.100},
		"deepseek-v4-flash": {EstUSD: 0.050},
	}
	got := FormatByModel(by)
	// Alphabetically sorted: deepseek-v4-flash first.
	if !strings.Contains(got, "deepseek-v4-flash=0.050") {
		t.Fatalf("missing deepseek: %s", got)
	}
	if !strings.Contains(got, "kimi-k3=0.100") {
		t.Fatalf("missing kimi: %s", got)
	}
	if idxD := strings.Index(got, "deepseek"); idxD < 0 {
		t.Fatal("deepseek should be first alphabetically")
	} else if idxK := strings.Index(got, "kimi"); idxK < idxD {
		t.Fatal("deepseek should be before kimi alphabetically")
	}
}

func TestSummarizeEvents(t *testing.T) {
	events := []Event{
		{SessionID: "s1", Provider: "moonshot", Model: "kimi-k3", PromptTokens: 100, CompletionTokens: 50, CacheHitTokens: 20, CacheMissTokens: 80, EstUSD: 0.300},
		{SessionID: "s1", Provider: "deepseek", Model: "deepseek-v4-flash", PromptTokens: 200, CompletionTokens: 100, CacheHitTokens: 0, CacheMissTokens: 200, EstUSD: 0.100},
	}
	sum := summarizeEvents(events)

	if sum.SessionID != "s1" {
		t.Fatalf("SessionID = %q", sum.SessionID)
	}
	if sum.RequestCount != 2 {
		t.Fatalf("RequestCount = %d", sum.RequestCount)
	}
	if sum.PromptTokens != 300 {
		t.Fatalf("PromptTokens = %d", sum.PromptTokens)
	}
	if sum.CompletionTokens != 150 {
		t.Fatalf("CompletionTokens = %d", sum.CompletionTokens)
	}
	if sum.CacheHitTokens != 20 {
		t.Fatalf("CacheHitTokens = %d", sum.CacheHitTokens)
	}
	if sum.CacheMissTokens != 280 {
		t.Fatalf("CacheMissTokens = %d", sum.CacheMissTokens)
	}
	if sum.EstUSD != 0.4 {
		t.Fatalf("EstUSD = %v", sum.EstUSD)
	}
	if len(sum.ByModel) != 2 {
		t.Fatalf("ByModel len = %d", len(sum.ByModel))
	}
	if sum.ByModel["kimi-k3"].EstUSD != 0.300 {
		t.Fatalf("kimi-k3 EstUSD = %v", sum.ByModel["kimi-k3"].EstUSD)
	}
	if sum.ByModel["deepseek-v4-flash"].EstUSD != 0.100 {
		t.Fatalf("deepseek-v4-flash EstUSD = %v", sum.ByModel["deepseek-v4-flash"].EstUSD)
	}
}

func TestParseLogLevel_Cases(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"  debug  ", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParseLogLevel(tt.in); got != tt.want {
				t.Fatalf("ParseLogLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
