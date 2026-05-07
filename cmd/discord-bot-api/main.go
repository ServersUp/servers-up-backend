package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/google/uuid"
)

// Database defines the required interface for the subscription store.
type Database interface {
	AddSubscription(ctx context.Context, sub models.Subscription) error
	DeleteSubscription(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error
	ListSubscriptionsByGuild(ctx context.Context, guildID string) ([]models.Subscription, error)
}

// ConfigProvider defines the required interface for fetching configurations.
type ConfigProvider interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

// Handler manages the dependencies for the Discord Bot API.
type Handler struct {
	database         Database
	configProvider   ConfigProvider
	discordPublicKey string
	httpClient       *http.Client
	discordBotToken  string

	mappingMu       sync.RWMutex
	mappingCached   servermap.Mapping
	mappingCachedAt time.Time

	channelNamesMu    sync.RWMutex
	channelNamesGuild string
	channelNamesByID  map[string]string
	channelNamesAt    time.Time
}

const (
	defaultConfigBucket     = "serversup-config"
	defaultServerMappingKey = "server-mapping.json"
	mappingCacheTTL         = 60 * time.Second
	channelNamesCacheTTL    = 2 * time.Minute
)

func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	publicKeyPath := os.Getenv("DISCORD_BOT_PUBLIC_KEY_PATH")
	if publicKeyPath == "" {
		slog.Error("missing required env DISCORD_BOT_PUBLIC_KEY_PATH")
		os.Exit(1)
	}

	provider := config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg))
	publicKey, err := provider.GetSecret(ctx, publicKeyPath)
	if err != nil {
		slog.Error("failed to load discord public key from ssm", "error", err, "path", publicKeyPath)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: 12 * time.Second}
	var botToken string
	if p := os.Getenv("DISCORD_BOT_TOKEN_PATH"); p != "" {
		tok, err := provider.GetSecret(ctx, p)
		if err != nil {
			slog.Warn("DISCORD_BOT_TOKEN_PATH set but secret not loaded; role/channel labels may be limited", "error", err, "path", p)
		} else {
			botToken = tok
		}
	}

	return &Handler{
		database:         db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")),
		configProvider:   provider,
		discordPublicKey: publicKey,
		httpClient:       httpClient,
		discordBotToken:  botToken,
	}
}

func (h *Handler) HandleRequest(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// 1. Security Verification
	signature := request.Headers["x-signature-ed25519"]
	timestamp := request.Headers["x-signature-timestamp"]

	// The body may be base64 encoded by AWS if it contains special characters.
	// We must decode it to ensure we verify the exact bytes Discord signed.
	bodyBytes := []byte(request.Body)
	if request.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(request.Body)
		if err != nil {
			slog.Error("failed to decode base64 body", "error", err)
			return events.LambdaFunctionURLResponse{StatusCode: http.StatusBadRequest, Body: "Invalid encoding"}, nil
		}
		bodyBytes = decoded
	}

	// Log incoming request details for troubleshooting
	slog.Debug("Incoming Discord Request", "sig", signature, "ts", timestamp, "body", string(bodyBytes), "isBase64", request.IsBase64Encoded)

	if err := discord.VerifySignature(h.discordPublicKey, signature, timestamp, string(bodyBytes)); err != nil {
		slog.Warn("Invalid request signature", "error", err, "sig", signature, "ts", timestamp)
		return events.LambdaFunctionURLResponse{StatusCode: http.StatusUnauthorized, Body: "Invalid signature"}, nil
	}

	// 2. Parse Interaction
	var interaction discord.Interaction
	if err := json.Unmarshal(bodyBytes, &interaction); err != nil {
		slog.Error("failed to unmarshal interaction", "error", err)
		return events.LambdaFunctionURLResponse{StatusCode: http.StatusBadRequest, Body: "Invalid JSON"}, nil
	}

	// 3. Handle Interaction Types
	switch interaction.Type {
	case discord.InteractionTypePing:
		slog.Info("Handling Discord Ping (Type 1)")
		return h.jsonResponse(http.StatusOK, discord.InteractionResponse{Type: discord.InteractionResponseTypePong})

	case discord.InteractionTypeApplicationCommand:
		var data discord.InteractionData
		if err := json.Unmarshal(interaction.Data, &data); err != nil {
			return h.discordResponse("Sorry — I couldn’t parse that command payload. Please try again.")
		}

		slog.Info("Handling Slash Command", "command", data.Name, "guild", interaction.GuildID)

		switch data.Name {
		case "subscribe":
			return h.handleSubscribe(ctx, interaction, data)
		case "unsubscribe":
			return h.handleUnsubscribe(ctx, interaction, data)
		case "subscriptions":
			return h.handleListSubscriptions(ctx, interaction)
		case "help":
			return h.handleHelp()
		default:
			return h.discordResponse("Unknown command. Use `/help` to see what I can do.")
		}

	case discord.InteractionTypeApplicationCommandAutocomplete:
		var data discord.InteractionData
		if err := json.Unmarshal(interaction.Data, &data); err != nil {
			slog.Warn("autocomplete: failed to parse interaction data", "error", err)
			return h.autocompleteResponse(nil)
		}
		return h.handleAutocomplete(ctx, interaction, data)
	}

	return h.discordResponse("Unsupported interaction type.")
}

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

