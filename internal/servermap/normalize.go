package servermap

import (
	"regexp"
	"sort"
	"strings"
)

var sepRe = regexp.MustCompile(`[-_\s]+`)

// NormalizeKey normalizes user input to the slug keys used in server-mapping.json.
// Examples:
// - "Area 52" -> "area-52"
// - "  Illidan " -> "illidan"
func NormalizeKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}

	s = sepRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// Some games have a user-facing name that differs from the internal game ID. The ID must stay
// hyphen-free because labels are game-region-server, so the friendly name is an alias only:
// accepted as input and shown in labels and lists. Add new aliases here.
var (
	gameDisplayByID = map[string]string{"forever": "wow-forever"}
	gameIDByDisplay = map[string]string{"wow-forever": "forever"}
)

// NormalizeGameKey normalizes user input for a game and resolves a display alias to the game ID.
func NormalizeGameKey(s string) string {
	k := NormalizeKey(s)
	if id, ok := gameIDByDisplay[k]; ok {
		return id
	}
	return k
}

// DisplayGameKey returns the user-facing name for a game ID (the ID itself unless aliased).
func DisplayGameKey(gameID string) string {
	if d, ok := gameDisplayByID[gameID]; ok {
		return d
	}
	return gameID
}

// DisplayGameKeys returns the user-facing names of the given game IDs, sorted.
func DisplayGameKeys(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = DisplayGameKey(id)
	}
	sort.Strings(out)
	return out
}

// SplitDisplayLabel splits "game-region-server" into its parts, aware of hyphenated display
// game names such as "wow-forever". ok is false when the label has fewer than three parts.
func SplitDisplayLabel(label string) (game, region, server string, ok bool) {
	game = ""
	rest := label
	for display := range gameIDByDisplay {
		if r, found := strings.CutPrefix(label, display+"-"); found {
			game, rest = display, r
			break
		}
	}
	if game == "" {
		g, r, found := strings.Cut(label, "-")
		if !found {
			return "", "", "", false
		}
		game, rest = g, r
	}
	region, server, ok = strings.Cut(rest, "-")
	if !ok || server == "" {
		return "", "", "", false
	}
	return game, region, server, true
}
