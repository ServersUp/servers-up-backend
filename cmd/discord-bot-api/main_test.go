package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

// MockDatabase implements the Database interface for testing.
type MockDatabase struct {
	AddFunc    func(ctx context.Context, sub models.Subscription) error
	DeleteFunc func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error
	ListFunc   func(ctx context.Context, guildID string) ([]models.Subscription, error)
}

func (m *MockDatabase) AddSubscription(ctx context.Context, sub models.Subscription) error {
	return m.AddFunc(ctx, sub)
}
func (m *MockDatabase) DeleteSubscription(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
	return m.DeleteFunc(ctx, guildID, channelID, serverID, subscriptionID)
}
func (m *MockDatabase) ListSubscriptionsByGuild(ctx context.Context, guildID string) ([]models.Subscription, error) {
	return m.ListFunc(ctx, guildID)
}

// MockConfig implements the ConfigProvider interface for testing.
type MockConfig struct {
	LoadFunc func(ctx context.Context, bucket, key string, target any) error
}

func (m *MockConfig) LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error {
	return m.LoadFunc(ctx, bucket, key, target)
}

func TestHandleRequest(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	publicKeyHex := hex.EncodeToString(pub)

	mockDB := &MockDatabase{}
	mockConfig := &MockConfig{
		LoadFunc: func(ctx context.Context, bucket, key string, target any) error {
			t := target.(*servermap.Mapping)
			t.Games = map[string]servermap.Game{
				"wow": {
					Provider: "battlenet",
					Servers: map[string]servermap.Server{
						"illidan": {Region: "us", Identifier: 57},
					},
				},
				"wipe": {
					Provider: "other",
					Servers: map[string]servermap.Server{
						"alpha": {Region: "us", Identifier: 1},
					},
				},
			}
			return nil
		},
	}

	handler := &Handler{
		database:         mockDB,
		configProvider:   mockConfig,
		discordPublicKey: publicKeyHex,
	}

	t.Run("Ping (Type 1)", func(t *testing.T) {
		body := `{"type": 1}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if resp.Body != `{"type":1}` {
			t.Errorf("expected pong response, got %s", resp.Body)
		}
	})

	t.Run("Subscribe (Type 2)", func(t *testing.T) {
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}, {"name": "role", "value": "123"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		mockDB.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
			return nil, nil
		}
		mockDB.AddFunc = func(ctx context.Context, sub models.Subscription) error {
			if sub.ServerID != "battlenet#us#57" {
				return fmt.Errorf("unexpected server ID: %s", sub.ServerID)
			}
			if sub.Mention != "<@&123>" {
				return fmt.Errorf("unexpected mention: %s", sub.Mention)
			}
			return nil
		}

		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		var discordResp discord.InteractionResponse
		json.Unmarshal([]byte(resp.Body), &discordResp)
		if discordResp.Data.Content == "" {
			t.Error("expected content in response")
		}
	})

	t.Run("Subscribe duplicate blocked (Type 2)", func(t *testing.T) {
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}, {"name": "role", "value": "123"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		var addCalls int
		mockDB.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
			return []models.Subscription{
				{
					ServerID:  "battlenet#us#57",
					GuildID:   "guild-1",
					ChannelID: "chan-1",
					Mention:   "<@&123>",
					RoleName:  "Raid",
				},
			}, nil
		}
		mockDB.AddFunc = func(ctx context.Context, sub models.Subscription) error {
			addCalls++
			return nil
		}

		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if addCalls != 0 {
			t.Fatalf("expected AddSubscription not called, got %d calls", addCalls)
		}
		var discordResp discord.InteractionResponse
		json.Unmarshal([]byte(resp.Body), &discordResp)
		if !strings.Contains(discordResp.Data.Content, "Already subscribed") {
			t.Fatalf("expected already subscribed message, got %q", discordResp.Data.Content)
		}
		if !strings.Contains(discordResp.Data.Content, "wow-illidan") || !strings.Contains(discordResp.Data.Content, "@Raid") {
			t.Fatalf("expected human-readable game-server and role, got %q", discordResp.Data.Content)
		}
	})

	t.Run("Unsubscribe removes selected subscription (Type 2)", func(t *testing.T) {
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "unsubscribe", "options": [{"name": "subscription", "value": "sub-illidan-1"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		mockDB.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
			if guildID != "guild-1" {
				return nil, fmt.Errorf("unexpected guildID: %s", guildID)
			}
			return []models.Subscription{
				{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", SubscriptionID: "sub-illidan-1", Mention: "", RoleName: ""},
			}, nil
		}

		var calls int
		mockDB.DeleteFunc = func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
			calls++
			if serverID != "battlenet#us#57" || subscriptionID != "sub-illidan-1" {
				return fmt.Errorf("unexpected keys: %s %s", serverID, subscriptionID)
			}
			if channelID != "chan-1" || guildID != "guild-1" {
				return fmt.Errorf("unexpected guild/channel: %s %s", guildID, channelID)
			}
			return nil
		}

		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if calls != 1 {
			t.Fatalf("expected DeleteSubscription called once, got %d", calls)
		}
		var discordResp discord.InteractionResponse
		json.Unmarshal([]byte(resp.Body), &discordResp)
		if !strings.Contains(discordResp.Data.Content, "Unsubscribed") || !strings.Contains(discordResp.Data.Content, "wow-illidan") {
			t.Fatalf("expected unsubscribe wording, got %q", discordResp.Data.Content)
		}
	})

	t.Run("Subscriptions list (Type 2)", func(t *testing.T) {
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-9", "data": {"name": "subscriptions"}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		mockDB.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
			if guildID != "guild-1" {
				return nil, fmt.Errorf("unexpected guildID: %s", guildID)
			}
			return []models.Subscription{
				{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", Mention: ""},
				{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", Mention: "<@&123>"},
				{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-2", Mention: ""},
			}, nil
		}

		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		var discordResp discord.InteractionResponse
		json.Unmarshal([]byte(resp.Body), &discordResp)
		if !strings.Contains(discordResp.Data.Content, "<#chan-1>") {
			t.Fatalf("expected channel grouping, got %q", discordResp.Data.Content)
		}
		if !strings.Contains(discordResp.Data.Content, "wow-illidan") {
			t.Fatalf("expected human server label, got %q", discordResp.Data.Content)
		}
	})

	t.Run("Invalid Signature", func(t *testing.T) {
		resp, _ := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   "wrong",
				"x-signature-timestamp": "123",
			},
			Body: `{"type": 1}`,
		})

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Autocomplete game focused (Type 4)", func(t *testing.T) {
		body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "game", "value": "w", "focused": true}, {"type": 3, "name": "server"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		resp, err := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})
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
	})

	t.Run("Autocomplete subscription for unsubscribe (Type 4)", func(t *testing.T) {
		body := `{"type": 4, "guild_id": "guild-1", "channel_id": "chan-999", "data": {"name": "unsubscribe", "options": [{"type": 3, "name": "subscription", "value": "ill", "focused": true}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		mockDB.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
			return []models.Subscription{
				{ServerID: "battlenet#us#57", GuildID: "guild-1", ChannelID: "chan-1", SubscriptionID: "sub-1", Mention: "<@&99>", RoleName: "Booty Bay"},
				{ServerID: "other#us#1", GuildID: "guild-1", ChannelID: "chan-2", SubscriptionID: "sub-2", Mention: ""},
			}, nil
		}

		resp, err := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})
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
	})

	t.Run("Autocomplete server focused without game (Type 4)", func(t *testing.T) {
		body := `{"type": 4, "guild_id": "guild-1", "data": {"name": "subscribe", "options": [{"type": 3, "name": "server", "value": "ill", "focused": true}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		resp, err := handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
			Headers: map[string]string{
				"x-signature-ed25519":   sig,
				"x-signature-timestamp": timestamp,
			},
			Body: body,
		})
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
			t.Fatalf("expected empty choices without game, got %#v", discordResp.Data.Choices)
		}
	})
}