func (h *Handler) subscriptionDisplayLabel(mapping servermap.Mapping, sub models.Subscription) string {
	human := h.humanServerLabel(mapping, sub.ServerID)
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
	game, server := splitGameServerHuman(h.humanServerLabel(mapping, sub.ServerID))
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

func (h *Handler) channelPretty(ctx context.Context, guildID, channelID string) string {
	if m := h.guildChannelNames(ctx, guildID); m != nil {
		if n := m[channelID]; n != "" {
			return "#" + n
		}
	}
	return fmt.Sprintf("<#%s>", channelID)
}

func (h *Handler) guildChannelNames(ctx context.Context, guildID string) map[string]string {
	if h.discordBotToken == "" {
		return nil
	}
	h.channelNamesMu.RLock()
	if h.channelNamesGuild == guildID && h.channelNamesByID != nil &&
		time.Since(h.channelNamesAt) < channelNamesCacheTTL {
		m := h.channelNamesByID
		h.channelNamesMu.RUnlock()
		return m
	}
	h.channelNamesMu.RUnlock()

	names, err := discord.GuildChannelNames(ctx, h.httpClient, h.discordBotToken, guildID)
	if err != nil {
		slog.Warn("discord: could not list guild channels", "error", err, "guildID", guildID)
		return nil
	}
	h.channelNamesMu.Lock()
	h.channelNamesGuild = guildID
	h.channelNamesByID = names
	h.channelNamesAt = time.Now()
	h.channelNamesMu.Unlock()
	return names
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

func (h *Handler) autocompleteResponse(choices []discord.ApplicationCommandOptionChoice) (events.LambdaFunctionURLResponse, error) {
	if choices == nil {
		choices = []discord.ApplicationCommandOptionChoice{}
	}
	return h.jsonResponse(http.StatusOK, discord.InteractionResponse{
		Type: discord.InteractionResponseTypeApplicationCommandAutocompleteResult,
		Data: &discord.InteractionResponseData{Choices: choices},
	})
}

func (h *Handler) handleSubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	rawServer := h.getOption(data.Options, "server")
	roleID := h.getOption(data.Options, "role")

	gameName := servermap.NormalizeKey(rawGame)
	serverName := servermap.NormalizeKey(rawServer)

	slog.Info("subscribe request received",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"rawGame", rawGame,
		"rawServer", rawServer,
		"gameName", gameName,
		"serverName", serverName,
	)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, game, serverKey, server, lookupErr := mapping.Lookup(gameName, serverName)
	if lookupErr != nil {
		slog.Warn("subscribe request lookup failed",
			"error", lookupErr,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"rawGame", rawGame,
			"rawServer", rawServer,
			"gameName", gameName,
			"serverName", serverName,
		)
		return h.discordResponse(h.formatLookupError(mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	mention := ""
	if roleID != "" {
		mention = fmt.Sprintf("<@&%s>", roleID)
	}

	roleName := ""
	if roleID != "" && h.discordBotToken != "" {
		if n, err := discord.GuildRoleName(ctx, h.httpClient, h.discordBotToken, interaction.GuildID, roleID); err != nil {
			slog.Warn("could not resolve Discord role name", "error", err, "roleID", roleID)
		} else {
			roleName = n
		}
	}

	existing, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for duplicate check", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to verify subscription. Please try again later.")
	}
	for _, e := range existing {
		if e.ChannelID == interaction.ChannelID && e.ServerID == technicalID && e.Mention == mention {
			displayRole := roleName
			if displayRole == "" {
				displayRole = e.RoleName
			}
			return h.discordResponse(h.alreadySubscribedMessage(ctx, interaction.GuildID, interaction.ChannelID, gameID, serverKey, displayRole, mention))
		}
	}

	slog.Info("subscribe request resolved",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"provider", game.Provider,
		"region", server.Region,
		"serverKey", serverKey,
		"serverIdentifier", fmt.Sprint(server.Identifier),
		"technicalServerID", technicalID,
	)

	sub := models.Subscription{
		ServerID:       technicalID,
		SubscriptionID: uuid.New().String(),
		GuildID:        interaction.GuildID,
		ChannelID:      interaction.ChannelID,
		Mention:        mention,
		RoleName:       roleName,
	}

	if err := h.database.AddSubscription(ctx, sub); err != nil {
		slog.Error("failed to add subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"roleID", roleID,
			"gameID", gameID,
			"serverKey", serverKey,
			"technicalServerID", technicalID,
		)
		return h.discordResponse("Failed to create subscription. Please try again later.")
	}

	slog.Info("subscription created",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"roleID", roleID,
		"gameID", gameID,
		"serverKey", serverKey,
		"technicalServerID", technicalID,
	)

	metrics.EmitCount("ServersUp/Backend", "SubscriptionAdded", map[string]string{"gameId": gameID}, 1)

	chLabel := h.channelPretty(ctx, interaction.GuildID, interaction.ChannelID)
	humanKey := fmt.Sprintf("%s-%s", gameID, serverKey)
	if roleName != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed @%s to **%s** server status updates in %s.", roleName, humanKey, chLabel))
	}
	if mention != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed with a role mention to **%s** server status updates in %s.", humanKey, chLabel))
	}
	return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** server status updates in %s.", humanKey, chLabel))
}

