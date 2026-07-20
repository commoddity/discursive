package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
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
		{name: "deepseek alias", requested: "o3-mini", wantAlias: "o3-mini", wantReal: "deepseek-v4-flash"},
		{name: "kimi alias", requested: "gpt-4o", wantAlias: "gpt-4o", wantReal: "kimi-k3"},
		{name: "real k2.6", requested: "kimi-k2.6", wantAlias: "kimi-k2.6", wantReal: "kimi-k2.6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRoot()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"set", "--model", tt.requested})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
			if err != nil {
				t.Fatal(err)
			}
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
	cmd.SetArgs([]string{"set", "--model", "not-a-model"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompleteModelIDs(t *testing.T) {
	all := completeModelIDs("")
	if len(all) < 4 {
		t.Fatalf("expected advertised models, got %v", all)
	}
	filtered := completeModelIDs("gpt-4")
	for _, id := range filtered {
		if !strings.HasPrefix(id, "gpt-4") {
			t.Fatalf("unexpected id %q for prefix gpt-4", id)
		}
	}
	if len(filtered) == 0 {
		t.Fatal("expected gpt-4* completions")
	}
}

func TestShellCompletionHints(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "log-level args", args: []string{"__complete", "log-level", ""}, want: "debug"},
		{name: "start tunnel flag", args: []string{"__complete", "start", "--tunnel", ""}, want: "named"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRoot()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)
			_ = cmd.Execute() // __complete exits 0 with completions on stdout
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("output %q missing %q", out.String(), tt.want)
			}
		})
	}
}
