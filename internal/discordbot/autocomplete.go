package discordbot

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) handleAutocomplete(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	focused := findFocusedOption(data.Options)
	if focused == nil {
		return h.autocompleteResponse(nil)
	}

	const maxChoices = 25
	switch data.Name {
	case "subscribe":
		mapping, err := h.loadServerMapping(ctx)
		if err != nil {
			slog.Error("autocomplete: failed to load server mapping", "error", err)
			return h.autocompleteResponse(nil)
		}
		switch focused.Name {
		case "game":
			partial := optionStringValue(focused)
			games := mapping.ListGames()
			matches := filterSortedKeysPrefix(games, partial, maxChoices)
			return h.autocompleteResponse(keysToAutocompleteChoices(matches))
		case "server":
			gameNorm := servermap.NormalizeKey(h.getOption(data.Options, "game"))
			if gameNorm == "" {
				return h.autocompleteResponse(nil)
			}
			servers, err := mapping.ListServers(gameNorm)
			if err != nil {
				return h.autocompleteResponse(nil)
			}
			partial := optionStringValue(focused)
			matches := filterSortedKeysPrefix(servers, partial, maxChoices)
			return h.autocompleteResponse(keysToAutocompleteChoices(matches))
		default:
			return h.autocompleteResponse(nil)
		}
	case "servers":
		if focused.Name != "game" {
			return h.autocompleteResponse(nil)
		}
		mapping, err := h.loadServerMapping(ctx)
		if err != nil {
			slog.Error("autocomplete: failed to load server mapping", "error", err)
			return h.autocompleteResponse(nil)
		}
		partial := optionStringValue(focused)
		games := mapping.ListGames()
		matches := filterSortedKeysPrefix(games, partial, maxChoices)
		return h.autocompleteResponse(keysToAutocompleteChoices(matches))
	case "status":
		mapping, err := h.loadServerMapping(ctx)
		if err != nil {
			slog.Error("autocomplete: failed to load server mapping", "error", err)
			return h.autocompleteResponse(nil)
		}
		switch focused.Name {
		case "game":
			partial := optionStringValue(focused)
			games := mapping.ListGames()
			matches := filterSortedKeysPrefix(games, partial, maxChoices)
			return h.autocompleteResponse(keysToAutocompleteChoices(matches))
		case "server":
			gameNorm := servermap.NormalizeKey(h.getOption(data.Options, "game"))
			if gameNorm == "" {
				return h.autocompleteResponse(nil)
			}
			servers, err := mapping.ListServers(gameNorm)
			if err != nil {
				return h.autocompleteResponse(nil)
			}
			partial := optionStringValue(focused)
			matches := filterSortedKeysPrefix(servers, partial, maxChoices)
			return h.autocompleteResponse(keysToAutocompleteChoices(matches))
		default:
			return h.autocompleteResponse(nil)
		}
	case "unsubscribe":
		if focused.Name != "subscription" {
			return h.autocompleteResponse(nil)
		}
		mapping, err := h.loadServerMapping(ctx)
		if err != nil {
			slog.Error("autocomplete: failed to load server mapping", "error", err)
			return h.autocompleteResponse(nil)
		}
		subs, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
		if err != nil {
			slog.Error("autocomplete: failed to list subscriptions", "error", err)
			return h.autocompleteResponse(nil)
		}
		sort.Slice(subs, func(i, j int) bool {
			if subs[i].ChannelID != subs[j].ChannelID {
				return subs[i].ChannelID < subs[j].ChannelID
			}
			if subs[i].ServerID != subs[j].ServerID {
				return subs[i].ServerID < subs[j].ServerID
			}
			return subs[i].Mention < subs[j].Mention
		})
		partial := optionStringValue(focused)
		choices := h.subscriptionChoicesForQuery(ctx, interaction.GuildID, mapping, subs, partial, maxChoices)
		return h.autocompleteResponse(choices)
	default:
		return h.autocompleteResponse(nil)
	}
}

func (h *Handler) subscriptionChoicesForQuery(ctx context.Context, guildID string, mapping servermap.Mapping, subs []models.Subscription, partial string, max int) []discord.ApplicationCommandOptionChoice {
	q := strings.ToLower(strings.TrimSpace(partial))
	out := make([]discord.ApplicationCommandOptionChoice, 0, max)
	for _, sub := range subs {
		label := h.subscriptionUnsubscribeChoiceText(ctx, guildID, mapping, sub)
		if q != "" && !strings.Contains(strings.ToLower(label), q) {
			continue
		}
		name := label
		if len(name) > 100 {
			name = name[:97] + "..."
		}
		out = append(out, discord.ApplicationCommandOptionChoice{
			Name:  name,
			Value: sub.SubscriptionID,
		})
		if len(out) >= max {
			break
		}
	}
	return out
}
