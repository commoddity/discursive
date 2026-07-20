package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStatusCmd_Output(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Initialize defaults.
	cmd0 := NewRoot()
	_ = cmd0.Execute()

	// Capture stdout via pipe (slog writes to os.Stdout).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	cmd := NewRoot()
	cmd.SetArgs([]string{"status"})
	execErr := cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	if execErr != nil {
		t.Fatal(execErr)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	output := buf.String()

	// Verify JSON-slog lines contain expected fields.
	for _, field := range []string{
		`"alias_model"`,
		`"real_model"`,
		`"local_port"`,
		`"data_root"`,
		`"version"`,
		`"models"`,
		`"gateway_key_masked"`,
	} {
		if !strings.Contains(output, field) {
			t.Fatalf("status output missing field %q: %s", field, output)
		}
	}
	if strings.Contains(output, `"gateway_key"`) {
		t.Fatal("status without --show-key must not emit gateway_key")
	}
}

func TestStatusCmd_ShowKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd0 := NewRoot()
	_ = cmd0.Execute()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	cmd := NewRoot()
	cmd.SetArgs([]string{"status", "--show-key"})
	execErr := cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	if execErr != nil {
		t.Fatal(execErr)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	output := buf.String()

	if !strings.Contains(output, `"gateway_key"`) {
		t.Fatalf("status --show-key missing gateway_key: %s", output)
	}
	if strings.Contains(output, `"gateway_key_masked"`) {
		t.Fatal("status --show-key must not emit gateway_key_masked")
	}
	// Full key starts with sk-
	if !strings.Contains(output, `"sk-`) {
		t.Fatalf("status --show-key should include full sk- key: %s", output)
	}
}

func TestRotateGatewayKey_ShowKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd0 := NewRoot()
	_ = cmd0.Execute()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	cmd := NewRoot()
	cmd.SetArgs([]string{"set", "--rotate-gateway-key", "--show-key"})
	execErr := cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	if execErr != nil {
		t.Fatal(execErr)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	output := buf.String()

	if !strings.Contains(output, `"gateway_key"`) {
		t.Fatalf("rotate --show-key missing gateway_key: %s", output)
	}
	if strings.Contains(output, `"gateway_key_masked"`) {
		t.Fatal("rotate --show-key must not emit gateway_key_masked")
	}
}

func TestGatewayKeyLogAttrs(t *testing.T) {
	attrs := gatewayKeyLogAttrs("sk-secret", false)
	if len(attrs) != 2 || attrs[0] != "gateway_key_masked" {
		t.Fatalf("masked attrs: %#v", attrs)
	}
	attrs = gatewayKeyLogAttrs("sk-secret", true)
	if len(attrs) != 2 || attrs[0] != "gateway_key" || attrs[1] != "sk-secret" {
		t.Fatalf("show attrs: %#v", attrs)
	}
}

func TestStatusCmd_ModelsAreJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd0 := NewRoot()
	_ = cmd0.Execute()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	cmd := NewRoot()
	cmd.SetArgs([]string{"status"})
	_ = cmd.Execute()
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	// Each non-empty line should be parseable JSON.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d not JSON: %q err %v", i, line, err)
		}
	}
}
