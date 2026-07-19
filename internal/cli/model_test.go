package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"discursive/internal/config"
)

func TestSetupLoggerEmitsJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	setupLogger()
	slog.Info("json_logger_probe", "ok", true)
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	line := strings.TrimSpace(buf.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("not JSON: %q err %v", line, err)
	}
	if m["msg"] != "json_logger_probe" {
		t.Fatalf("msg: %v", m["msg"])
	}
}

func TestSetModelPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name      string
		requested string
		wantAlias string
		wantReal  string
	}{
		{name: "deepseek alias", requested: "gpt-4o-mini", wantAlias: "gpt-4o-mini", wantReal: "deepseek-v4-flash"},
		{name: "kimi alias", requested: "gpt-5-high", wantAlias: "gpt-5-high", wantReal: "kimi-k3"},
		{name: "real id", requested: "kimi-k2.7-code", wantAlias: "kimi-k2.7-code", wantReal: "kimi-k2.7-code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRoot()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"set-model", tt.requested})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
			s, err := config.Load(dataRoot)
			if err != nil {
				t.Fatal(err)
			}
			if s.AliasModel != tt.wantAlias || s.RealModel != tt.wantReal {
				t.Fatalf("got alias=%q real=%q want %q %q", s.AliasModel, s.RealModel, tt.wantAlias, tt.wantReal)
			}
		})
	}
}

func TestSetModelUnknown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := NewRoot()
	cmd.SetArgs([]string{"set-model", "not-a-model"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
