package discordbot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleGames(ctx context.Context) (events.LambdaFunctionURLResponse, error) {
	slog.Info("games list requested")
	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}
	games := mapping.ListGames()
	if len(games) == 0 {
		return h.discordResponse("No games are configured yet.")
	}
	var b strings.Builder
	b.WriteString("**Supported games**\n")
	for _, g := range games {
		b.WriteString("- `")
		b.WriteString(g)
		b.WriteString("`\n")
	}
	return h.discordResponse(strings.TrimRight(b.String(), "\n"))
}
