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
			t := target.(*ServerMapping)
			t.Games = map[string]struct {
				Provider string `json:"provider"`
				Servers  map[string]struct {
					Region     string `json:"region"`
					Identifier any    `json:"identifier"`
				} `json:"servers"`
			}{
				"wow": {
					Provider: "battlenet",
					Servers: map[string]struct {
						Region     string `json:"region"`
						Identifier any    `json:"identifier"`
					}{
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
		body := `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}]}}`
		timestamp := "12345"
		sig := hex.EncodeToString(ed25519.Sign(priv, []byte(timestamp+body)))

		mockDB.AddFunc = func(ctx context.Context, sub models.Subscription) error {
			if sub.ServerID != "battlenet#us#57" {
				return fmt.Errorf("unexpected server ID: %s", sub.ServerID)
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
