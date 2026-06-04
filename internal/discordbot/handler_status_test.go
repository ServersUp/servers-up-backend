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

func TestHandleRequest_StatusUP(t *testing.T) {
	t.Parallel()

	var getCalls int
	f := newStatusHandlerFixture(t, &MockStatusStore{
		GetFunc: func(ctx context.Context, gameID, serverID string) (*models.GameServerStatus, error) {
			getCalls++
			if gameID != "wow" || serverID != "battlenet#us#57" {
				return nil, fmt.Errorf("unexpected keys: %s %s", gameID, serverID)
			}
			return &models.GameServerStatus{
				GameID:        "wow",
				ServerID:      serverID,
				Status:        "UP",
				LastUpdatedAt: 1710000000,
			}, nil
		},
	})

	statusBody := `{"type": 2, "guild_id": "guild-1", "member": {"user": {"id": "user-status-1"}}, "data": {"name": "status", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}]}}`

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, statusBody))
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
	if !strings.Contains(discordResp.Data.Content, "**wow-us-illidan** is **UP**") {
		t.Fatalf("expected status line with region, got %q", discordResp.Data.Content)
	}
	if getCalls != 1 {
		t.Fatalf("expected 1 GetServerStatus call, got %d", getCalls)
	}

	// Second identical request should hit the result cache, not DDB.
	resp2, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, statusBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on cached status, got %d", resp2.StatusCode)
	}
	if getCalls != 1 {
		t.Fatalf("expected cache to prevent second DDB read, got %d calls", getCalls)
	}
}

func TestHandleRequest_StatusRateLimited(t *testing.T) {
	t.Parallel()

	var getCalls int
	f := newStatusHandlerFixture(t, &MockStatusStore{
		GetFunc: func(ctx context.Context, gameID, serverID string) (*models.GameServerStatus, error) {
			getCalls++
			return &models.GameServerStatus{GameID: gameID, ServerID: serverID, Status: "UP", LastUpdatedAt: 1}, nil
		},
	})

	statusBody := `{"type": 2, "guild_id": "guild-1", "member": {"user": {"id": "user-rate-1"}}, "data": {"name": "status", "options": [{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}]}}`

	for i := 0; i < statusPerUserLimit; i++ {
		_, _ = f.handler.HandleRequest(context.Background(), f.signedRequest(t, statusBody))
	}
	before := getCalls

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, statusBody))
	if err != nil {
		t.Fatal(err)
	}
	var discordResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &discordResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discordResp.Data.Content, "too quickly") {
		t.Fatalf("expected rate limit message, got %q", discordResp.Data.Content)
	}
	if discordResp.Data.Flags != 64 {
		t.Fatalf("expected ephemeral flags 64, got %d", discordResp.Data.Flags)
	}
	if getCalls != before {
		t.Fatalf("rate limited request should not call GetServerStatus; calls %d -> %d", before, getCalls)
	}
}
