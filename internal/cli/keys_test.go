package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"discursive/internal/config"
)

func TestReadUpstreamKeyPlainFlag(t *testing.T) {
	cmd := newSetMoonshotKeyCmd()
	got, err := readUpstreamKeyPlain(cmd, "moonshot", "  sk-from-flag  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-flag" {
		t.Fatalf("got %q", got)
	}
}

func TestReadUpstreamKeyPlainStdinPipe(t *testing.T) {
	cmd := newSetMoonshotKeyCmd()
	cmd.SetIn(strings.NewReader("sk-from-stdin\n"))
	got, err := readUpstreamKeyPlain(cmd, "moonshot", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-stdin" {
		t.Fatalf("got %q", got)
	}
}

func TestSetMoonshotKeyFromStdinPipe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("sk-piped-moonshot-key\n"))
	cmd.SetArgs([]string{"set-moonshot-key"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out.String(), "sk-piped-moonshot-key") {
		t.Fatal("plaintext key in output")
	}

	dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.HasMoonshotKey() {
		t.Fatal("expected moonshot key saved")
	}
}
