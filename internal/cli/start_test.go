package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/cli/initcmd"
	"github.com/commoddity/discursive/internal/config"
)

func TestStartValidateNamedMissingToken(t *testing.T) {
	dataRoot := t.TempDir()
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.PublicBaseURL = "https://ai.example.com/v1"
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatal(err)
	}
	err := config.ValidateTunnelSettings(s)
	if err == nil {
		t.Fatal("expected validation error without tunnel token")
	}
}

func TestStartAutoSetupNonInteractiveErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"start"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected setup-required error")
	}
	if !strings.Contains(err.Error(), "setup required") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestStartSkipsSetupWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}

	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMoonshotKey(dataRoot, "sk-ms"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTunnelToken(dataRoot, "tok"); err != nil {
		t.Fatal(err)
	}
	s.PublicBaseURL = "https://ai.example.com/v1"
	s.TunnelMode = config.TunnelModeNamed
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatal(err)
	}
	if config.NeedsSetup(s) {
		t.Fatal("expected configured settings")
	}
}

func TestSetupFillsOnlyMissingPublicURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}

	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMoonshotKey(dataRoot, "sk-ms"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTunnelToken(dataRoot, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	portable := func() bool { return false }
	setupCmd := initcmd.NewCmd(portable)
	setupCmd.SetOut(&out)
	setupCmd.SetErr(&errBuf)
	setupCmd.SetIn(strings.NewReader("https://only-url.example.com/v1\n"))
	if err := initcmd.RunSetup(setupCmd, portable, initcmd.Flags{}, initcmd.Opts{FromStart: true}); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	loaded, err := config.Load(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublicBaseURL != "https://only-url.example.com/v1" {
		t.Fatalf("got %q", loaded.PublicBaseURL)
	}
	if !loaded.HasMoonshotKey() || !loaded.HasTunnelToken() {
		t.Fatal("expected existing secrets preserved")
	}
}
