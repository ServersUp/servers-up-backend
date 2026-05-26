package discordbot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
)

func (h *Handler) handleSubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	if resp, ok := h.requireSubscriptionPermission(interaction); !ok {
		return resp, nil
	}

	rawGame := h.getOption(data.Options, "game")
	rawRegion := h.getOption(data.Options, "region")
	rawServer := h.getOption(data.Options, "server")
	roleID := h.getOption(data.Options, "role")

	gameName := servermap.NormalizeKey(rawGame)
	regionName := servermap.NormalizeKey(rawRegion)
	serverName := servermap.NormalizeKey(rawServer)

	slog.Info("subscribe request received",
		"interactionId", interaction.ID,
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"rawGame", rawGame,
		"rawRegion", rawRegion,
		"rawServer", rawServer,
		"gameName", gameName,
		"regionName", regionName,
		"serverName", serverName,
	)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, regionKey, serverKey, game, server, lookupErr := mapping.Lookup(gameName, regionName, serverName)
	if lookupErr != nil {
		slog.Warn("subscribe request lookup failed",
			"error", lookupErr,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"gameName", gameName,
			"regionName", regionName,
			"serverName", serverName,
		)
		return h.discordResponse(h.formatLookupError(mapping, lookupErr, gameName, regionName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, regionKey, server.Identifier)

	mention := ""
	if roleID != "" {
		mention = fmt.Sprintf("<@&%s>", roleID)
	}

	roleName := ""
	if roleID != "" && h.discordBotToken != "" {
		if n, err := discord.GuildRoleName(ctx, h.httpClient, h.discordBotToken, interaction.GuildID, roleID); err != nil {
			slog.Warn("could not resolve Discord role name", "error", err, "roleID", roleID)
		} else {
			roleName = n
		}
	}

	existing, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for duplicate check", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to verify subscription. Please try again later.")
	}
	for _, e := range existing {
		if e.ChannelID == interaction.ChannelID && e.ServerID == technicalID {
			humanLabel := servermap.DisplayLabel(gameID, regionKey, serverKey)
			return h.discordResponse(h.alreadySubscribedMessage(ctx, interaction.GuildID, interaction.ChannelID, humanLabel, e.RoleName, e.Mention))
		}
	}

	slog.Info("subscribe request resolved",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"provider", game.Provider,
		"regionKey", regionKey,
		"serverKey", serverKey,
		"serverIdentifier", fmt.Sprint(server.Identifier),
		"technicalServerID", technicalID,
	)

	serverLabel := servermap.DisplayLabel(gameID, regionKey, serverKey)
	sub := models.Subscription{
		ServerID:       technicalID,
		SubscriptionID: uuid.New().String(),
		GuildID:        interaction.GuildID,
		ChannelID:      interaction.ChannelID,
		Mention:        mention,
		RoleName:       roleName,
		ServerLabel:    serverLabel,
	}

	if err := h.database.AddSubscription(ctx, sub); err != nil {
		slog.Error("failed to add subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"gameID", gameID,
			"serverKey", serverKey,
			"technicalServerID", technicalID,
		)
		return h.discordResponse("Failed to create subscription. Please try again later.")
	}

	slog.Info("subscription created",
		"interactionId", interaction.ID,
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"serverKey", serverKey,
		"technicalServerID", technicalID,
	)

	metrics.EmitCount(metrics.Namespace, "SubscriptionWrite", map[string]string{"command": "subscribe"}, 1)

	chLabel := h.channelPretty(ctx, interaction.GuildID, interaction.ChannelID)
	if roleName != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed @%s to **%s** server status updates in %s.", roleName, serverLabel, chLabel))
	}
	if mention != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed with a role mention to **%s** server status updates in %s.", serverLabel, chLabel))
	}
	return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** server status updates in %s.", serverLabel, chLabel))
}
