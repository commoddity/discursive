package usage

import (
	"log/slog"
	"os"
	"strings"
)

// ParseLogLevel maps EnvLogLevel values to slog.Level (default Info).
func ParseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogLevelFromEnv reads DISCURSIVE_LOG_LEVEL (default info).
func LogLevelFromEnv() slog.Level {
	return ParseLogLevel(os.Getenv(EnvLogLevel))
}
