package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/feedback"
)

type fakeFeedbackMailer struct {
	sent    []feedback.Submission
	err     error
	sendErr error
}

func (f *fakeFeedbackMailer) Send(_ context.Context, sub feedback.Submission) error {
	f.sent = append(f.sent, sub)
	if f.sendErr != nil {
		return f.sendErr
	}
	return f.err
}

func feedbackInteractionBody(command string, options map[string]string) string {
	opts := make([]discord.InteractionOption, 0, len(options))
	for name, value := range options {
		opts = append(opts, discord.InteractionOption{
			Type:  3,
			Name:  name,
			Value: value,
		})
	}
	payload := map[string]any{
		"id":             "interaction-1",
		"application_id": "app-1",
		"type":           int(discord.InteractionTypeApplicationCommand),
		"data": map[string]any{
			"id":      "command-1",
			"name":    command,
			"type":    2,
			"options": opts,
		},
		"guild_id":   "guild-1",
		"channel_id": "channel-1",
		"token":      "token-1",
		"version":    10,
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func assertFeedbackResponse(t *testing.T, body string, wantEphemeral bool) discord.InteractionResponse {
	t.Helper()

	var resp discord.InteractionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Type != discord.InteractionResponseTypeChannelMessageWithSource {
		t.Fatalf("response type = %v, want %v", resp.Type, discord.InteractionResponseTypeChannelMessageWithSource)
	}
	if resp.Data == nil {
		t.Fatal("response data is nil")
	}
	if wantEphemeral && resp.Data.Flags != 64 {
		t.Fatalf("response flags = %d, want 64", resp.Data.Flags)
	}
	return resp
}

func TestHandleFeedbackValid(t *testing.T) {
	f := newTestHandlerFixture(t)
	mailer := &fakeFeedbackMailer{}
	f.handler.mailer = mailer

	req := f.signedRequest(t, feedbackInteractionBody("feedback", map[string]string{
		"message": "hello feedback",
	}))
	resp, err := f.handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	discordResp := assertFeedbackResponse(t, resp.Body, true)
	if discordResp.Data.Content != feedbackAcceptedMessage {
		t.Fatalf("accepted content = %q, want %q", discordResp.Data.Content, feedbackAcceptedMessage)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(mailer.sent))
	}
	if mailer.sent[0].Kind != feedback.KindFeedback {
		t.Fatalf("kind = %q, want %q", mailer.sent[0].Kind, feedback.KindFeedback)
	}
	if mailer.sent[0].Source != feedback.SourceDiscord {
		t.Fatalf("source = %q, want %q", mailer.sent[0].Source, feedback.SourceDiscord)
	}
	if mailer.sent[0].Message != "hello feedback" {
		t.Fatalf("message = %q, want %q", mailer.sent[0].Message, "hello feedback")
	}
	if strings.Contains(discordResp.Data.Content, "hello feedback") {
		t.Fatalf("response contains submitted message: %q", discordResp.Data.Content)
	}
}

func TestHandleReportValid(t *testing.T) {
	f := newTestHandlerFixture(t)
	mailer := &fakeFeedbackMailer{}
	f.handler.mailer = mailer

	req := f.signedRequest(t, feedbackInteractionBody("report", map[string]string{
		"message":         "hello report",
		"subscription_id": "sub-123",
	}))
	resp, err := f.handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	discordResp := assertFeedbackResponse(t, resp.Body, true)
	if discordResp.Data.Content != feedbackAcceptedMessage {
		t.Fatalf("accepted content = %q, want %q", discordResp.Data.Content, feedbackAcceptedMessage)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(mailer.sent))
	}
	if mailer.sent[0].Kind != feedback.KindReport {
		t.Fatalf("kind = %q, want %q", mailer.sent[0].Kind, feedback.KindReport)
	}
	if mailer.sent[0].Source != feedback.SourceDiscord {
		t.Fatalf("source = %q, want %q", mailer.sent[0].Source, feedback.SourceDiscord)
	}
	if mailer.sent[0].Message != "hello report" {
		t.Fatalf("message = %q, want %q", mailer.sent[0].Message, "hello report")
	}
	if mailer.sent[0].SubscriptionID != "sub-123" {
		t.Fatalf("subscription id = %q, want %q", mailer.sent[0].SubscriptionID, "sub-123")
	}
	if strings.Contains(discordResp.Data.Content, "hello report") {
		t.Fatalf("response contains submitted message: %q", discordResp.Data.Content)
	}
	if strings.Contains(discordResp.Data.Content, "sub-123") {
		t.Fatalf("response contains subscription id: %q", discordResp.Data.Content)
	}
}

func TestHandleFeedbackMissingMessageRejected(t *testing.T) {
	f := newTestHandlerFixture(t)
	mailer := &fakeFeedbackMailer{}
	f.handler.mailer = mailer

	req := f.signedRequest(t, feedbackInteractionBody("feedback", map[string]string{}))
	resp, err := f.handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	discordResp := assertFeedbackResponse(t, resp.Body, true)
	if discordResp.Data.Content != feedbackInvalidMessage {
		t.Fatalf("invalid content = %q, want %q", discordResp.Data.Content, feedbackInvalidMessage)
	}
	if discordResp.Data.Content == feedbackAcceptedMessage {
		t.Fatal("rejected response must differ from accepted response")
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("sent count = %d, want 0", len(mailer.sent))
	}
	if strings.Contains(discordResp.Data.Content, "message") {
		t.Fatalf("response contains message text: %q", discordResp.Data.Content)
	}
}

func TestHandleReportMissingSubscriptionIDRejected(t *testing.T) {
	f := newTestHandlerFixture(t)
	mailer := &fakeFeedbackMailer{}
	f.handler.mailer = mailer

	req := f.signedRequest(t, feedbackInteractionBody("report", map[string]string{
		"message": "hello report",
	}))
	resp, err := f.handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	discordResp := assertFeedbackResponse(t, resp.Body, true)
	if discordResp.Data.Content != feedbackInvalidMessage {
		t.Fatalf("invalid content = %q, want %q", discordResp.Data.Content, feedbackInvalidMessage)
	}
	if discordResp.Data.Content == feedbackAcceptedMessage {
		t.Fatal("rejected response must differ from accepted response")
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("sent count = %d, want 0", len(mailer.sent))
	}
	if strings.Contains(discordResp.Data.Content, "hello report") {
		t.Fatalf("response contains submitted message: %q", discordResp.Data.Content)
	}
}

func TestHandleFeedbackProviderFailure(t *testing.T) {
	f := newTestHandlerFixture(t)
	mailer := &fakeFeedbackMailer{sendErr: errors.New("provider failed")}
	f.handler.mailer = mailer

	req := f.signedRequest(t, feedbackInteractionBody("feedback", map[string]string{
		"message": "hello feedback",
	}))
	resp, err := f.handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	discordResp := assertFeedbackResponse(t, resp.Body, true)
	if discordResp.Data.Content != feedbackDeliveryFailedMessage {
		t.Fatalf("delivery-failed content = %q, want %q", discordResp.Data.Content, feedbackDeliveryFailedMessage)
	}
	if discordResp.Data.Content == feedbackAcceptedMessage {
		t.Fatal("delivery-failed response must differ from accepted response")
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(mailer.sent))
	}
	if strings.Contains(discordResp.Data.Content, "hello feedback") {
		t.Fatalf("response contains submitted message: %q", discordResp.Data.Content)
	}
	if strings.Contains(discordResp.Data.Content, "provider failed") {
		t.Fatalf("response contains provider error: %q", discordResp.Data.Content)
	}
}
