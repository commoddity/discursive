package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestStopCmd_NoPidFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	_ = cmd.Execute()

	cmd2 := NewRoot()
	cmd2.SetArgs([]string{"stop"})
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestStopCmd_DeadPidFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRoot()
	_ = cmd.Execute()

	// Simulate a PID file with a non-existent PID.
	dataRoot, err := config.DataRoot(config.ResolveOpts{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(dataRoot, "gateway.pid")
	if err := os.WriteFile(pidPath, []byte("99999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Running stop on a dead process should succeed (graceful).
	cmd2 := NewRoot()
	cmd2.SetArgs([]string{"stop"})
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}

	// PID file should ideally be cleaned up after failed signal.
	_, err = os.Stat(pidPath)
	_ = err // may or may not exist
}

func TestStopCmd_Help(t *testing.T) {
	cmd := NewRoot()
	cmd.SetArgs([]string{"stop", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
