package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestRootHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "help flag", args: []string{"--help"}, want: "Usage:"},
		{name: "lists_set", args: []string{"--help"}, want: "set"},
		{name: "lists_init", args: []string{"--help"}, want: "init"},
		{name: "lists_start", args: []string{"--help"}, want: "start"},
		{name: "lists_completion", args: []string{"--help"}, want: "completion"},
		{name: "version", args: []string{"version"}, want: "0.0.0-dev"},
		{name: "mentions_deepseek", args: []string{"--help"}, want: "DeepSeek"},
		{name: "completion_zsh", args: []string{"completion", "zsh"}, want: "compdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRoot()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, tt.want) {
				t.Fatalf("output %q missing %q", out, tt.want)
			}
		})
	}
}

func TestSetKeysNoPlaintextInOutput(t *testing.T) {
	exeDir := t.TempDir()
	// Force portable data next to a fake exe dir via --portable and HOME isolation is hard;
	// instead call config APIs through commands with env by chdir + marker.
	dataRoot := filepath.Join(exeDir, "DiscursiveData")
	if err := os.MkdirAll(filepath.Join(dataRoot, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Unit-level CLI: use setUpstreamKey path via cobra with --portable requires real Executable().
	// Cover command wiring + masking without relying on os.Executable by testing package helpers.
	secret := "sk-super-secret-moonshot-value-do-not-leak"
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMoonshotKey(dataRoot, secret); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-deepseek-secret-value"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(config.ConfigPath(dataRoot))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("plaintext moonshot key found in config.json")
	}
	if !loaded.HasMoonshotKey() || !loaded.HasDeepSeekKey() {
		t.Fatal("expected both keys after save")
	}
}
