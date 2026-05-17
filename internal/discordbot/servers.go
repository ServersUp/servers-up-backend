package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleServers(ctx context.Context, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	gameName := servermap.NormalizeKey(rawGame)

	slog.Info("servers list requested", "rawGame", rawGame, "gameName", gameName)

	if gameName == "" {
		return h.discordResponse("Missing `game`. Start typing in **game** to search, or use `/help`.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	servers, err := mapping.ListServers(gameName)
	if err != nil {
		return h.discordResponse(h.formatLookupError(mapping, err, gameName, ""))
	}

	if len(servers) == 0 {
		return h.discordResponse(fmt.Sprintf("No servers are configured for game `%s`.", gameName))
	}

	if len(servers) > maxInlineServerNames {
		return h.discordResponse(formatLongServerListMessage(gameName, servers))
	}

	return h.discordResponse(formatInlineServerListMessage(gameName, servers))
}

func formatInlineServerListMessage(game string, servers []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Servers for `%s`** (%d)\n", game, len(servers))
	for _, s := range servers {
		b.WriteString("- `")
		b.WriteString(s)
		b.WriteString("`\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatLongServerListMessage(game string, allServers []string) string {
	present := make(map[string]struct{}, len(allServers))
	for _, s := range allServers {
		present[s] = struct{}{}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "The **`%s`** server list is too long to show here (%d servers).\n\n", game, len(allServers))

	if game == "wow" {
		var popular []string
		for _, key := range wowPopularServerKeys {
			if _, ok := present[key]; ok {
				popular = append(popular, key)
			}
		}
		if len(popular) > 0 {
			b.WriteString("**Popular US realms** (use these with `/subscribe` and `/status`):\n")
			for _, s := range popular {
				b.WriteString("- `")
				b.WriteString(s)
				b.WriteString("`\n")
			}
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "Browse the full list: %s", supportedGamesListURL)
	return b.String()
}
