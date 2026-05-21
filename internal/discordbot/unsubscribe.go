package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleUnsubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	if resp, ok := h.requireSubscriptionPermission(interaction); !ok {
		return resp, nil
	}

	subscriptionID := strings.TrimSpace(h.getOption(data.Options, "subscription"))
	slog.Info("unsubscribe request received",
		"interactionId", interaction.ID,
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"subscriptionID", subscriptionID,
	)

	if subscriptionID == "" {
		slog.Warn("unsubscribe request missing subscription",
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
		)
		return h.discordResponse("Choose a **subscription** (type to search), matching what `/subscriptions` shows for this guild.")
	}

	subs, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for unsubscribe", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to load subscriptions. Please try again later.")
	}

	var match *models.Subscription
	for i := range subs {
		s := &subs[i]
		if s.SubscriptionID == subscriptionID {
			match = s
			break
		}
	}
	if match == nil {
		slog.Warn("unsubscribe subscription id not found in guild",
			"guildID", interaction.GuildID,
			"subscriptionID", subscriptionID,
		)
		return h.discordResponse("That subscription was not found in this guild. Run `/subscriptions` and try again.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	human := subscriptionServerLabel(mapping, *match)
	slog.Info("unsubscribe request resolved",
		"guildID", interaction.GuildID,
		"requestedChannelID", interaction.ChannelID,
		"subscriptionChannelID", match.ChannelID,
		"serverID", match.ServerID,
		"humanServer", human,
		"mention", match.Mention,
		"roleName", match.RoleName,
		"subscriptionID", match.SubscriptionID,
	)

	if err := h.database.DeleteSubscription(ctx, interaction.GuildID, match.ChannelID, match.ServerID, match.SubscriptionID); err != nil {
		slog.Error("failed to delete subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"subscriptionID", match.SubscriptionID,
		)
		return h.discordResponse("An error occurred while trying to unsubscribe.")
	}

	chLabel := h.channelPretty(ctx, interaction.GuildID, match.ChannelID)
	if match.RoleName != "" {
		slog.Info("unsubscribe completed",
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"humanServer", human,
			"roleName", match.RoleName,
		)
		return h.discordResponse(fmt.Sprintf("Unsubscribed @%s from **%s** server status updates in %s.", match.RoleName, human, chLabel))
	}
	if match.Mention != "" {
		slog.Info("unsubscribe completed (role mention)",
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"humanServer", human,
		)
		return h.discordResponse(fmt.Sprintf("Unsubscribed from **%s** server status updates in %s (role mention).", human, chLabel))
	}
	slog.Info("unsubscribe completed (channel-wide)",
		"guildID", interaction.GuildID,
		"channelID", match.ChannelID,
		"serverID", match.ServerID,
		"humanServer", human,
	)
	return h.discordResponse(fmt.Sprintf("Unsubscribed from **%s** server status updates in %s.", human, chLabel))
}
