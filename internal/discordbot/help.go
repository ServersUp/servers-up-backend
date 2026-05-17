package discordbot

import (
	"log/slog"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleHelp() (events.LambdaFunctionURLResponse, error) {
	slog.Info("help requested")
	msg := strings.Join([]string{
		"**ServersUp Discord Bot — Help**",
		"",
		"Full info: https://serversup.github.io",
		"",
		"**Commands**",
		"- `/subscribe game:<game> server:<server> [role:<role>]` — subscribe this channel to server status updates (type to search **game** and **server**; pick **role** from Discord’s role picker)",
		"- `/unsubscribe subscription:<subscription>` — remove one subscription anywhere in **this guild** (autocomplete shows game, server, role, and channel name; type to search)",
		"- `/subscriptions` — list all subscriptions in this guild, grouped by channel",
		"- `/games` — list supported games from configuration",
		"- `/servers game:<game>` — list servers available for a game (type to search **game**)",
		"- `/status game:<game> server:<server>` — show the current UP/DOWN status for a server",
		"- `/help` — show this message",
		"",
		"**Tips**",
		"- Game + server names are case-insensitive. Spaces/underscores are treated like hyphens (e.g. `Area 52` → `area-52`).",
		"- `/status` is rate-limited per user (and per guild) to keep lookups fast for everyone.",
		"- Run `/subscriptions` to see what’s configured; `/unsubscribe` uses the same entries (including which channel each row is in).",
	}, "\n")
	return h.discordResponse(msg)
}
