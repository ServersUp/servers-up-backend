package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
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

func TestHandleRequest_success_singleMessage(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := &Handler{discord: md}

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
	h := &Handler{discord: md}

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

func TestHandleRequest_invalidJSON_marksFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := &Handler{discord: md}

	ev := events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "bad", Body: `{not-json}`},
		},
	}
	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "bad" {
		t.Fatalf("expected failure for bad, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 0 {
		t.Fatalf("expected no discord calls, got %d", len(md.calls))
	}
}

func TestHandleRequest_missingFields_marksFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{}
	h := &Handler{discord: md}

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
	if len(resp.BatchItemFailures) != 3 {
		t.Fatalf("expected 3 failures, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 0 {
		t.Fatalf("expected no discord calls, got %d", len(md.calls))
	}
}

func TestHandleRequest_partialFailure_onlyFailsBadMessage(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{
		sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
			if channelID == "badchan" {
				return errors.New("discord error")
			}
			return nil
		},
	}
	h := &Handler{discord: md}

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
	if len(md.calls) != 2 {
		t.Fatalf("expected 2 discord calls, got %d", len(md.calls))
	}
}

func TestProcessRecord_propagatesDiscordError(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return errors.New("boom")
	}}
	h := &Handler{discord: md}

	err := h.processRecord(context.Background(), events.SQSMessage{
		MessageId: "m1",
		Body:      `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatDiscordContent(t *testing.T) {
	t.Parallel()

	got := formatDiscordContent(models.GuildNotifyJob{ServerID: "s", Status: "UP"})
	if !strings.Contains(got, "`s`") || !strings.Contains(got, "**UP**") {
		t.Fatalf("unexpected: %q", got)
	}

	got = formatDiscordContent(models.GuildNotifyJob{ServerID: "s", Status: "DOWN", RoleID: "1"})
	if !strings.HasPrefix(got, "<@&1> ") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestHandleRequest_discordError_marksFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
		return errors.New("discord down")
	}}
	h := &Handler{discord: md}

	ev := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "m1", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`},
	}}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "m1" {
		t.Fatalf("expected m1 failure, got %+v", resp.BatchItemFailures)
	}
}

func TestHandleRequest_noRecords(t *testing.T) {
	t.Parallel()
	h := &Handler{discord: &mockDiscord{}}
	resp, err := h.HandleRequest(context.Background(), events.SQSEvent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.BatchItemFailures)
	}
}

func TestHandleRequest_stillContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	md := &mockDiscord{
		sendFunc: func(ctx context.Context, channelID, content, roleID string) error {
			if strings.Contains(content, "**DOWN**") {
				return errors.New("nope")
			}
			return nil
		},
	}
	h := &Handler{discord: md}

	ev := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "a", Body: `{"serverId":"x","status":"DOWN","guildId":"g","channelId":"c","roleId":""}`},
		{MessageId: "b", Body: `{"serverId":"x","status":"UP","guildId":"g","channelId":"c","roleId":""}`},
	}}

	resp, err := h.HandleRequest(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "a" {
		t.Fatalf("expected only a failed, got %+v", resp.BatchItemFailures)
	}
	if len(md.calls) != 2 {
		t.Fatalf("expected both attempted, got %d", len(md.calls))
	}
}

var _ DiscordClient = (*mockDiscord)(nil)

