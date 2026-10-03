package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleListSubscriptions(ctx context.Context, interaction discord.Interaction) (events.LambdaFunctionURLResponse, error) {
	isDM := isDMInteraction(interaction)
	userID := ""
	if isDM {
		userID = interaction.InvokerUserID()
		if userID == "" {
			slog.Warn("dm subscriptions list missing invoker user",
				"interactionId", interaction.ID,
				"channelID", interaction.ChannelID,
			)
			return h.discordResponse("I couldn't identify your Discord account. Please try again.")
		}
	}
	slog.Info("subscriptions list requested", "guildID", interaction.GuildID, "channelID", interaction.ChannelID, "dm", isDM, "userID", userID)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	storageGuildID := interaction.GuildID
	if isDM {
		storageGuildID = dmGuildID(userID)
	}

	subs, err := h.database.ListSubscriptionsByGuild(ctx, storageGuildID)
	if err != nil {
		slog.Error("failed to list subscriptions", "error", err, "guildID", storageGuildID, "dm", isDM)
		return h.discordResponse("Failed to list subscriptions. Please try again later.")
	}
	if len(subs) == 0 {
		slog.Info("subscriptions list resolved (empty)", "guildID", storageGuildID, "dm", isDM)
		if isDM {
			return h.discordResponse("No subscriptions found for your DMs.")
		}
		return h.discordResponse("No subscriptions found for this guild.")
	}
	slog.Info("subscriptions list resolved", "guildID", storageGuildID, "dm", isDM, "count", len(subs))

	lines := h.buildSubscriptionLines(ctx, mapping, isDM, subs)
	content := strings.Join(lines, "\n")
	if len(content) > 1900 {
		slog.Warn("subscriptions list truncated for discord limit",
			"guildID", storageGuildID,
			"dm", isDM,
			"length", len(content),
		)
		content = content[:1900] + "\n\n(truncated)"
	}
	slog.Info("subscriptions list response built",
		"guildID", storageGuildID,
		"dm", isDM,
		"length", len(content),
	)
	return h.discordResponse(content)
}

// buildSubscriptionLines formats the subscriptions list. Guild listings group
// by channel; DM listings are a single flat list for the user.
func (h *Handler) buildSubscriptionLines(ctx context.Context, mapping servermap.Mapping, isDM bool, subs []models.Subscription) []string {
	if isDM {
		sorted := sortSubscriptions(subs)
		lines := []string{"**Your DM subscriptions**"}
		for _, sub := range sorted {
			lines = append(lines, fmt.Sprintf("- `%s`", subscriptionServerLabel(mapping, sub)))
		}
		return lines
	}

	byChannel := map[string][]models.Subscription{}
	for _, sub := range subs {
		byChannel[sub.ChannelID] = append(byChannel[sub.ChannelID], sub)
	}
	channelIDs := make([]string, 0, len(byChannel))
	for ch := range byChannel {
		channelIDs = append(channelIDs, ch)
	}
	sort.Strings(channelIDs)

	lines := []string{"**Subscriptions for this guild**"}
	for _, ch := range channelIDs {
		lines = append(lines, fmt.Sprintf("**<#%s>**", ch))
		for _, sub := range sortSubscriptions(byChannel[ch]) {
			human := subscriptionServerLabel(mapping, sub)
			if sub.Mention != "" {
				lines = append(lines, fmt.Sprintf("- `%s` %s", human, sub.Mention))
			} else {
				lines = append(lines, fmt.Sprintf("- `%s`", human))
			}
		}
	}
	return lines
}

func sortSubscriptions(subs []models.Subscription) []models.Subscription {
	sorted := make([]models.Subscription, len(subs))
	copy(sorted, subs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ServerID == sorted[j].ServerID {
			return sorted[i].Mention < sorted[j].Mention
		}
		return sorted[i].ServerID < sorted[j].ServerID
	})
	return sorted
}
