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

const dmBodyTemplate = `{"type": 2, "channel_id": "dmch-1", "user": {"id": "user-9"}, "data": {"name": "%s", "options": [%s]}}`

func dmSubscribeBody(name string, opts string) string {
	return fmt.Sprintf(dmBodyTemplate, name, opts)
}

func dmServerOpts() string {
	return `{"name": "game", "value": "wow"}, {"name": "region", "value": "us"}, {"name": "server", "value": "illidan"}`
}

func dmContent(t *testing.T, respBody string) string {
	t.Helper()
	var resp discord.InteractionResponse
	if err := json.Unmarshal([]byte(respBody), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data == nil {
		t.Fatal("expected response data")
	}
	return resp.Data.Content
}

func TestHandleRequest_DMSubscribe(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var stored models.Subscription
	addCalled := false
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		if guildID != dmGuildID("user-9") {
			t.Errorf("expected list for dm guild id, got %q", guildID)
		}
		return nil, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalled = true
		stored = sub
		return nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscribe", dmServerOpts())))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !addCalled {
		t.Fatal("expected AddSubscription to be called")
	}
	if stored.GuildID != dmGuildID("user-9") {
		t.Errorf("expected guild id %q, got %q", dmGuildID("user-9"), stored.GuildID)
	}
	if stored.TargetType != targetTypeDM {
		t.Errorf("expected target type %q, got %q", targetTypeDM, stored.TargetType)
	}
	if stored.UserID != "user-9" {
		t.Errorf("expected user id %q, got %q", "user-9", stored.UserID)
	}
	if stored.ChannelID != "dmch-1" {
		t.Errorf("expected channel id %q, got %q", "dmch-1", stored.ChannelID)
	}
	if stored.Mention != "" || stored.RoleName != "" {
		t.Errorf("expected no role mention for DM, got mention=%q roleName=%q", stored.Mention, stored.RoleName)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "Subscribed you to **wow-us-illidan** status updates in your DMs.") {
		t.Fatalf("unexpected DM subscribe message: %q", content)
	}
}

func TestHandleRequest_DMSubscribeIgnoresRole(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var stored models.Subscription
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return nil, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		stored = sub
		return nil
	}

	opts := dmServerOpts() + `, {"name": "role", "value": "42"}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscribe", opts)))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mention != "" || stored.RoleName != "" {
		t.Errorf("role must be ignored in DMs, got mention=%q roleName=%q", stored.Mention, stored.RoleName)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHandleRequest_DMSubscribeDuplicate(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	addCalls := 0
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: dmGuildID("user-9"), ChannelID: "other-dmch", TargetType: targetTypeDM, UserID: "user-9", ServerLabel: "wow-us-illidan"},
		}, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalls++
		return nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscribe", dmServerOpts())))
	if err != nil {
		t.Fatal(err)
	}
	if addCalls != 0 {
		t.Fatalf("expected AddSubscription not called, got %d calls", addCalls)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "You already receive **wow-us-illidan** updates in your DMs") {
		t.Fatalf("unexpected duplicate message: %q", content)
	}
}

func TestHandleRequest_DMSubscribeCap(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	existing := make([]models.Subscription, 0, maxDMSubscriptions)
	for i := 0; i < maxDMSubscriptions; i++ {
		existing = append(existing, models.Subscription{
			ServerID:   fmt.Sprintf("battlenet#us#%d", 100+i),
			GuildID:    dmGuildID("user-9"),
			ChannelID:  "dmch-1",
			TargetType: targetTypeDM,
			UserID:     "user-9",
		})
	}
	addCalls := 0
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return existing, nil
	}
	f.db.AddFunc = func(ctx context.Context, sub models.Subscription) error {
		addCalls++
		return nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscribe", dmServerOpts())))
	if err != nil {
		t.Fatal(err)
	}
	if addCalls != 0 {
		t.Fatalf("expected AddSubscription not called, got %d calls", addCalls)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, fmt.Sprintf("maximum of %d DM subscriptions", maxDMSubscriptions)) {
		t.Fatalf("unexpected cap message: %q", content)
	}
}

func TestHandleRequest_DMSubscriptions(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		if guildID != dmGuildID("user-9") {
			t.Errorf("expected list for dm guild id, got %q", guildID)
		}
		return []models.Subscription{
			{ServerID: "battlenet#us#57", GuildID: dmGuildID("user-9"), ChannelID: "dmch-1", TargetType: targetTypeDM, UserID: "user-9", ServerLabel: "wow-us-illidan"},
		}, nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscriptions", "")))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "**Your DM subscriptions**") {
		t.Fatalf("expected DM header, got %q", content)
	}
	if !strings.Contains(content, "wow-us-illidan") {
		t.Fatalf("expected server label, got %q", content)
	}
}

func TestHandleRequest_DMSubscriptionsEmpty(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return nil, nil
	}

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("subscriptions", "")))
	if err != nil {
		t.Fatal(err)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "No subscriptions found for your DMs.") {
		t.Fatalf("unexpected empty message: %q", content)
	}
}

func TestHandleRequest_DMUnsubscribe(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	var deletedGuild, deletedChannel, deletedSubID string
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		return []models.Subscription{
			{SubscriptionID: "sub-1", ServerID: "battlenet#us#57", GuildID: dmGuildID("user-9"), ChannelID: "dmch-old", TargetType: targetTypeDM, UserID: "user-9", ServerLabel: "wow-us-illidan"},
		}, nil
	}
	f.db.DeleteFunc = func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
		deletedGuild, deletedChannel, deletedSubID = guildID, channelID, subscriptionID
		return nil
	}

	opts := `{"name": "subscription", "value": "sub-1"}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("unsubscribe", opts)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if deletedGuild != dmGuildID("user-9") {
		t.Errorf("expected delete guild %q, got %q", dmGuildID("user-9"), deletedGuild)
	}
	if deletedChannel != "dmch-old" {
		t.Errorf("expected delete channel %q, got %q", "dmch-old", deletedChannel)
	}
	if deletedSubID != "sub-1" {
		t.Errorf("expected delete subscription %q, got %q", "sub-1", deletedSubID)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "Unsubscribed from **wow-us-illidan** server status updates in your DMs.") {
		t.Fatalf("unexpected unsubscribe message: %q", content)
	}
}

