package discordbot

import (
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
)

func TestDMGuildIDRoundTrip(t *testing.T) {
	t.Parallel()

	id := dmGuildID("123456789")
	if id != "dm#123456789" {
		t.Fatalf("expected dm#123456789, got %q", id)
	}
	if !isDMGuildID(id) {
		t.Errorf("expected %q to be a DM guild id", id)
	}
	if isDMGuildID("guild-1") {
		t.Error("expected guild-1 to not be a DM guild id")
	}
	if got := dmUserIDFromGuildID(id); got != "123456789" {
		t.Errorf("expected user id 123456789, got %q", got)
	}
}

func TestIsDMInteraction(t *testing.T) {
	t.Parallel()

	var dm discord.Interaction
	if !isDMInteraction(dm) {
		t.Error("expected empty guild id interaction to be a DM")
	}
	dm.GuildID = "guild-1"
	if isDMInteraction(dm) {
		t.Error("expected guild interaction to not be a DM")
	}
}
