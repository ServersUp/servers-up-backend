package discordbot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
)

func TestHandleRequest_Unsubscribe(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "unsubscribe", "options": [{"name": "subscription", "value": "sub-illidan-1"}]}}`

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		if guildID != "guild-1" {
			return nil, fmt.Errorf("unexpected guildID: %s", guildID)
		}
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", SubscriptionID: "sub-illidan-1"},
		}, nil
	}

	var calls int
	f.db.DeleteFunc = func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
		calls++
		if serverID != "battlenet#us#57" || subscriptionID != "sub-illidan-1" {
			return fmt.Errorf("unexpected keys: %s %s", serverID, subscriptionID)
		}
		if channelID != "chan-1" || guildID != "guild-1" {
			return fmt.Errorf("unexpected guild/channel: %s %s", guildID, channelID)
		}
		return nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("expected DeleteSubscription called once, got %d", calls)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discordResp.Data.Content, "Unsubscribed") || !strings.Contains(discordResp.Data.Content, "wow-us-illidan") {
		t.Fatalf("expected unsubscribe wording with region label, got %q", discordResp.Data.Content)
	}
}
