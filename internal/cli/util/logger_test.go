package util

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSetupLoggerEmitsJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	SetupLogger()
	slog.Info("json_logger_probe", "ok", true)
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	line := strings.TrimSpace(buf.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("not JSON: %q err %v", line, err)
	}
	if m["msg"] != "json_logger_probe" {
		t.Fatalf("msg: %v", m["msg"])
	}
}

func TestReloadLogger(t *testing.T) {
	ReloadLogger(slog.LevelDebug)
	slog.Debug("test reload")
}
