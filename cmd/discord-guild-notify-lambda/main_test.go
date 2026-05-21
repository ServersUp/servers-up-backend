package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

type mockDiscord struct {
	sendFunc func(ctx context.Context, channelID, content, roleID string) error
	calls    []discordCall
}

type discordCall struct {
	channelID string
	content   string
	roleID    string
}

func (m *mockDiscord) SendChannelMessage(ctx context.Context, channelID, content, roleID string) error {
	m.calls = append(m.calls, discordCall{channelID: channelID, content: content, roleID: roleID})
	if m.sendFunc != nil {
		return m.sendFunc(ctx, channelID, content, roleID)
	}
	return nil
}

func newTestHandler(discordClient DiscordClient, mapping servermap.Mapping) *Handler {
	cache := servermap.NewCachedMapping(time.Hour)
	cache.Seed(mapping)
	return &Handler{discord: discordClient, mappingCache: cache}
}

func TestHandleRequest_success_singleMessage(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "m1", Body: `{"serverId":"battlenet#us#11","status":"DOWN","guildId":"g","channelId":"c","roleId":""}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 1 {
		t.Fatalf("expected 1 discord call, got %d", len(md.calls))
	}
	if md.calls[0].channelID != "c" {
		t.Fatalf("channelID=%q", md.calls[0].channelID)
	}
	if !strings.Contains(md.calls[0].content, "battlenet#us#11") || !strings.Contains(md.calls[0].content, "**DOWN**") {
		t.Fatalf("unexpected content: %q", md.calls[0].content)
	}
}

func TestHandleRequest_success_withRoleMention(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "m1", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":"777"}`},
		},
	}
	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 1 {
		t.Fatalf("expected 1 discord call, got %d", len(md.calls))
	}
	if !strings.HasPrefix(md.calls[0].content, "<@&777> ") {
		t.Fatalf("expected role mention prefix, got %q", md.calls[0].content)
	}
	if md.calls[0].roleID != "777" {
		t.Fatalf("expected roleID passed to client, got %q", md.calls[0].roleID)
	}
}

func TestHandleRequest_usesHumanServerNameWhenMappingAvailable(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := newTestHandler(md, servermap.Mapping{
		Games: map[string]servermap.Game{
			"wow": {
				Provider: "battlenet",
				Servers: map[string]servermap.Server{
					"illidan": {Region: "us", Identifier: 57},
				},
			},
		},
	})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "m1", Body: `{"serverId":"battlenet#us#57","status":"DOWN","guildId":"g","channelId":"c","roleId":""}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
	if !strings.Contains(md.calls[0].content, "wow-illidan") {
		t.Fatalf("expected game+server in content, got %q", md.calls[0].content)
	}
}

func TestHandleRequest_prefersJobServerLabelOverHumanLabel(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	// Mapping resolves battlenet#us#57 to "wipe-b" — different from the subscribed label.
	h := newTestHandler(md, servermap.Mapping{
		Games: map[string]servermap.Game{
			"wipe": {
				Provider: "battlenet",
				Servers: map[string]servermap.Server{
					"b": {Region: "us", Identifier: 57},
				},
			},
		},
	})

	// Job carries the label captured at subscribe time.
	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "m1", Body: `{"serverId":"battlenet#us#57","status":"DOWN","guildId":"g","channelId":"c","serverLabel":"wow-illidan"}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
	if !strings.Contains(md.calls[0].content, "wow-illidan") {
		t.Fatalf("expected job serverLabel in content, got %q", md.calls[0].content)
	}
	if strings.Contains(md.calls[0].content, "wipe-b") {
		t.Fatalf("should not fall back to HumanLabel when serverLabel is set, got %q", md.calls[0].content)
	}
}

func TestHandleRequest_invalidJSON_ackDeletes(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "bad", Body: `{not-json}`},
		},
	}
	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures (ack-delete), got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 0 {
		t.Fatalf("expected no discord calls, got %d", len(md.calls))
	}
}

func TestHandleRequest_missingFields_ackDeletes(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "m1", Body: `{"serverId":"","status":"UP","guildId":"g","channelId":"c"}`},
			{MessageId: "m2", Body: `{"serverId":"x","status":"","guildId":"g","channelId":"c"}`},
			{MessageId: "m3", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":""}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures (ack-delete), got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 0 {
		t.Fatalf("expected no discord calls, got %d", len(md.calls))
	}
}

func TestHandleRequest_discord403_ackDeletes(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return &discord.APIError{StatusCode: 403, Body: "forbidden"}
	}}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "m1", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`},
	}}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures for permanent 403, got %+v", resp.BatchItemFailures)
	}
}

func TestHandleRequest_discord429_marksFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return &discord.APIError{StatusCode: 429, Body: "rate limited"}
	}}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "m1", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`},
	}}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "m1" {
		t.Fatalf("expected m1 failure for 429, got %+v", resp.BatchItemFailures)
	}
}

