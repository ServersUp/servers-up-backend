package logsetup

import (
	"context"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env  string
		want slog.Level
	}{
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"bogus", slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := parseLevel(tc.env); got != tc.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tc.env, got, tc.want)
		}
	}
}

func TestConfigureDefaultFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "ERROR")
	ConfigureDefaultFromEnv()
	if slog.Default().Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected INFO disabled at ERROR level")
	}
}
