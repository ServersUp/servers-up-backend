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

func TestHandleRequest_Subscribe(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}, {"name": "role", "value": "123"}]}}`

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return nil, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		if sub.ServerID != "battlenet#us#57" {
			return fmt.Errorf("unexpected server ID: %s", sub.ServerID)
		}
		if sub.Mention != "<@&123>" {
			return fmt.Errorf("unexpected mention: %s", sub.Mention)
		}
		if sub.ServerLabel != "wow-us-illidan" {
			return fmt.Errorf("unexpected server label: %s", sub.ServerLabel)
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
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if discordResp.Data.Content == "" {
		t.Error("expected non-empty content in response")
	}
}

func TestHandleRequest_SubscribeDuplicate(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}, {"name": "role", "value": "123"}]}}`

	var addCalls int
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", Mention: "<@&123>", RoleName: "Raid"},
		}, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalls++
		return nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if addCalls != 0 {
		t.Fatalf("expected AddSubscription not called, got %d calls", addCalls)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discordResp.Data.Content, "Already subscribed") {
		t.Fatalf("expected already subscribed message, got %q", discordResp.Data.Content)
	}
	if !strings.Contains(discordResp.Data.Content, "wow-us-illidan") || !strings.Contains(discordResp.Data.Content, "@Raid") {
		t.Fatalf("expected human-readable game-region-server and role, got %q", discordResp.Data.Content)
	}
}

func TestHandleRequest_SubscribeDuplicateVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		existing   models.Subscription
		body       string
		wantSubstr []string
	}{
		{
			name: "existing role, subscribe channel-wide",
			existing: models.Subscription{
				ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1",
				Mention: "<@&123>", RoleName: "Raid",
			},
			body:       `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}]}}`,
			wantSubstr: []string{"Already subscribed", "wow-us-illidan", "@Raid"},
		},
		{
			name: "existing channel-wide, subscribe with role",
			existing: models.Subscription{
				ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1",
			},
			body:       `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}, {"name": "role", "value": "123"}]}}`,
			wantSubstr: []string{"Already subscribed", "wow-us-illidan"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Each subtest gets its own fixture to avoid data races on shared MockDatabase fields.
			sf := newTestHandlerFixture(t)
			var addCalls int
			sf.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
				return []models.Subscription{tc.existing}, nil
			}
			sf.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
				addCalls++
				return nil
			}

			resp, err := sf.handler.HandleRequest(context.Background(), sf.signedRequest(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if addCalls != 0 {
				t.Fatalf("expected AddSubscription not called, got %d calls", addCalls)
			}
			var discordResp discord.InteractionResponse
			if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
				t.Fatal(err)
			}
			for _, s := range tc.wantSubstr {
				if !strings.Contains(discordResp.Data.Content, s) {
					t.Fatalf("expected %q in message, got %q", s, discordResp.Data.Content)
				}
			}
		})
	}
}
