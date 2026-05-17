package discordbot

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/aws/aws-lambda-go/events"
)

func TestSubscribe_deniedWithoutManageChannels(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	handler := &Handler{
		discordPublicKey: hex.EncodeToString(pub),
	}

	body := `{"type": 2, "id": "ix-1", "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "u1"}, "permissions": "2048"}, "data": {"name": "subscribe", "options": [{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}]}}`
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
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
		t.Fatalf("status %d", resp.StatusCode)
	}

	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if discordResp.Data.Flags != 64 {
		t.Fatalf("expected ephemeral, flags=%d", discordResp.Data.Flags)
	}
	if discordResp.Data.Content == "" {
		t.Fatal("expected denial content")
	}
}
