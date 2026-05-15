package discordbot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
)

func (h *Handler) handleSubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	rawServer := h.getOption(data.Options, "server")
	roleID := h.getOption(data.Options, "role")

	gameName := servermap.NormalizeKey(rawGame)
	serverName := servermap.NormalizeKey(rawServer)

	slog.Info("subscribe request received",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"rawGame", rawGame,
		"rawServer", rawServer,
		"gameName", gameName,
		"serverName", serverName,
	)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, game, serverKey, server, lookupErr := mapping.Lookup(gameName, serverName)
	if lookupErr != nil {
		slog.Warn("subscribe request lookup failed",
			"error", lookupErr,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"rawGame", rawGame,
			"rawServer", rawServer,
			"gameName", gameName,
			"serverName", serverName,
		)
		return h.discordResponse(h.formatLookupError(mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

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
		if e.ChannelID == interaction.ChannelID && e.ServerID == technicalID && e.Mention == mention {
			displayRole := roleName
			if displayRole == "" {
				displayRole = e.RoleName
			}
			return h.discordResponse(h.alreadySubscribedMessage(ctx, interaction.GuildID, interaction.ChannelID, gameID, serverKey, displayRole, mention))
		}
	}

	slog.Info("subscribe request resolved",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"provider", game.Provider,
		"region", server.Region,
		"serverKey", serverKey,
		"serverIdentifier", fmt.Sprint(server.Identifier),
		"technicalServerID", technicalID,
	)

	sub := models.Subscription{
		ServerID:       technicalID,
		SubscriptionID: uuid.New().String(),
		GuildID:        interaction.GuildID,
		ChannelID:      interaction.ChannelID,
		Mention:        mention,
		RoleName:       roleName,
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
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"serverKey", serverKey,
		"technicalServerID", technicalID,
	)

	chLabel := h.channelPretty(ctx, interaction.GuildID, interaction.ChannelID)
	humanKey := fmt.Sprintf("%s-%s", gameID, serverKey)
	if roleName != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed @%s to **%s** server status updates in %s.", roleName, humanKey, chLabel))
	}
	if mention != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed with a role mention to **%s** server status updates in %s.", humanKey, chLabel))
	}
	return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** server status updates in %s.", humanKey, chLabel))
}
