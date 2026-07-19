package cli

import (
	"bytes"
	"testing"
)

func TestDoctorCmd_NoKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := NewRoot()
	cmd.SetArgs([]string{"doctor"})
	// Doctor reports failures + returns error → expected.
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected doctor to return error without keys")
	}
}

func TestDoctorCmd_RunsWithoutCrash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Initialize default settings.
	cmd0 := NewRoot()
	_ = cmd0.Execute()

	cmd := NewRoot()
	cmd.SetArgs([]string{"doctor"})
	// Runs without crashing (may fail due to missing keys).
	_ = cmd.Execute()
}

func TestDoctorCmd_Help(t *testing.T) {
	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorHelpPrintsUsage(t *testing.T) {
	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"help", "doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("expected doctor help output")
	}
}

func TestDoctorHelpPrintsAllCommands(t *testing.T) {
	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"doctor", "status", "usage", "stop"} {
		if !bytes.Contains(out.Bytes(), []byte(name)) {
			t.Fatalf("--help missing command %q", name)
		}
	}
}
