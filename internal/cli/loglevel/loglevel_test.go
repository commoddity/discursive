package loglevel

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/commoddity/discursive/internal/usage"
)

func TestLevelEmoji(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{level: slog.LevelDebug, want: "🐛"},
		{level: slog.LevelInfo, want: "ℹ️"},
		{level: slog.LevelWarn, want: "⚠️"},
		{level: slog.LevelError, want: "🚨"},
		{level: slog.Level(-10), want: "🐛"},
		{level: slog.Level(4), want: "⚠️"},
		{level: slog.Level(6), want: "🚨"},
		{level: slog.Level(12), want: "🚨"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("level-%d", tt.level), func(t *testing.T) {
			got := levelEmoji(tt.level)
			if got != tt.want {
				t.Fatalf("levelEmoji(%d) = %q want %q", tt.level, got, tt.want)
			}
		})
	}
}

func TestSlogLevelName(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{level: slog.LevelDebug, want: "debug"},
		{level: slog.LevelInfo, want: "info"},
		{level: slog.LevelWarn, want: "warn"},
		{level: slog.LevelError, want: "error"},
		{level: slog.Level(-10), want: "debug"},
		{level: slog.Level(4), want: "warn"},
		{level: slog.Level(6), want: "error"},
		{level: slog.Level(12), want: "error"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("level-%d", tt.level), func(t *testing.T) {
			got := slogLevelName(tt.level)
			if got != tt.want {
				t.Fatalf("slogLevelName(%d) = %q want %q", tt.level, got, tt.want)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	for _, name := range []string{"debug", "info", "warn", "error", "DEBUG", "INFO", "WARN", "ERROR", "warning", "WARNING"} {
		level := usage.ParseLogLevel(name)
		if level < slog.LevelDebug {
			t.Fatalf("ParseLogLevel(%q) = %d, expected >= LevelDebug", name, level)
		}
	}
}
