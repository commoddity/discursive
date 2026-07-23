// Package util holds cross-command helpers for the Discursive CLI
// (logger setup, data-root resolution, pretty JSON, key masking).
//
// Contract: leaf helpers only — must not import command subpackages.
package util

import (
	"log/slog"
	"os"
	"strings"

	"github.com/commoddity/discursive/internal/usage"
)

// SetupLogger configures slog JSON on stdout from DISCURSIVE_LOG_LEVEL.
func SetupLogger() {
	level := usage.LogLevelFromEnv()
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}

// SetupLoggerWithLevel configures slog from an explicit level string
// (e.g. start --log-level). Unknown values fall back to info with a warn.
func SetupLoggerWithLevel(raw string) {
	level := usage.ParseLogLevel(raw)
	if raw != "" {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "debug", "info", "warn", "error", "warning":
			// valid
		default:
			slog.Warn("unknown log level, using info", "level", raw)
			level = slog.LevelInfo
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}

// ReloadLogger sets slog to the given level (used by log-level command).
func ReloadLogger(level slog.Level) {
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}
