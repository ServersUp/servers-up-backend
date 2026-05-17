package discordbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

// StatusStore reads current game server status from the status DynamoDB table.
type StatusStore interface {
	GetServerStatus(ctx context.Context, gameID, serverID string) (*models.GameServerStatus, error)
}

func (h *Handler) handleStatus(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	rawServer := h.getOption(data.Options, "server")
	gameName := servermap.NormalizeKey(rawGame)
	serverName := servermap.NormalizeKey(rawServer)

	slog.Info("status requested",
		"rawGame", rawGame,
		"rawServer", rawServer,
		"gameName", gameName,
		"serverName", serverName,
		"userId", interaction.InvokerUserID(),
		"guildId", interaction.GuildID,
	)

	if gameName == "" {
		return h.discordResponse("Missing `game`. Start typing in **game** to search, or use `/help`.")
	}
	if serverName == "" {
		return h.discordResponse("Missing `server`. Choose **game** first, then type to search **server**.")
	}

	if h.statusStore == nil {
		slog.Error("status store not configured (DDB_TABLE_NAME missing)")
		return h.discordResponse("System error: Status lookup is not configured right now. Please try again later.")
	}

	if h.statusLimiter != nil {
		if allowed, retryAfter := h.statusLimiter.Allow(interaction); !allowed {
			secs := int(math.Ceil(retryAfter.Seconds()))
			if secs < 1 {
				secs = 1
			}
			slog.Info("status rate limited", "userId", interaction.InvokerUserID(), "guildId", interaction.GuildID, "retryAfterSec", secs)
			unit := "seconds"
			if secs == 1 {
				unit = "second"
			}
			return h.discordResponseEphemeral(fmt.Sprintf("You're checking status too quickly. Try again in %d %s.", secs, unit))
		}
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, game, serverKey, server, lookupErr := mapping.Lookup(gameName, serverName)
	if lookupErr != nil {
		return h.discordResponse(h.formatLookupError(mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	var row *models.GameServerStatus
	if h.statusCache != nil {
		if cached, ok := h.statusCache.Get(gameID, technicalID); ok {
			row = cached
		}
	}
	if row == nil {
		row, err = h.statusStore.GetServerStatus(ctx, gameID, technicalID)
		if err != nil {
			if errors.Is(err, db.ErrServerStatusNotFound) {
				human := fmt.Sprintf("%s-%s", gameID, serverKey)
				return h.discordResponse(fmt.Sprintf("No status recorded yet for **%s**. The poller may not have run yet.", human))
			}
			slog.Error("failed to load server status", "error", err, "gameID", gameID, "serverID", technicalID)
			return h.discordResponse("Failed to load server status. Please try again later.")
		}
		if h.statusCache != nil {
			h.statusCache.Set(gameID, technicalID, row)
		}
	}

	human := fmt.Sprintf("%s-%s", gameID, serverKey)
	updated := formatStatusLastUpdated(row.LastUpdatedAt)
	return h.discordResponse(fmt.Sprintf("**%s** is **%s** (last changed %s).", human, row.Status, updated))
}

func formatStatusLastUpdated(unix int64) string {
	if unix <= 0 {
		return "unknown"
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04 UTC")
}
