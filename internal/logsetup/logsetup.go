package logsetup

import (
	"log/slog"
	"os"
	"strings"
)

// ConfigureDefaultFromEnv sets the default slog logger to JSON on stdout.
// LOG_LEVEL may be DEBUG, INFO, WARN, or ERROR (default INFO).
func ConfigureDefaultFromEnv() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(os.Getenv("LOG_LEVEL")),
	})))
}

func parseLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
