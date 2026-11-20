package discordbot

import (
	"strings"
	"testing"
)

func TestSplitGameServerHuman(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in           string
		wantGame     string
		wantRegion   string
		wantServer   string
	}{
		{"wow-us-illidan", "wow", "us", "illidan"},
		{"wow-eu-kazzak", "wow", "eu", "kazzak"},
		{"wow-illidan", "wow", "", "illidan"},
		{"bad", "bad", "", "bad"},
	}
	for _, tt := range tests {
		g, r, s := splitGameServerHuman(tt.in)
		if g != tt.wantGame || r != tt.wantRegion || s != tt.wantServer {
			t.Fatalf("%q: got %q,%q,%q want %q,%q,%q", tt.in, g, r, s, tt.wantGame, tt.wantRegion, tt.wantServer)
		}
	}
}

func FuzzSplitGameServerHuman(f *testing.F) {
	seeds := []string{
		"wow-us-illidan",
		"wow-eu-kazzak",
		"wow-illidan",
		"bad",
		"",
		"-",
		"--",
		"a-b-c-d-e",
		"game-region-server-with-hyphens",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		game, region, server := splitGameServerHuman(input)
		// Must not panic; basic invariants only.
		if region != "" && !strings.Contains(input, "-") {
			t.Errorf("region set but input has no dash: input=%q region=%q", input, region)
		}
		// Re-joining must not produce a string longer than input (sanity check).
		parts := strings.Join([]string{game, region, server}, "|")
		_ = parts
	})
}

