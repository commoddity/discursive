package util

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReadSecretPlainFlag(t *testing.T) {
	cmd := &cobra.Command{}
	got, err := ReadSecretPlain(cmd, "Test", "  sk-from-flag  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-flag" {
		t.Fatalf("got %q", got)
	}
}

func TestReadSecretPlainStdinPipe(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("sk-from-stdin\n"))
	got, err := ReadSecretPlain(cmd, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-from-stdin" {
		t.Fatalf("got %q", got)
	}
}

func TestReadLinePlainFlag(t *testing.T) {
	cmd := &cobra.Command{}
	got, err := ReadLinePlain(cmd, "URL", "  https://x.example.com/v1  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}
