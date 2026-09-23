package feedback

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
)

type fakeSES struct {
	calls   int
	lastCtx context.Context
	lastIn  *ses.SendEmailInput
	err     error
}

func (f *fakeSES) SendEmail(ctx context.Context, params *ses.SendEmailInput, _ ...func(*ses.Options)) (*ses.SendEmailOutput, error) {
	f.calls++
	f.lastCtx = ctx
	f.lastIn = params
	if f.err != nil {
		return nil, f.err
	}
	return &ses.SendEmailOutput{}, nil
}

func TestNewSESMailerRejectsNilClient(t *testing.T) {
	if _, err := NewSESMailer(nil, "a@example.com", "b@example.com"); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewSESMailerRejectsBlankFrom(t *testing.T) {
	if _, err := NewSESMailer(&fakeSES{}, "  ", "b@example.com"); err == nil {
		t.Fatal("expected error for blank from")
	}
}

func TestNewSESMailerRejectsBlankTo(t *testing.T) {
	if _, err := NewSESMailer(&fakeSES{}, "a@example.com", ""); err == nil {
		t.Fatal("expected error for blank to")
	}
}

func TestSendFeedbackPayloadMapping(t *testing.T) {
	f := &fakeSES{}
	m, err := NewSESMailer(f, "noreply@example.com", "ops@example.com")
	if err != nil {
		t.Fatalf("NewSESMailer: %v", err)
	}

	sub := Submission{
		Kind:    KindFeedback,
		Source:  SourceWebsite,
		Message: "hello world",
	}
	if err := m.Send(context.Background(), sub); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}
	in := f.lastIn
	if in == nil {
		t.Fatal("lastIn is nil")
	}
	if got := aws.ToString(in.Source); got != "noreply@example.com" {
		t.Errorf("Source = %q", got)
	}
	if in.Destination == nil || len(in.Destination.ToAddresses) != 1 || in.Destination.ToAddresses[0] != "ops@example.com" {
		t.Errorf("Destination.ToAddresses = %v", in.Destination)
	}
	if in.ReplyToAddresses != nil && len(in.ReplyToAddresses) != 0 {
		t.Errorf("ReplyToAddresses = %v", in.ReplyToAddresses)
	}
	if got := aws.ToString(in.Message.Subject.Data); got != "Feedback" {
		t.Errorf("Subject = %q", got)
	}
	if got := aws.ToString(in.Message.Subject.Charset); got != "UTF-8" {
		t.Errorf("Subject.Charset = %q", got)
	}
	wantBody := "Kind: feedback\nSource: website\n\nhello world"
	if got := aws.ToString(in.Message.Body.Text.Data); got != wantBody {
		t.Errorf("Body = %q, want %q", got, wantBody)
	}
	if got := aws.ToString(in.Message.Body.Text.Charset); got != "UTF-8" {
		t.Errorf("Body.Charset = %q", got)
	}
}

func TestSendReportPayloadMapping(t *testing.T) {
	f := &fakeSES{}
	m, err := NewSESMailer(f, "noreply@example.com", "ops@example.com")
	if err != nil {
		t.Fatalf("NewSESMailer: %v", err)
	}

	sub := Submission{
		Kind:           KindReport,
		Source:         SourceDiscord,
		Message:        "bad thing",
		SubscriptionID: "sub-123",
	}
	if err := m.Send(context.Background(), sub); err != nil {
		t.Fatalf("Send: %v", err)
	}

	in := f.lastIn
	if got := aws.ToString(in.Message.Subject.Data); got != "Report" {
		t.Errorf("Subject = %q", got)
	}
	wantBody := "Kind: report\nSource: discord\n\nbad thing\n\nSubscription ID: sub-123"
	if got := aws.ToString(in.Message.Body.Text.Data); got != wantBody {
		t.Errorf("Body = %q, want %q", got, wantBody)
	}
}

func TestSendInvalidSubmissionZeroCalls(t *testing.T) {
	f := &fakeSES{}
	m, err := NewSESMailer(f, "a@example.com", "b@example.com")
	if err != nil {
		t.Fatalf("NewSESMailer: %v", err)
	}

	invalid := Submission{Kind: "bogus", Source: SourceWebsite, Message: "x"}
	if err := m.Send(context.Background(), invalid); err == nil {
		t.Fatal("expected validation error")
	}
	if f.calls != 0 {
		t.Errorf("calls = %d, want 0", f.calls)
	}
}

func TestSendPropagatesCallerContext(t *testing.T) {
	f := &fakeSES{}
	m, err := NewSESMailer(f, "a@example.com", "b@example.com")
	if err != nil {
		t.Fatalf("NewSESMailer: %v", err)
	}

	ctx := context.WithValue(context.Background(), contextKey{}, "sentinel")
	sub := Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "ctx test"}
	if err := m.Send(ctx, sub); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if f.lastCtx != ctx {
		t.Error("context was not propagated")
	}
}

type contextKey struct{}

func TestSendWrappedProviderError(t *testing.T) {
	sentinel := errors.New("ses provider down")
	f := &fakeSES{err: sentinel}
	m, err := NewSESMailer(f, "a@example.com", "b@example.com")
	if err != nil {
		t.Fatalf("NewSESMailer: %v", err)
	}

	sub := Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "err test"}
	err = m.Send(context.Background(), sub)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is = false, err = %v", err)
	}
}
