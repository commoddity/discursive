package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestReadSecretPlainFlag(t *testing.T) {
	cmd := newSetCmd()
	got, err := readSecretPlain(cmd, "Test", "  sk-from-flag  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-flag" {
		t.Fatalf("got %q", got)
	}
}

func TestReadSecretPlainStdinPipe(t *testing.T) {
	cmd := newSetCmd()
	cmd.SetIn(strings.NewReader("sk-from-stdin\n"))
	got, err := readSecretPlain(cmd, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-stdin" {
		t.Fatalf("got %q", got)
	}
}

func TestSetMoonshotKeyFromFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--moonshot-key", "sk-piped-moonshot-key"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out.String(), "sk-piped-moonshot-key") {
		t.Fatal("plaintext key in output")
	}

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.HasMoonshotKey() {
		t.Fatal("expected moonshot key saved")
	}
}

func TestSetDeepSeekKeyFromFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--deepseek-key", "sk-deepseek-key"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.HasDeepSeekKey() {
		t.Fatal("expected deepseek key saved")
	}
}

func TestSetNoFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for no flags")
	}
}

func TestSetModelFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--model", "o3-mini"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if s.AliasModel != "o3-mini" || s.RealModel != "deepseek-v4-flash" {
		t.Fatalf("got alias=%q real=%q want o3-mini deepseek-v4-flash", s.AliasModel, s.RealModel)
	}
}
