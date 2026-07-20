package cli

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/doctor"
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
		{level: slog.Level(-10), want: "🐛"}, // below debug → debug (first match)
		{level: slog.Level(4), want: "⚠️"},  // equals LevelWarn → warn
		{level: slog.Level(6), want: "🚨"},   // between warn and error → error (default)
		{level: slog.Level(12), want: "🚨"},  // above error → error (default)
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
		{level: slog.Level(4), want: "warn"},  // equals LevelWarn
		{level: slog.Level(6), want: "error"}, // between warn and error → error (default)
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

func TestReloadLogger(t *testing.T) {
	// Verify reloadLogger doesn't panic and sets a new handler.
	reloadLogger(slog.LevelDebug)
	// Log something — should not panic.
	slog.Debug("test reload")
}

func TestCountFailed(t *testing.T) {
	tests := []struct {
		name   string
		report doctor.Report
		want   int
	}{
		{
			name: "all ok",
			report: doctor.Report{
				OK:     true,
				Checks: []doctor.Check{{Name: "a", OK: true}, {Name: "b", OK: true}},
			},
			want: 0,
		},
		{
			name: "some failed",
			report: doctor.Report{
				OK: false,
				Checks: []doctor.Check{
					{Name: "a", OK: true},
					{Name: "b", OK: false},
					{Name: "c", OK: false},
				},
			},
			want: 2,
		},
		{
			name: "all failed",
			report: doctor.Report{
				OK:     false,
				Checks: []doctor.Check{{Name: "a", OK: false}, {Name: "b", OK: false}},
			},
			want: 2,
		},
		{
			name: "empty",
			report: doctor.Report{
				OK:     true,
				Checks: nil,
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countFailed(tt.report)
			if got != tt.want {
				t.Fatalf("countFailed = %d want %d", got, tt.want)
			}
		})
	}
}

func TestWritePrettyLine(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		contain string
	}{
		{
			name:    "info log",
			raw:     `{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"hello"}`,
			contain: "INFO",
		},
		{
			name:    "debug log",
			raw:     `{"time":"2026-01-01T00:00:00Z","level":"DEBUG","msg":"debugging"}`,
			contain: "DEBU",
		},
		{
			name:    "warn log",
			raw:     `{"time":"2026-01-01T00:00:00Z","level":"WARN","msg":"warning"}`,
			contain: "WARN",
		},
		{
			name:    "error log",
			raw:     `{"time":"2026-01-01T00:00:00Z","level":"ERROR","msg":"failed"}`,
			contain: "ERRO",
		},
		{
			name:    "no level",
			raw:     `{"msg":"bare"}`,
			contain: `"msg": "bare"`,
		},
		{
			name:    "invalid json",
			raw:     "not json at all",
			contain: "not json at all",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writePrettyLine(&buf, tt.raw)
			if !strings.Contains(buf.String(), tt.contain) {
				t.Fatalf("output %q does not contain %q", buf.String(), tt.contain)
			}
		})
	}
}

func TestFormatLogLines(t *testing.T) {
	t.Run("valid json lines", func(t *testing.T) {
		input := `{"level":"INFO","msg":"one"}
{"level":"WARN","msg":"two"}
`
		var buf bytes.Buffer
		err := formatLogLines(&buf, strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if !strings.Contains(out, "INFO") || !strings.Contains(out, "WARN") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
	t.Run("mixed valid and invalid", func(t *testing.T) {
		input := `{"level":"INFO","msg":"ok"}
garbage line
{"level":"ERROR","msg":"bad"}
`
		var buf bytes.Buffer
		err := formatLogLines(&buf, strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if !strings.Contains(out, "garbage line") || !strings.Contains(out, "ERRO") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
	t.Run("empty input", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatLogLines(&buf, strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		if buf.Len() != 0 {
			t.Fatal("expected empty output")
		}
	})
	t.Run("blank lines skipped", func(t *testing.T) {
		input := "\n\n{\"level\":\"INFO\",\"msg\":\"only\"}\n\n"
		var buf bytes.Buffer
		err := formatLogLines(&buf, strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if !strings.Contains(out, "only") {
			t.Fatalf("expected output to contain log message: %q", out)
		}
		// The output should have exactly one log entry (blank lines are skipped).
		// Each entry is a multi-line pretty-printed JSON — verify no duplicate.
		count := strings.Count(out, `"msg": "only"`)
		if count != 1 {
			t.Fatalf("expected exactly 1 occurrence of message, got %d: %q", count, out)
		}
	})
}

func TestParseLogLevel(t *testing.T) {
	// Ensure ParseLogLevel from usage handles all valid levels.
	for _, name := range []string{"debug", "info", "warn", "error", "DEBUG", "INFO", "WARN", "ERROR", "warning", "WARNING"} {
		level := usage.ParseLogLevel(name)
		if level < slog.LevelDebug {
			t.Fatalf("ParseLogLevel(%q) = %d, expected >= LevelDebug", name, level)
		}
	}
}
