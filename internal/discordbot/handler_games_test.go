package discordbot

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
)

func TestHandleRequest_Games(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "games"}}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	c := discordResp.Data.Content
	if !strings.Contains(c, "**Supported games**") {
		t.Fatalf("expected supported games header, got %q", c)
	}
	if !strings.Contains(c, "`wipe`") || !strings.Contains(c, "`wow`") {
		t.Fatalf("expected both games from mapping, got %q", c)
	}
	wipeIdx := strings.Index(c, "`wipe`")
	wowIdx := strings.Index(c, "`wow`")
	if wipeIdx == -1 || wowIdx == -1 || wipeIdx >= wowIdx {
		t.Fatalf("expected alphabetical wipe then wow, got %q", c)
	}
}

func TestHandleRequest_Subscriptions(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-9", "data": {"name": "subscriptions"}}`

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		if guildID != "guild-1" {
			return nil, nil
		}
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", Mention: ""},
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", Mention: "<@&123>"},
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-2", Mention: ""},
		}, nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discordResp.Data.Content, "<#chan-1>") {
		t.Fatalf("expected channel grouping, got %q", discordResp.Data.Content)
	}
	if !strings.Contains(discordResp.Data.Content, "wow-us-illidan") {
		t.Fatalf("expected human server label with region, got %q", discordResp.Data.Content)
	}
}

func TestHandleRequest_Servers(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "data": {"name": "servers", "options": [{"name": "game", "value": "wipe"}, {"name": "region", "value": "us"}]}}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discordResp.Data.Content, "`alpha`") {
		t.Fatalf("expected wipe servers, got %q", discordResp.Data.Content)
	}
}
