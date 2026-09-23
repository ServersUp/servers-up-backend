package discordbot

import (
	"context"
	"log/slog"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/feedback"
	"github.com/aws/aws-lambda-go/events"
)

// User-facing ephemeral messages for /feedback and /report. Each outcome
// returns a distinct fixed string so users can tell whether their submission
// was accepted, rejected as invalid, or failed to deliver. None of the
// messages echo submitted content, subscription IDs, or provider errors.
const (
	feedbackAcceptedMessage       = "Thanks — your submission was received."
	feedbackInvalidMessage        = "Sorry — your submission was incomplete or invalid. Please try again."
	feedbackDeliveryFailedMessage = "Sorry — we could not deliver your submission right now. Please try again later."
)

// handleFeedback processes the /feedback command. It reads the message option,
// validates the submission, and sends it through the mailer. Each outcome
// returns a generic ephemeral response.
func (h *Handler) handleFeedback(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	message := h.getOption(data.Options, "message")
	sub := feedback.Submission{
		Kind:    feedback.KindFeedback,
		Source:  feedback.SourceDiscord,
		Message: message,
	}
	if err := sub.Validate(); err != nil {
		slog.WarnContext(ctx, "discord feedback submission rejected")
		return h.discordResponseEphemeral(feedbackInvalidMessage)
	}
	if err := h.mailer.Send(ctx, sub); err != nil {
		slog.ErrorContext(ctx, "discord feedback submission failed to send")
		return h.discordResponseEphemeral(feedbackDeliveryFailedMessage)
	}
	slog.InfoContext(ctx, "discord feedback submission sent")
	return h.discordResponseEphemeral(feedbackAcceptedMessage)
}

// handleReport processes the /report command. It reads the message and
// subscription_id options, validates the submission, and sends it through the
// mailer. The subscription ID is passed through as data only; it is not
// verified against any store. Each outcome returns a generic ephemeral
// response.
func (h *Handler) handleReport(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	sub := feedback.Submission{
		Kind:           feedback.KindReport,
		Source:         feedback.SourceDiscord,
		Message:        h.getOption(data.Options, "message"),
		SubscriptionID: h.getOption(data.Options, "subscription_id"),
	}
	if err := sub.Validate(); err != nil {
		slog.WarnContext(ctx, "discord report submission rejected")
		return h.discordResponseEphemeral(feedbackInvalidMessage)
	}
	if err := h.mailer.Send(ctx, sub); err != nil {
		slog.ErrorContext(ctx, "discord report submission failed to send")
		return h.discordResponseEphemeral(feedbackDeliveryFailedMessage)
	}
	slog.InfoContext(ctx, "discord report submission sent")
	return h.discordResponseEphemeral(feedbackAcceptedMessage)
}