func (h *Handler) handleUnsubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	subscriptionID := strings.TrimSpace(h.getOption(data.Options, "subscription"))
	slog.Info("unsubscribe request received",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
		"subscriptionID", subscriptionID,
	)

	if subscriptionID == "" {
		slog.Warn("unsubscribe request missing subscription",
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
		)
		return h.discordResponse("Choose a **subscription** (type to search), matching what `/subscriptions` shows for this guild.")
	}

	subs, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for unsubscribe", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to load subscriptions. Please try again later.")
	}

	var match *models.Subscription
	for i := range subs {
		s := &subs[i]
		if s.SubscriptionID == subscriptionID {
			match = s
			break
		}
	}
	if match == nil {
		slog.Warn("unsubscribe subscription id not found in guild",
			"guildID", interaction.GuildID,
			"subscriptionID", subscriptionID,
		)
		return h.discordResponse("That subscription was not found in this guild. Run `/subscriptions` and try again.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	human := h.humanServerLabel(mapping, match.ServerID)
	slog.Info("unsubscribe request resolved",
		"guildID", interaction.GuildID,
		"requestedChannelID", interaction.ChannelID,
		"subscriptionChannelID", match.ChannelID,
		"serverID", match.ServerID,
		"humanServer", human,
		"mention", match.Mention,
		"roleName", match.RoleName,
		"subscriptionID", match.SubscriptionID,
	)

	if err := h.database.DeleteSubscription(ctx, interaction.GuildID, match.ChannelID, match.ServerID, match.SubscriptionID); err != nil {
		slog.Error("failed to delete subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"subscriptionID", match.SubscriptionID,
		)
		return h.discordResponse("An error occurred while trying to unsubscribe.")
	}

	gameID, _ := splitGameServerHuman(human)
	metrics.EmitCount("ServersUp/Backend", "SubscriptionRemoved", map[string]string{"gameId": gameID}, 1)

	chLabel := h.channelPretty(ctx, interaction.GuildID, match.ChannelID)
	if match.RoleName != "" {
		slog.Info("unsubscribe completed",
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"humanServer", human,
			"roleName", match.RoleName,
		)
		return h.discordResponse(fmt.Sprintf("Unsubscribed @%s from **%s** server status updates in %s.", match.RoleName, human, chLabel))
	}
	if match.Mention != "" {
		slog.Info("unsubscribe completed (role mention)",
			"guildID", interaction.GuildID,
			"channelID", match.ChannelID,
			"serverID", match.ServerID,
			"humanServer", human,
		)
		return h.discordResponse(fmt.Sprintf("Unsubscribed from **%s** server status updates in %s (role mention).", human, chLabel))
	}
	slog.Info("unsubscribe completed (channel-wide)",
		"guildID", interaction.GuildID,
		"channelID", match.ChannelID,
		"serverID", match.ServerID,
		"humanServer", human,
	)
	return h.discordResponse(fmt.Sprintf("Unsubscribed from **%s** server status updates in %s.", human, chLabel))
}

func (h *Handler) handleListSubscriptions(ctx context.Context, interaction discord.Interaction) (events.LambdaFunctionURLResponse, error) {
	slog.Info("subscriptions list requested", "guildID", interaction.GuildID, "channelID", interaction.ChannelID)

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	subs, err := h.database.ListSubscriptionsByGuild(ctx, interaction.GuildID)
	if err != nil {
		slog.Error("failed to list subscriptions for guild", "error", err, "guildID", interaction.GuildID)
		return h.discordResponse("Failed to list subscriptions. Please try again later.")
	}
	if len(subs) == 0 {
		slog.Info("subscriptions list resolved (empty)", "guildID", interaction.GuildID)
		return h.discordResponse("No subscriptions found for this guild.")
	}
	slog.Info("subscriptions list resolved", "guildID", interaction.GuildID, "count", len(subs))

	// Group by channel, then sort for stable output.
	byChannel := map[string][]models.Subscription{}
	for _, sub := range subs {
		byChannel[sub.ChannelID] = append(byChannel[sub.ChannelID], sub)
	}
	channelIDs := make([]string, 0, len(byChannel))
	for ch := range byChannel {
		channelIDs = append(channelIDs, ch)
	}
	sort.Strings(channelIDs)

	lines := []string{"**Subscriptions for this guild**"}
	for _, ch := range channelIDs {
		lines = append(lines, fmt.Sprintf("**<#%s>**", ch))
		entries := byChannel[ch]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].ServerID == entries[j].ServerID {
				return entries[i].Mention < entries[j].Mention
			}
			return entries[i].ServerID < entries[j].ServerID
		})

		for _, sub := range entries {
			human := h.humanServerLabel(mapping, sub.ServerID)
			if sub.Mention != "" {
				lines = append(lines, fmt.Sprintf("- `%s` %s", human, sub.Mention))
			} else {
				lines = append(lines, fmt.Sprintf("- `%s`", human))
			}
		}
	}

	content := strings.Join(lines, "\n")
	if len(content) > 1900 {
		slog.Warn("subscriptions list truncated for discord limit",
			"guildID", interaction.GuildID,
			"length", len(content),
		)
		content = content[:1900] + "\n\n(truncated)"
	}
	slog.Info("subscriptions list response built",
		"guildID", interaction.GuildID,
		"channels", len(channelIDs),
		"length", len(content),
	)
	return h.discordResponse(content)
}

