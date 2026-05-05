package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

// MockDatabase implements the Database interface for testing.
type MockDatabase struct {
	AddFunc    func(ctx context.Context, sub models.Subscription) error
	DeleteFunc func(ctx context.Context, serverID, channelID string) (bool, error)
}

func (m *MockDatabase) AddSubscription(ctx context.Context, sub models.Subscription) error {
	return m.AddFunc(ctx, sub)
}
func (m *MockDatabase) DeleteSubscriptionByChannel(ctx context.Context, serverID, channelID string) (bool, error) {
	return m.DeleteFunc(ctx, serverID, channelID)
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

	t.Run("Unsubscribe removes all channel subscriptions (Type 2)", func(t *testing.T) {
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "unsubscribe", "options": [{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		var calls int
		mockDB.DeleteFunc = func(ctx context.Context, serverID, channelID string) (bool, error) {
			calls++
			if serverID != "battlenet#us#57" {
				return false, fmt.Errorf("unexpected serverID: %s", serverID)
			}
			if channelID != "chan-1" {
				return false, fmt.Errorf("unexpected channelID: %s", channelID)
			}
			// We can't see the internal per-item deletes from this handler-level mock,
			// but we can at least verify the handler calls deletion once for the channel.
			return true, nil
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
			t.Fatalf("expected DeleteSubscriptionByChannel called once, got %d", calls)
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
}
