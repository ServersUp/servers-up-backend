package discordbot

import (
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func findFocusedOption(opts []discord.InteractionOption) *discord.InteractionOption {
	for i := range opts {
		o := &opts[i]
		if o.Focused {
			return o
		}
		if nested := findFocusedOption(o.Options); nested != nil {
			return nested
		}
	}
	return nil
}

func optionStringValue(opt *discord.InteractionOption) string {
	if opt == nil {
		return ""
	}
	if s, ok := opt.Value.(string); ok {
		return s
	}
	return ""
}

// filterSortedKeysPrefix keeps sort order of keys; matches normalized key prefix (case-insensitive via NormalizeKey).
func filterSortedKeysPrefix(sortedKeys []string, partial string, max int) []string {
	if max <= 0 {
		return nil
	}
	q := servermap.NormalizeKey(partial)
	out := make([]string, 0, max)
	for _, k := range sortedKeys {
		kn := servermap.NormalizeKey(k)
		if q == "" || strings.HasPrefix(kn, q) {
			out = append(out, k)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func keysToAutocompleteChoices(keys []string) []discord.ApplicationCommandOptionChoice {
	out := make([]discord.ApplicationCommandOptionChoice, len(keys))
	for i, k := range keys {
		out[i] = discord.ApplicationCommandOptionChoice{Name: k, Value: k}
	}
	return out
}
