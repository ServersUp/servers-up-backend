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

func (h *Handler) handleRegions(ctx context.Context, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	gameName := servermap.NormalizeKey(rawGame)
	slog.Info("regions list requested", "rawGame", rawGame, "gameName", gameName)

	if gameName == "" {
		return h.discordResponse("Missing `game`. Start typing in **game** to search, or use `/help`.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	regions, err := mapping.ListRegions(gameName)
	if err != nil {
		return h.discordResponse(h.formatLookupError(mapping, err, gameName, "", ""))
	}
	if len(regions) == 0 {
		return h.discordResponse(fmt.Sprintf("No regions are configured for game `%s`.", gameName))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Regions for `%s`** (%d)\n", gameName, len(regions))
	for _, r := range regions {
		b.WriteString("- `")
		b.WriteString(r)
		b.WriteString("`\n")
	}
	return h.discordResponse(strings.TrimRight(b.String(), "\n"))
}