func TestHandleRequest_DMUnsubscribeIsolation(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	deleteCalls := 0
	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		// Only the requesting user's DM subscriptions are visible.
		if guildID != dmGuildID("user-9") {
			t.Errorf("expected list for dm guild id, got %q", guildID)
		}
		return []models.Subscription{
			{SubscriptionID: "sub-mine", ServerID: "battlenet#us#57", GuildID: dmGuildID("user-9"), ChannelID: "dmch-1", TargetType: targetTypeDM, UserID: "user-9"},
		}, nil
	}
	f.db.DeleteFunc = func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
		deleteCalls++
		return nil
	}

	// Trying to unsubscribe another user's subscription id must fail to match.
	opts := `{"name": "subscription", "value": "sub-other-user"}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, dmSubscribeBody("unsubscribe", opts)))
	if err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 0 {
		t.Fatalf("expected DeleteSubscription not called, got %d calls", deleteCalls)
	}
	content := dmContent(t, resp.Body)
	if !strings.Contains(content, "not found in your DMs") {
		t.Fatalf("unexpected isolation message: %q", content)
	}
}

func TestHandleRequest_DMAutocompleteUnsubscribe(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	f.db.ListFunc = func(ctx context.Context, guildID string) ([]models.Subscription, error) {
		if guildID != dmGuildID("user-9") {
			t.Errorf("expected list for dm guild id, got %q", guildID)
		}
		return []models.Subscription{
			{SubscriptionID: "sub-1", ServerID: "battlenet#us#57", GuildID: dmGuildID("user-9"), ChannelID: "dmch-1", TargetType: targetTypeDM, UserID: "user-9", ServerLabel: "wow-us-illidan"},
		}, nil
	}

	body := `{"type": 4, "channel_id": "dmch-1", "user": {"id": "user-9"}, "data": {"name": "unsubscribe", "options": [{"name": "subscription", "value": "illidan", "focused": true}]}}`
	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var autocompleteResp discord.InteractionResponse
	if err := json.Unmarshal([]byte(resp.Body), &autocompleteResp); err != nil {
		t.Fatal(err)
	}
	if autocompleteResp.Data == nil {
		t.Fatal("expected autocomplete data")
	}
	choices := autocompleteResp.Data.Choices
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	if choices[0].Value != "sub-1" {
		t.Errorf("expected choice value sub-1, got %q", choices[0].Value)
	}
	if !strings.Contains(choices[0].Name, "wow-us-illidan") || !strings.Contains(choices[0].Name, "in your DMs") {
		t.Errorf("unexpected choice name: %q", choices[0].Name)
	}
}
