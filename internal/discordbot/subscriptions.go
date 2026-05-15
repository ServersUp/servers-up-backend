package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleListSubscriptions(ctx context.Context, interaction discord.Interaction) (events.LambdaFunctionURLResponse, error) {
	slog.Info("subscriptions list requested", "guildID", interaction.GuildID, "channelID", interaction.ChannelID)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	subs, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for guild", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to list subscriptions. Please try again later.")
	}
	if len(subs) == 0 {
		slog.Info("subscriptions list resolved (empty)", "guildID", interaction.GuildID)
		return h.discordResponse("No subscriptions found for this guild.")
	}
	slog.Info("subscriptions list resolved", "guildID", interaction.GuildID, "count", len(subs))

	// Group by channel, then sort for stable output.
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
		entries := byChannel[ch]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].ServerID == entries[j].ServerID {
				return entries[i].Mention < entries[j].Mention
			}
			return entries[i].ServerID < entries[j].ServerID
		})

		for _, sub := range entries {
			human := mapping.HumanLabel(sub.ServerID)
			if sub.Mention != "" {
				lines = append(lines, fmt.Sprintf("- `%s` %s", human, sub.Mention))
			} else {
				lines = append(lines, fmt.Sprintf("- `%s`", human))
			}
		}
	}

	content := strings.Join(lines, "\n")
	if len(content) > 1900 {
		slog.Warn("subscriptions list truncated for discord limit",
			"guildID", interaction.GuildID,
			"length", len(content),
		)
		content = content[:1900] + "\n\n(truncated)"
	}
	slog.Info("subscriptions list response built",
		"guildID", interaction.GuildID,
		"channels", len(channelIDs),
		"length", len(content),
	)
	return h.discordResponse(content)
}
