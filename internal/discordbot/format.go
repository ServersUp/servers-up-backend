package discordbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

// subscriptionServerLabel returns the human-readable server label for a subscription,
// preferring the label captured at subscribe time over a live reverse-mapping lookup.
func subscriptionServerLabel(mapping servermap.Mapping, sub models.Subscription) string {
	if sub.ServerLabel != "" {
		return sub.ServerLabel
	}
	return mapping.HumanLabel(sub.ServerID)
}

func (h *Handler) subscriptionDisplayLabel(mapping servermap.Mapping, sub models.Subscription) string {
	human := subscriptionServerLabel(mapping, sub)
	if sub.RoleName != "" {
		return fmt.Sprintf("%s @%s", human, sub.RoleName)
	}
	if sub.Mention != "" {
		return fmt.Sprintf("%s %s", human, sub.Mention)
	}
	return human
}

// subscriptionUnsubscribeChoiceText is shown in autocomplete only (no subscription IDs; role as @Name when known).
func (h *Handler) subscriptionUnsubscribeChoiceText(ctx context.Context, guildID string, mapping servermap.Mapping, sub models.Subscription) string {
	game, server := splitGameServerHuman(subscriptionServerLabel(mapping, sub))
	role := h.subscriptionRoleDisplay(sub)
	ch := h.channelPretty(ctx, guildID, sub.ChannelID)
	return fmt.Sprintf("%s · %s · %s · in %s", game, server, role, ch)
}

func splitGameServerHuman(human string) (game, server string) {
	game, server, ok := strings.Cut(human, "-")
	if !ok || server == "" {
		return human, human
	}
	return game, server
}

func (h *Handler) subscriptionRoleDisplay(sub models.Subscription) string {
	if sub.RoleName != "" {
		return "@" + sub.RoleName
	}
	if sub.Mention != "" {
		return "role mention"
	}
	return "channel-wide"
}

func (h *Handler) alreadySubscribedMessage(ctx context.Context, guildID, channelID, gameID, serverKey, roleName, mention string) string {
	human := fmt.Sprintf("%s-%s", gameID, serverKey)
	ch := h.channelPretty(ctx, guildID, channelID)
	switch {
	case roleName != "":
		return fmt.Sprintf("Already subscribed — **%s** in %s with @%s.", human, ch, roleName)
	case mention != "":
		return fmt.Sprintf("Already subscribed — **%s** in %s with a role mention.", human, ch)
	default:
		return fmt.Sprintf("Already subscribed — **%s** in %s.", human, ch)
	}
}
