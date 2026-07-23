package logs

import (
	"bytes"
	"strings"
	"testing"
)

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
		count := strings.Count(out, `"msg": "only"`)
		if count != 1 {
			t.Fatalf("expected exactly 1 occurrence of message, got %d: %q", count, out)
		}
	})
}
