package feedback

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
)

// SESEmailAPI is the subset of the SES client used by SESMailer.
type SESEmailAPI interface {
	SendEmail(ctx context.Context, params *ses.SendEmailInput, optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error)
}

// SESMailer sends validated submissions via Amazon SES.
type SESMailer struct {
	client SESEmailAPI
	from   string
	to     string
}

// NewSESMailer returns a SESMailer that sends from fromEmail to toEmail.
// It returns an error if client is nil or either address is blank.
func NewSESMailer(client SESEmailAPI, fromEmail, toEmail string) (*SESMailer, error) {
	if client == nil {
		return nil, fmt.Errorf("feedback: SES client must not be nil")
	}
	if strings.TrimSpace(fromEmail) == "" {
		return nil, fmt.Errorf("feedback: from address must not be blank")
	}
	if strings.TrimSpace(toEmail) == "" {
		return nil, fmt.Errorf("feedback: to address must not be blank")
	}
	return &SESMailer{client: client, from: fromEmail, to: toEmail}, nil
}

// Send validates the submission and delivers it via a single SES SendEmail call.
func (m *SESMailer) Send(ctx context.Context, submission Submission) error {
	if err := submission.Validate(); err != nil {
		return err
	}

	_, err := m.client.SendEmail(ctx, &ses.SendEmailInput{
		Source: aws.String(m.from),
		Destination: &types.Destination{
			ToAddresses: []string{
				m.to,
			},
		},
		Message: &types.Message{
			Subject: &types.Content{
				Data:    aws.String(submission.Subject()),
				Charset: aws.String("UTF-8"),
			},
			Body: &types.Body{
				Text: &types.Content{
					Data:    aws.String(submission.MailContent()),
					Charset: aws.String("UTF-8"),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("feedback: SES SendEmail: %w", err)
	}
	return nil
}