func (h *Handler) humanServerLabel(mapping servermap.Mapping, technicalServerID string) string {
	parts := strings.Split(technicalServerID, "#")
	if len(parts) != 3 {
		return technicalServerID
	}
	provider := parts[0]
	region := parts[1]
	identifier := parts[2]

	for gameID, game := range mapping.Games {
		if game.Provider != provider {
			continue
		}
		for serverKey, server := range game.Servers {
			if server.Region == region && fmt.Sprint(server.Identifier) == identifier {
				return fmt.Sprintf("%s-%s", gameID, serverKey)
			}
		}
	}
	return technicalServerID
}

// Helper methods for standardized responses

func (h *Handler) getOption(options []discord.InteractionOption, name string) string {
	for _, opt := range options {
		if opt.Name == name {
			if val, ok := opt.Value.(string); ok {
				return val
			}
		}
	}
	return ""
}

func (h *Handler) discordResponse(content string) (events.LambdaFunctionURLResponse, error) {
	return h.jsonResponse(http.StatusOK, discord.InteractionResponse{
		Type: discord.InteractionResponseTypeChannelMessageWithSource,
		Data: &discord.InteractionResponseData{
			Content: content,
		},
	})
}

func (h *Handler) jsonResponse(statusCode int, body any) (events.LambdaFunctionURLResponse, error) {
	jsonBytes, _ := json.Marshal(body)
	return events.LambdaFunctionURLResponse{
		StatusCode: statusCode,
		Body:       string(jsonBytes),
		Headers:    map[string]string{"Content-Type": "application/json"},
	}, nil
}

