package feedback

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Kind identifies the category of a submission.
type Kind string

const (
	KindFeedback Kind = "feedback"
	KindReport   Kind = "report"
)

// Source identifies where a submission originated.
type Source string

const (
	SourceWebsite Source = "website"
	SourceDiscord Source = "discord"
)

// Submission is a user-submitted feedback or report.
type Submission struct {
	Kind           Kind
	Source         Source
	Message        string
	SubscriptionID string
}

const (
	MaxMessageLength     = 4000
	MaxSubscriptionIDLen = 128
)

var (
	ErrInvalidSubmission     = errors.New("invalid submission")
	ErrInvalidSubscriptionID = errors.New("invalid subscription id")
)

// Mailer sends a validated submission.
type Mailer interface {
	Send(ctx context.Context, submission Submission) error
}

// Validate checks the submission for supported kind/source, non-empty
// message, rune-safe message length, and report subscription ID rules.
func (s Submission) Validate() error {
	switch s.Kind {
	case KindFeedback, KindReport:
	default:
		return ErrInvalidSubmission
	}
	switch s.Source {
	case SourceWebsite, SourceDiscord:
	default:
		return ErrInvalidSubmission
	}
	if strings.TrimSpace(s.Message) == "" {
		return ErrInvalidSubmission
	}
	if utf8.RuneCountInString(s.Message) > MaxMessageLength {
		return ErrInvalidSubmission
	}
	if id := strings.TrimSpace(s.SubscriptionID); id != "" {
		if utf8.RuneCountInString(s.SubscriptionID) > MaxSubscriptionIDLen {
			return ErrInvalidSubscriptionID
		}
	}
	if s.Kind == KindReport && strings.TrimSpace(s.SubscriptionID) == "" {
		return ErrInvalidSubscriptionID
	}
	return nil
}

// Subject returns the fixed email subject for the submission kind.
func (s Submission) Subject() string {
	if s.Kind == KindReport {
		return "Report"
	}
	return "Feedback"
}

// MailContent returns the email body with fixed Kind and Source labels,
// preserving the original message and original subscription ID text, and
// including the subscription ID only when it is non-empty after trimming.
func (s Submission) MailContent() string {
	var b strings.Builder
	b.WriteString("Kind: ")
	b.WriteString(string(s.Kind))
	b.WriteString("\nSource: ")
	b.WriteString(string(s.Source))
	b.WriteString("\n\n")
	b.WriteString(s.Message)
	if strings.TrimSpace(s.SubscriptionID) != "" {
		b.WriteString("\n\nSubscription ID: ")
		b.WriteString(s.SubscriptionID)
	}
	return b.String()
}
