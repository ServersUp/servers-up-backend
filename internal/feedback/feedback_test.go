package feedback

import (
	"context"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		sub     Submission
		wantErr error
	}{
		{"valid feedback website", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "hello"}, nil},
		{"valid feedback discord", Submission{Kind: KindFeedback, Source: SourceDiscord, Message: "hi"}, nil},
		{"valid report website", Submission{Kind: KindReport, Source: SourceWebsite, Message: "bad", SubscriptionID: "sub1"}, nil},
		{"valid report discord", Submission{Kind: KindReport, Source: SourceDiscord, Message: "bad", SubscriptionID: "sub2"}, nil},
		{"missing kind", Submission{Source: SourceWebsite, Message: "x"}, ErrInvalidSubmission},
		{"unsupported kind", Submission{Kind: "other", Source: SourceWebsite, Message: "x"}, ErrInvalidSubmission},
		{"missing source", Submission{Kind: KindFeedback, Message: "x"}, ErrInvalidSubmission},
		{"unsupported source", Submission{Kind: KindFeedback, Source: "other", Message: "x"}, ErrInvalidSubmission},
		{"missing message", Submission{Kind: KindFeedback, Source: SourceWebsite}, ErrInvalidSubmission},
		{"whitespace message", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "   "}, ErrInvalidSubmission},
		{"report missing id", Submission{Kind: KindReport, Source: SourceWebsite, Message: "x"}, ErrInvalidSubscriptionID},
		{"report whitespace id", Submission{Kind: KindReport, Source: SourceWebsite, Message: "x", SubscriptionID: "  "}, ErrInvalidSubscriptionID},
		{"unicode boundary message", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: strings.Repeat("é", MaxMessageLength)}, nil},
		{"over-limit message", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: strings.Repeat("é", MaxMessageLength+1)}, ErrInvalidSubmission},
		{"unicode boundary id", Submission{Kind: KindReport, Source: SourceWebsite, Message: "x", SubscriptionID: strings.Repeat("é", MaxSubscriptionIDLen)}, nil},
		{"over-limit id", Submission{Kind: KindReport, Source: SourceWebsite, Message: "x", SubscriptionID: strings.Repeat("é", MaxSubscriptionIDLen+1)}, ErrInvalidSubscriptionID},
		{"feedback whitespace-only id allowed", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "x", SubscriptionID: "  "}, nil},
		{"feedback over-limit id", Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "x", SubscriptionID: strings.Repeat("é", MaxSubscriptionIDLen+1)}, ErrInvalidSubscriptionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.sub.Validate(); err != tc.wantErr {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSubjectSafety(t *testing.T) {
	if got := (Submission{Kind: KindFeedback}).Subject(); got != "Feedback" {
		t.Fatalf("feedback subject = %q", got)
	}
	if got := (Submission{Kind: KindReport}).Subject(); got != "Report" {
		t.Fatalf("report subject = %q", got)
	}
}

func TestMailContent(t *testing.T) {
	s := Submission{Kind: KindReport, Source: SourceDiscord, Message: "hello", SubscriptionID: "sub1"}
	want := "Kind: report\nSource: discord\n\nhello\n\nSubscription ID: sub1"
	if got := s.MailContent(); got != want {
		t.Fatalf("MailContent() = %q, want %q", got, want)
	}
	fb := Submission{Kind: KindFeedback, Source: SourceWebsite, Message: "hi", SubscriptionID: "  "}
	wantFB := "Kind: feedback\nSource: website\n\nhi"
	if got := fb.MailContent(); got != wantFB {
		t.Fatalf("MailContent() = %q, want %q", got, wantFB)
	}
}

type stubMailer struct{}

func (stubMailer) Send(context.Context, Submission) error { return nil }

var _ Mailer = stubMailer{}