func (h *Handler) loadServerMapping(ctx context.Context) (servermap.Mapping, error) {
	h.mappingMu.RLock()
	if !h.mappingCachedAt.IsZero() && time.Since(h.mappingCachedAt) < mappingCacheTTL {
		m := h.mappingCached
		h.mappingMu.RUnlock()
		return m, nil
	}
	h.mappingMu.RUnlock()

	mapping, err := h.loadServerMappingFromS3(ctx)
	if err != nil {
		return servermap.Mapping{}, err
	}

	h.mappingMu.Lock()
	h.mappingCached = mapping
	h.mappingCachedAt = time.Now()
	h.mappingMu.Unlock()
	return mapping, nil
}

func (h *Handler) loadServerMappingFromS3(ctx context.Context) (servermap.Mapping, error) {
	var mapping servermap.Mapping

	bucket := os.Getenv("CONFIG_BUCKET")
	if bucket == "" {
		bucket = defaultConfigBucket
	}
	key := os.Getenv("SERVER_MAPPING_PATH")
	if key == "" {
		key = defaultServerMappingKey
	}

	if err := h.configProvider.LoadJSONFromS3(ctx, bucket, key, &mapping); err != nil {
		return servermap.Mapping{}, err
	}
	return mapping, nil
}

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
		"- `/help` — show this message",
		"",
		"**Tips**",
		"- Game + server names are case-insensitive. Spaces/underscores are treated like hyphens (e.g. `Area 52` → `area-52`).",
		"- Run `/subscriptions` to see what’s configured; `/unsubscribe` uses the same entries (including which channel each row is in).",
	}, "\n")
	return h.discordResponse(msg)
}

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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}

func wrapBackticks(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("`%s`", it))
	}
	return out
}
