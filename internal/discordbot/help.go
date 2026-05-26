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
		"- `/subscribe game:<game> region:<region> server:<server> [role:<role>]` — subscribe this channel to server status updates (pick **game**, then **region**, then **server**; optional **role** from Discord’s role picker)",
		"- `/unsubscribe subscription:<subscription>` — remove one subscription anywhere in **this guild** (autocomplete shows game, region, server, role, and channel)",
		"- `/subscriptions` — list all subscriptions in this guild, grouped by channel (labels like `wow-us-illidan`)",
		"- `/games` — list supported games from configuration",
		"- `/regions game:<game>` — list regions available for a game (e.g. `us`, `eu`, `kr`, `tw`)",
		"- `/servers game:<game> region:<region>` — list servers for a game in a region",
		"- `/status game:<game> region:<region> server:<server>` — show current UP/DOWN status for a server",
		"- `/help` — show this message",
		"",
		"**Tips**",
		"- Game, region, and server names are case-insensitive. Spaces/underscores are treated like hyphens (e.g. `Area 52` → `area-52`).",
		"- `/status` is rate-limited per user (and per guild) to keep lookups fast for everyone.",
		"- Run `/subscriptions` to see what’s configured; `/unsubscribe` uses the same entries (including region and channel).",
	}, "\n")
	return h.discordResponse(msg)
}
