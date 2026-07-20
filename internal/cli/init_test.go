package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestInitNonInteractiveFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"init",
		"--moonshot-key", "sk-moonshot-init-test",
		"--deepseek-key", "sk-deepseek-init-test",
		"--tunnel-token", "eyJtunnel-token-init-test",
		"--public-url", "https://ai.example.com/v1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out.String(), "sk-moonshot-init-test") {
		t.Fatal("plaintext moonshot key in output")
	}
	if strings.Contains(out.String(), "eyJtunnel-token-init-test") {
		t.Fatal("plaintext tunnel token in output")
	}

	dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.HasMoonshotKey() || !s.HasDeepSeekKey() || !s.HasTunnelToken() {
		t.Fatal("expected all secrets saved")
	}
	if s.PublicBaseURL != "https://ai.example.com/v1" {
		t.Fatalf("public url: got %q", s.PublicBaseURL)
	}
	if config.NormalizeTunnelMode(s.TunnelMode) != config.TunnelModeNamed {
		t.Fatalf("tunnel mode: got %q", s.TunnelMode)
	}
	if err := config.ValidateTunnelSettings(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestInitStdinPipe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("sk-ms\nsk-ds\ntok-cf\nhttps://gw.example.com\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.PublicBaseURL != "https://gw.example.com/v1" {
		t.Fatalf("public url: got %q", s.PublicBaseURL)
	}
	if !s.HasMoonshotKey() || !s.HasDeepSeekKey() || !s.HasTunnelToken() {
		t.Fatal("expected all secrets saved")
	}
}

func TestInitMissingPublicURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"init",
		"--moonshot-key", "sk-ms",
		"--deepseek-key", "sk-ds",
		"--tunnel-token", "tok",
		"--public-url", "",
	})
	cmd.SetIn(strings.NewReader("")) // no URL on stdin either
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing public URL")
	}
	if !strings.Contains(err.Error(), "Public HTTPS base URL") && !strings.Contains(err.Error(), "public base URL") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestSetTunnelTokenWithPublicURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"set-tunnel-token",
		"--token", "eyJnamed-token",
		"--public-url", "https://named.example.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.HasTunnelToken() {
		t.Fatal("expected tunnel token")
	}
	if s.PublicBaseURL != "https://named.example.com/v1" {
		t.Fatalf("public url: got %q", s.PublicBaseURL)
	}
}

func TestSetPublicURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set-public-url", "--url", "https://fix.example.com/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dataRoot := filepath.Join(home, "Library", "Application Support", "Discursive")
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.PublicBaseURL != "https://fix.example.com/v1" {
		t.Fatalf("got %q", s.PublicBaseURL)
	}
}

func TestReadLinePlainFlag(t *testing.T) {
	cmd := newSetPublicURLCmd()
	got, err := readLinePlain(cmd, "URL", "  https://x.example.com/v1  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}
