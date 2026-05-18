package discordbot

import (
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/aws/aws-lambda-go/events"
)

const subscriptionPermissionDenied = "You need **Manage Channels** or **Administrator** to manage server status subscriptions in this server."

func (h *Handler) requireSubscriptionPermission(interaction discord.Interaction) (events.LambdaFunctionURLResponse, bool) {
	if interaction.GuildID == "" {
		return events.LambdaFunctionURLResponse{}, true
	}
	if interaction.Member == nil {
		resp, _ := h.discordResponseEphemeral(subscriptionPermissionDenied)
		return resp, false
	}
	if discord.CanManageSubscriptions(interaction.Member.Permissions) {
		return events.LambdaFunctionURLResponse{}, true
	}
	resp, err := h.discordResponseEphemeral(subscriptionPermissionDenied)
	if err != nil {
		return events.LambdaFunctionURLResponse{}, false
	}
	return resp, false
}
