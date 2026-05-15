package discordbot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func (h *Handler) formatLookupError(mapping servermap.Mapping, err error, rawGame, rawServer string) string {
	switch {
	case errors.Is(err, servermap.ErrMissingGame):
		return "Missing `game`. Start typing in **game** to search, or use `/help`."
	case errors.Is(err, servermap.ErrMissingServer):
		if rawGame == "" {
			return "Missing `server`. Choose **game** first, then type to search **server**."
		}
		return fmt.Sprintf("Missing `server`. Type to search **server** for game `%s`.", servermap.NormalizeKey(rawGame))
	case errors.Is(err, servermap.ErrUnknownGame):
		games := mapping.ListGames()
		if len(games) == 0 {
			return fmt.Sprintf("Unknown game `%s`.", servermap.NormalizeKey(rawGame))
		}
		if len(games) > 10 {
			games = games[:10]
		}
		return fmt.Sprintf("Unknown game `%s`. Examples you can try: %s.", servermap.NormalizeKey(rawGame), strings.Join(wrapBackticks(games), ", "))
	case errors.Is(err, servermap.ErrUnknownServer):
		return fmt.Sprintf("Unknown server `%s` for game `%s`. Type to search **server** for that game.", servermap.NormalizeKey(rawServer), servermap.NormalizeKey(rawGame))
	default:
		return "Invalid request. Use `/help` for usage."
	}
}

func wrapBackticks(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("`%s`", it))
	}
	return out
}
