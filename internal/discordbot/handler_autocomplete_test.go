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

func TestHandleRequest_AutocompleteGameFocused(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "game", "value": "w", "focused": true}, {"type": 3, "name": "region"}, {"type": 3, "name": "server"}]}}`

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if discordResp.Type != discord.InteractionResponseTypeApplicationCommandAutocompleteResult {
		t.Fatalf("expected autocomplete response type 8, got %d", discordResp.Type)
	}
	if discordResp.Data == nil || len(discordResp.Data.Choices) > 25 {
		t.Fatalf("expected choices len <= 25, got %v", discordResp.Data)
	}
	if len(discordResp.Data.Choices) != 2 {
		t.Fatalf("expected 2 game suggestions for prefix w, got %d", len(discordResp.Data.Choices))
	}
	if discordResp.Data.Choices[0].Value != "wipe" || discordResp.Data.Choices[1].Value != "wow" {
		t.Fatalf("unexpected choices: %#v", discordResp.Data.Choices)
	}
}

func TestHandleRequest_AutocompleteRegionFocused(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "game", "value": "wow"}, {"type": 3, "name": "region", "value": "u", "focused": true}, {"type": 3, "name": "server"}]}}`

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if discordResp.Type != discord.InteractionResponseTypeApplicationCommandAutocompleteResult {
		t.Fatalf("expected autocomplete type 8, got %d", discordResp.Type)
	}
	if len(discordResp.Data.Choices) != 1 || discordResp.Data.Choices[0].Value != "us" {
		t.Fatalf("expected [us] for prefix u, got %#v", discordResp.Data.Choices)
	}
}

func TestHandleRequest_AutocompleteServerWithGameRegion(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "game", "value": "wow"}, {"type": 3, "name": "region", "value": "us"}, {"type": 3, "name": "server", "value": "ill", "focused": true}]}}`

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if len(discordResp.Data.Choices) != 1 || discordResp.Data.Choices[0].Value != "illidan" {
		t.Fatalf("expected [illidan], got %#v", discordResp.Data.Choices)
	}
}

func TestHandleRequest_AutocompleteServerWithoutRegion(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "game", "value": "wow"}, {"type": 3, "name": "server", "value": "ill", "focused": true}]}}`

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if len(discordResp.Data.Choices) != 0 {
		t.Fatalf("expected empty choices without region, got %#v", discordResp.Data.Choices)
	}
}

func TestHandleRequest_AutocompleteUnsubscribeSubscription(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 4, "guild_id": "guild-1", "channel_id": "chan-999", "data": {"name": "unsubscribe", "options": [{"type": 3, "name": "subscription", "value": "ill", "focused": true}]}}`

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", SubscriptionID: "sub-1", Mention: "<@&99>", RoleName: "Booty Bay"},
			{ServerID: "other#us#1", GuildID: "guild-1", ChannelID: "chan-2", SubscriptionID: "sub-2"},
		}, nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if len(discordResp.Data.Choices) != 1 || discordResp.Data.Choices[0].Value != "sub-1" {
		t.Fatalf("expected one channel-matched subscription choice, got %#v", discordResp.Data.Choices)
	}
	name := discordResp.Data.Choices[0].Name
	if !strings.Contains(name, "wow") || !strings.Contains(name, "illidan") || !strings.Contains(name, "@Booty Bay") {
		t.Fatalf("expected human game, server, and @role in choice name, got %q", name)
	}
	if strings.Contains(name, "sub-1") || strings.Contains(name, "<@&") || strings.Contains(name, "99") {
		t.Fatalf("choice name should not expose subscription id or raw role snowflake, got %q", name)
	}
}
