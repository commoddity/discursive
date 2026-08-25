package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

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

func TestSetModelFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-deepseek-model-test"); err != nil {
		t.Fatalf("set deepseek: %v", err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--model", "o3-mini"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := config.Load(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AliasModel != "o3-mini" || loaded.RealModel != "deepseek-v4-flash" {
		t.Fatalf("got alias=%q real=%q want o3-mini deepseek-v4-flash", loaded.AliasModel, loaded.RealModel)
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

func TestSetClearMoonshotKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetMoonshotKey(dataRoot, "sk-moonshot-clear-test"); err != nil {
		t.Fatalf("set moonshot: %v", err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-deepseek-clear-test"); err != nil {
		t.Fatalf("set deepseek: %v", err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--clear", "moonshot"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.HasMoonshotKey() {
		t.Fatal("moonshot key should be cleared")
	}
	if !loaded.HasDeepSeekKey() {
		t.Fatal("deepseek key should remain")
	}
}

func TestSetClearLastChatProviderFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-only-deepseek"); err != nil {
		t.Fatalf("set deepseek: %v", err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--clear", "deepseek"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when clearing last chat provider key")
	}
}

func TestSetClearAndSetSameProviderFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetMoonshotKey(dataRoot, "sk-ms"); err != nil {
		t.Fatalf("set moonshot: %v", err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-ds"); err != nil {
		t.Fatalf("set deepseek: %v", err)
	}
	if err := config.Save(dataRoot, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "--clear", "moonshot", "--moonshot-key", "sk-new"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for clear + set same provider")
	}
}
