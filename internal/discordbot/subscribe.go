package discordbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
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

	if serverName != "" && regionName == "" {
		slog.Warn("subscribe request has server without region",
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"serverName", serverName,
		)
		return h.discordResponse("The **server** option needs a **region**. Pick a region first, or leave both empty to watch **all servers** of a game.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	// Resolve the target (per-server, region scope, or game scope).
	target, resp := h.resolveSubscribeTarget(mapping, gameName, regionName, serverName)
	if resp != nil {
		return *resp, nil
	}

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
		if e.ChannelID == interaction.ChannelID && e.ServerID == target.pk {
			return h.discordResponse(h.alreadySubscribedMessage(ctx, interaction.GuildID, interaction.ChannelID, target.label, e.RoleName, e.Mention))
		}
	}

	slog.Info("subscribe request resolved",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"pk", target.pk,
		"scope", target.scopeType,
		"label", target.label,
	)

	sub := models.Subscription{
		ServerID:       target.pk,
		SubscriptionID: uuid.New().String(),
		GuildID:        interaction.GuildID,
		ChannelID:      interaction.ChannelID,
		Mention:        mention,
		RoleName:       roleName,
		ServerLabel:    target.label,
		Scope:          target.scopeType,
		GameID:         target.gameID,
		Region:         target.region,
	}

	if err := h.database.AddSubscription(ctx, sub); err != nil {
		slog.Error("failed to add subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"pk", target.pk,
		)
		return h.discordResponse("Failed to create subscription. Please try again later.")
	}

	if target.scopeType != "" {
		if err := h.ensureScopeBaseline(ctx, target.gameID, target.region, target.pk, mapping); err != nil {
			// The aggregator defensively baselines on its next run; never fail
			// the subscription for a baseline hiccup.
			slog.Warn("failed to ensure scope baseline", "error", err, "scopeKey", target.pk)
		}
	}

	slog.Info("subscription created",
		"interactionId", interaction.ID,
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"pk", target.pk,
		"scope", target.scopeType,
	)

	metrics.EmitCount(metrics.Namespace, "SubscriptionWrite", map[string]string{"command": "subscribe", "scope": target.scopeType}, 1)

	return h.subscribeSuccess(ctx, interaction.GuildID, interaction.ChannelID, target.label, target.scopeType, roleName, mention)
}

// subscribeTarget is a resolved subscription target.
type subscribeTarget struct {
	pk        string // partition key (concrete server ID or wildcard scope key)
	label     string // human label
	scopeType string // "", "region", or "game"
	gameID    string
	region    string
}

// resolveSubscribeTarget resolves the subscription target from the provided
// options. gameName is required; a server requires a region; with only a
// region the target is every server in that region; with neither the target is
// every server of the game.
func (h *Handler) resolveSubscribeTarget(mapping servermap.Mapping, gameName, regionName, serverName string) (subscribeTarget, *events.LambdaFunctionURLResponse) {
	if gameName == "" {
		msg := "A **game** is required. Run `/subscribe` and pick a game."
		return subscribeTarget{}, msgRespPtr(msg)
	}
	if _, ok := mapping.Games[gameName]; !ok {
		msg := "Unknown game. Run `/subscribe` and pick from the suggestions."
		return subscribeTarget{}, msgRespPtr(msg)
	}

	if serverName != "" {
		gameID, regionKey, serverKey, game, server, lookupErr := mapping.Lookup(gameName, regionName, serverName)
		if lookupErr != nil {
			msg := h.formatLookupError(mapping, lookupErr, gameName, regionName, serverName)
			return subscribeTarget{}, msgRespPtr(msg)
		}
		technicalID := serverid.Generate(game.Provider, regionKey, server.Identifier)
		return subscribeTarget{
			pk:        technicalID,
			label:     servermap.DisplayLabel(gameID, regionKey, serverKey),
			gameID:    gameID,
			region:    regionKey,
		}, nil
	}

	if regionName != "" {
		regions, err := mapping.ListRegions(gameName)
		if err != nil {
			msg := "Unknown game. Run `/subscribe` and pick from the suggestions."
			return subscribeTarget{}, msgRespPtr(msg)
		}
		valid := false
		for _, r := range regions {
			if r == regionName {
				valid = true
				break
			}
		}
		if !valid {
			msg := fmt.Sprintf("Unknown region **%s** for that game. Run `/subscribe` and pick from the suggestions.", strings.ToUpper(regionName))
			return subscribeTarget{}, msgRespPtr(msg)
		}
		return subscribeTarget{
			pk:        scope.Key(gameName, regionName),
			label:     scope.Label(gameName, regionName),
			scopeType: scope.TypeRegion,
			gameID:    gameName,
			region:    regionName,
		}, nil
	}

	return subscribeTarget{
		pk:        scope.Key(gameName, ""),
		label:     scope.Label(gameName, ""),
		scopeType: scope.TypeGame,
		gameID:    gameName,
	}, nil
}

// subscribeSuccess builds the confirmation message. Wildcard scopes mention the
// aggregate (all down / all up) behavior.
func (h *Handler) subscribeSuccess(ctx context.Context, guildID, channelID, label, scopeType, roleName, mention string) (events.LambdaFunctionURLResponse, error) {
	chLabel := h.channelPretty(ctx, guildID, channelID)
	aggNote := ""
	if scopeType != "" {
		aggNote = " You'll be notified once when they all go down and once when they all come back up."
	}
	switch {
	case roleName != "":
		return h.discordResponse(fmt.Sprintf("Subscribed @%s to **%s** status updates in %s.%s", roleName, label, chLabel, aggNote))
	case mention != "":
		return h.discordResponse(fmt.Sprintf("Subscribed with a role mention to **%s** status updates in %s.%s", label, chLabel, aggNote))
	default:
		return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** status updates in %s.%s", label, chLabel, aggNote))
	}
}

// msgResp builds a channel-message interaction response body without needing a
// Handler (used by the subscribe target resolver).
func msgResp(content string) events.LambdaFunctionURLResponse {
	body, _ := json.Marshal(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeChannelMessageWithSource,
		Data: &discord.InteractionResponseData{Content: content},
	})
	return events.LambdaFunctionURLResponse{StatusCode: http.StatusOK, Body: string(body)}
}

func msgRespPtr(content string) *events.LambdaFunctionURLResponse {
	r := msgResp(content)
	return &r
}
