package discordbot

import (
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
)

const (
	targetTypeDM       = "dm"
	dmGuildIDPrefix    = "dm#"
	maxDMSubscriptions = 25
)

func dmGuildID(userID string) string {
	return dmGuildIDPrefix + userID
}

func isDMGuildID(guildID string) bool {
	return strings.HasPrefix(guildID, dmGuildIDPrefix)
}

func dmUserIDFromGuildID(guildID string) string {
	return strings.TrimPrefix(guildID, dmGuildIDPrefix)
}

func isDMInteraction(interaction discord.Interaction) bool {
	return interaction.GuildID == ""
}