func TestHandleRequest_discord500_marksFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return &discord.APIError{StatusCode: 500, Body: "error"}
	}}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "m1", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`},
	}}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "m1" {
		t.Fatalf("expected m1 failure for 500, got %+v", resp.BatchItemFailures)
	}
}

func TestHandleRequest_partialBatch_403And429(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{
		sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
			switch channelID {
			case "perm":
				return &discord.APIError{StatusCode: 403, Body: "forbidden"}
			case "rate":
				return &discord.APIError{StatusCode: 429, Body: "rate limited"}
			default:
				return nil
			}
		},
	}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "perm", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"perm","roleId":""}`},
			{MessageId: "rate", Body: `{"serverId":"x","status":"DOWN","guildId":"g","channelId":"rate","roleId":""}`},
			{MessageId: "ok", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"good","roleId":""}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "rate" {
		t.Fatalf("expected only rate in failures, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 3 {
		t.Fatalf("expected 3 discord calls, got %d", len(md.calls))
	}
}

func TestHandleRequest_partialFailure_networkErrorRetries(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{
		sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
			if channelID == "badchan" {
				return errors.New("discord error")
			}
			return nil
		},
	}
	h := newTestHandler(md, servermap.Mapping{})

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "ok", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"good","roleId":""}`},
			{MessageId: "fail", Body: `{"serverId":"x","status":"DOWN","guildId":"g","channelId":"badchan","roleId":""}`},
		},
	}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "fail" {
		t.Fatalf("expected only 'fail' marked, got %+v", resp.BatchItemFailures)
	}
}

func TestProcessRecord_propagatesRetryableDiscordError(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return &discord.APIError{StatusCode: 503, Body: "unavailable"}
	}}
	h := newTestHandler(md, servermap.Mapping{})

	err := h.processRecord(context.Background(), events.SQSMessage{
		MessageId: "m1",
		Body:      `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessRecord_ackDeletesPermanentDiscordError(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return &discord.APIError{StatusCode: 404, Body: "not found"}
	}}
	h := newTestHandler(md, servermap.Mapping{})

	err := h.processRecord(context.Background(), events.SQSMessage{
		MessageId: "m1",
		Body:      `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`,
	})
	if err != nil {
		t.Fatalf("expected nil for permanent 404, got %v", err)
	}
}

func TestFormatDiscordContent(t *testing.T) {
	t.Parallel()

	got := formatDiscordContent(models.GuildNotifyJob{ServerID: "s", Status: "UP"}, "s")
	if !strings.Contains(got, "**s**") || !strings.Contains(got, "**UP**") {
		t.Fatalf("unexpected: %q", got)
	}

	got = formatDiscordContent(models.GuildNotifyJob{ServerID: "s", Status: "DOWN", RoleID: "1"}, "s")
	if !strings.HasPrefix(got, "<@&1> ") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestHandleRequest_noRecords(t *testing.T) {
	t.Parallel()
	h := newTestHandler(&mockDiscord{}, servermap.Mapping{})
	resp, err := h.HandleRequest(context.Background(), events.SQSEvent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
}

func TestSqsQueueNameFromURL(t *testing.T) {
	t.Parallel()
	if got := sqsQueueNameFromURL("https://sqs.us-east-1.amazonaws.com/123/discord-guild-notify-jobs-dlq"); got != "discord-guild-notify-jobs-dlq" {
		t.Fatalf("got %q", got)
	}
	if sqsQueueNameFromURL("") != "" {
		t.Fatal("expected empty")
	}
}

func TestDiscordHTTPClient_returnsAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Missing Access"}`))
	}))
	t.Cleanup(srv.Close)

	client := &discordHTTPClient{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		botToken:   "test",
	}
	err := client.SendChannelMessage(context.Background(), "ch", "hello", "")
	apiErr, ok := discord.AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T %v", err, err)
	}
	if apiErr.StatusCode != 403 || !apiErr.Permanent() {
		t.Fatalf("unexpected: %+v permanent=%v", apiErr, apiErr.Permanent())
	}
}

func TestDiscordHTTPClient_503Retryable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := &discordHTTPClient{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		botToken:   "test",
	}
	err := client.SendChannelMessage(context.Background(), "ch", "hello", "")
	apiErr, ok := discord.AsAPIError(err)
	if !ok || !apiErr.Retryable() {
		t.Fatalf("expected retryable APIError, got %v", err)
	}
}

func TestEmitNotifySendError_skipsNonHTTP(t *testing.T) {
	t.Parallel()
	// smoke: must not panic
	emitNotifySendError(0)
	emitNotifySendError(399)
	emitNotifySendError(600)
}

func TestHandleDiscordSendError_classification(t *testing.T) {
	t.Parallel()
	h := &Handler{}

	err := h.handleDiscordSendError(
		models.GuildNotifyJob{ServerID: "s", Status: "UP", GuildID: "g", ChannelID: "c"},
		"mid",
		fmt.Errorf("wrap: %w", &discord.APIError{StatusCode: 403}),
	)
	if err != nil {
		t.Fatalf("403 should ack-delete: %v", err)
	}

	err = h.handleDiscordSendError(
		models.GuildNotifyJob{ServerID: "s", Status: "UP", GuildID: "g", ChannelID: "c"},
		"mid",
		&discord.APIError{StatusCode: 429},
	)
	if err == nil {
		t.Fatal("429 should retry")
	}
}

var _ DiscordClient = (*mockDiscord)(nil)
