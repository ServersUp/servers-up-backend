package discordbot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func (h *Handler) formatLookupError(mapping servermap.Mapping, err error, rawGame, rawRegion, rawServer string) string {
	switch {
	case errors.Is(err, servermap.ErrMissingGame):
		return "Missing `game`. Start typing in **game** to search, or use `/help`."
	case errors.Is(err, servermap.ErrMissingRegion):
		if rawGame == "" {
			return "Missing `region`. Choose **game** first, then pick **region**."
		}
		return fmt.Sprintf("Missing `region`. Choose **region** for game `%s`.", servermap.NormalizeKey(rawGame))
	case errors.Is(err, servermap.ErrMissingServer):
		if rawGame == "" || rawRegion == "" {
			return "Missing `server`. Choose **game** and **region** first, then type to search **server**."
		}
		return fmt.Sprintf("Missing `server`. Type to search **server** for `%s` (%s).", servermap.NormalizeKey(rawGame), servermap.NormalizeKey(rawRegion))
	case errors.Is(err, servermap.ErrUnknownGame):
		games := mapping.ListGames()
		if len(games) == 0 {
			return fmt.Sprintf("Unknown game `%s`.", servermap.NormalizeKey(rawGame))
		}
		if len(games) > 10 {
			games = games[:10]
		}
		return fmt.Sprintf("Unknown game `%s`. Examples you can try: %s.", servermap.NormalizeKey(rawGame), strings.Join(wrapBackticks(games), ", "))
	case errors.Is(err, servermap.ErrUnknownRegion):
		regions, _ := mapping.ListRegions(servermap.NormalizeKey(rawGame))
		if len(regions) == 0 {
			return fmt.Sprintf("Unknown region `%s` for game `%s`.", servermap.NormalizeKey(rawRegion), servermap.NormalizeKey(rawGame))
		}
		return fmt.Sprintf("Unknown region `%s` for game `%s`. Available: %s.", servermap.NormalizeKey(rawRegion), servermap.NormalizeKey(rawGame), strings.Join(wrapBackticks(regions), ", "))
	case errors.Is(err, servermap.ErrUnknownServer):
		return fmt.Sprintf("Unknown server `%s` for game `%s` (%s). Type to search **server** for that region.", servermap.NormalizeKey(rawServer), servermap.NormalizeKey(rawGame), servermap.NormalizeKey(rawRegion))
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
