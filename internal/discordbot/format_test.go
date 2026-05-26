package discordbot

import "testing"

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
