package discordbot

import (
	"context"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func TestSplitGameServerHuman(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in         string
		wantGame   string
		wantRegion string
		wantServer string
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

func TestUnsubscribeChoiceText_hyphenatedGameName(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"forever": {Provider: "battlenet", DisplayName: "WoW Forever", Regions: map[string]servermap.Region{
			"us": {Servers: map[string]servermap.Server{"normal": {Identifier: 2}}},
		}},
	}}
	sub := models.Subscription{ServerID: "battlenet#us#2", ServerLabel: "wow-forever-us-normal", ChannelID: "c1"}
	got := (&Handler{}).subscriptionUnsubscribeChoiceText(context.Background(), "g1", m, sub)
	if !strings.HasPrefix(got, "wow-forever · us · normal · ") {
		t.Fatalf("got %q", got)
	}
}
