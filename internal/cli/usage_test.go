package cli

import (
	"bytes"
	"testing"
)

func TestUsageCmd_EmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	_ = cmd.Execute()

	cmd2 := NewRoot()
	cmd2.SetArgs([]string{"usage"})
	err := cmd2.Execute()
	if err != nil {
		t.Fatal(err)
	}
}

func TestUsageCmd_NonexistentSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	_ = cmd.Execute()

	cmd2 := NewRoot()
	cmd2.SetArgs([]string{"usage", "--session", "nonexistent"})
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestUsageCmd_Help(t *testing.T) {
	cmd := NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"usage", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
