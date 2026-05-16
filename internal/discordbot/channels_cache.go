package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/discord"
)

func (h *Handler) channelPretty(ctx context.Context, guildID, channelID string) string {
	m, err := h.guildChannelNames(ctx, guildID)
	if err != nil {
		slog.Warn("discord: could not list guild channels", "error", err, "guildID", guildID)
		return fmt.Sprintf("<#%s>", channelID)
	}
	if m != nil {
		if n := m[channelID]; n != "" {
			return "#" + n
		}
	}
	return fmt.Sprintf("<#%s>", channelID)
}

func (h *Handler) guildChannelNames(ctx context.Context, guildID string) (map[string]string, error) {
	if h.discordBotToken == "" {
		return nil, nil
	}
	h.channelNamesMu.RLock()
	if h.channelNamesGuild == guildID && h.channelNamesByID != nil &&
		time.Since(h.channelNamesAt) < channelNamesCacheTTL {
		m := h.channelNamesByID
		h.channelNamesMu.RUnlock()
		return m, nil
	}
	h.channelNamesMu.RUnlock()

	names, err := discord.GuildChannelNames(ctx, h.httpClient, h.discordBotToken, guildID)
	if err != nil {
		return nil, err
	}
	h.channelNamesMu.Lock()
	h.channelNamesGuild = guildID
	h.channelNamesByID = names
	h.channelNamesAt = time.Now()
	h.channelNamesMu.Unlock()
	return names, nil
}
