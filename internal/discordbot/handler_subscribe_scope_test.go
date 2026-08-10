package discordbot

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
)

func subscribeBody(options string) string {
	return `{"type": 2, "guild_id": "guild-1", "channel_id": "chan-1", "member": {"user": {"id": "user-1"}, "permissions": "16"}, "data": {"name": "subscribe", "options": [` + options + `]}}`
}

func subscribeRespContent(t *testing.T, f *testHandlerFixture, body string) string {
	t.Helper()
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var dr discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &dr); err != nil {
		t.Fatal(err)
	}
	return dr.Data.Content
}

func TestHandleRequest_SubscribeRegionScope(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var created models.Subscription
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return nil, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		created = sub
		return nil
	}

	content := subscribeRespContent(t, f, subscribeBody(`{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}`))

	if created.ServerID != scope.Key("wow", "us") {
		t.Fatalf("unexpected pk: %s", created.ServerID)
	}
	if created.Scope != scope.TypeRegion || created.GameID != "wow" || created.Region != "us" {
		t.Fatalf("unexpected scope fields: %+v", created)
	}
	if created.ServerLabel != "All WoW servers — US" {
		t.Fatalf("unexpected label: %q", created.ServerLabel)
	}
	if !strings.Contains(content, "All WoW servers — US") {
		t.Fatalf("confirmation should mention scope, got %q", content)
	}
	if len(f.scopes.PutCalls) != 1 {
		t.Fatalf("expected 1 baseline put, got %d", len(f.scopes.PutCalls))
	}
	bl := f.scopes.PutCalls[0]
	if bl.ScopeKey != scope.Key("wow", "us") || bl.LastNotifiedEpisode == "" {
		t.Fatalf("unexpected baseline: %+v", bl)
	}
}

func TestHandleRequest_SubscribeMissingRegionRejected(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var addCalls int
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalls++
		return nil
	}

	content := subscribeRespContent(t, f, subscribeBody(`{"name": "game", "value": "wow"}`))

	if addCalls != 0 {
		t.Fatalf("expected no subscription, got %d calls", addCalls)
	}
	if !strings.Contains(content, "**region** is required") {
		t.Fatalf("expected region-required message, got %q", content)
	}
}

func TestHandleRequest_SubscribeServerWithoutRegion(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var addCalls int
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalls++
		return nil
	}

	content := subscribeRespContent(t, f, subscribeBody(`{"name": "game", "value": "wow"}, {"name": "server", "value": "illidan"}`))
	if addCalls != 0 {
		t.Fatalf("expected no subscription, got %d calls", addCalls)
	}
	if !strings.Contains(content, "needs a **region**") {
		t.Fatalf("expected region-required message, got %q", content)
	}
}

func TestHandleRequest_SubscribeExistingBaselineNotDuplicated(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return nil, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		return nil
	}
	f.scopes.states = map[string]models.ScopeState{
		scope.Key("wow", "us"): {ScopeKey: scope.Key("wow", "us"), State: scope.StateMixed, StateSince: 1},
	}

	subscribeRespContent(t, f, subscribeBody(`{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}`))
	if len(f.scopes.PutCalls) != 0 {
		t.Fatalf("existing baseline must not be overwritten, got %d puts", len(f.scopes.PutCalls))
	}
}

func TestResolveSubscribeTarget(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)
	mapping, err := f.handler.loadServerMapping(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		game       string
		region     string
		server     string
		wantPK     string
		wantLabel  string
		wantScope  string
		wantError  bool
	}{
		{name: "region scope", game: "wow", region: "us", wantPK: "region#wow#us", wantLabel: "All WoW servers — US", wantScope: "region"},
		{name: "server scope", game: "wow", region: "us", server: "illidan", wantPK: "battlenet#us#57", wantLabel: "wow-us-illidan"},
		{name: "server without region", game: "wow", server: "illidan", wantError: true},
		{name: "missing region", game: "wow", wantError: true},
		{name: "unknown region", game: "wow", region: "eu", wantError: true},
		{name: "missing game", wantError: true},
		{name: "unknown game", game: "bogus", wantError: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, resp := f.handler.resolveSubscribeTarget(mapping, tc.game, tc.region, tc.server)
			if tc.wantError {
				if resp == nil {
					t.Fatalf("expected error response, got %+v", got)
				}
				return
			}
			if resp != nil {
				t.Fatalf("unexpected error response: %s", resp.Body)
			}
			if got.pk != tc.wantPK || got.label != tc.wantLabel || got.scopeType != tc.wantScope {
				t.Fatalf("got %+v, want pk=%q label=%q scope=%q", got, tc.wantPK, tc.wantLabel, tc.wantScope)
			}
		})
	}
}
