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
	DeleteGuildChannelSubscriptionsForServer(ctx context.Context, guildID, channelID, serverID string) (int, error)
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

	mappingMu       sync.RWMutex
	mappingCached   servermap.Mapping
	mappingCachedAt time.Time
}

const (
	defaultConfigBucket     = "serversup-config"
	defaultServerMappingKey = "server-mapping.json"
	mappingCacheTTL         = 60 * time.Second
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

	return &Handler{
		database:         db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")),
		configProvider:   provider,
		discordPublicKey: publicKey,
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
		case "games":
			return h.handleListGames(ctx)
		case "servers":
			return h.handleListServers(ctx, data)
		default:
			return h.discordResponse("Unknown command. Use `/help` to see what I can do.")
		}

	case discord.InteractionTypeApplicationCommandAutocomplete:
		var data discord.InteractionData
		if err := json.Unmarshal(interaction.Data, &data); err != nil {
			slog.Warn("autocomplete: failed to parse interaction data", "error", err)
			return h.autocompleteResponse(nil)
		}
		return h.handleAutocomplete(ctx, data)
	}

	return h.discordResponse("Unsupported interaction type.")
}

func (h *Handler) handleAutocomplete(ctx context.Context, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	switch data.Name {
	case "subscribe", "unsubscribe":
	default:
		return h.autocompleteResponse(nil)
	}

	focused := findFocusedOption(data.Options)
	if focused == nil {
		return h.autocompleteResponse(nil)
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("autocomplete: failed to load server mapping", "error", err)
		return h.autocompleteResponse(nil)
	}

	const maxChoices = 25
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
		return h.discordResponse(h.formatLookupError("subscribe", mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	mention := ""
	if roleID != "" {
		mention = fmt.Sprintf("<@&%s>", roleID)
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

	channelMention := fmt.Sprintf("<#%s>", interaction.ChannelID)
	if mention != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed %s to **%s** / **%s** updates in %s.", mention, gameID, serverKey, channelMention))
	}
	return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** / **%s** updates.", gameID, serverKey))
}

func (h *Handler) handleUnsubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	rawGame := h.getOption(data.Options, "game")
	rawServer := h.getOption(data.Options, "server")
	gameName := servermap.NormalizeKey(rawGame)
	serverName := servermap.NormalizeKey(rawServer)

	slog.Info("unsubscribe request received",
		"guildID", interaction.GuildID,
		"channelID", interaction.ChannelID,
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
		slog.Warn("unsubscribe request lookup failed",
			"error", lookupErr,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"rawGame", rawGame,
			"rawServer", rawServer,
			"gameName", gameName,
			"serverName", serverName,
		)
		return h.discordResponse(h.formatLookupError("unsubscribe", mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	deleted, err := h.database.DeleteGuildChannelSubscriptionsForServer(ctx, interaction.GuildID, interaction.ChannelID, technicalID)
	if err != nil {
		slog.Error("failed to delete subscription",
			"error", err,
			"guildID", interaction.GuildID,
			"channelID", interaction.ChannelID,
			"gameID", gameID,
			"serverKey", serverKey,
			"technicalServerID", technicalID,
		)
		return h.discordResponse("An error occurred while trying to unsubscribe.")
	}

	if deleted == 0 {
		return h.discordResponse(fmt.Sprintf("No subscription found for **%s** / **%s** in this channel.", gameID, serverKey))
	}

	return h.discordResponse(fmt.Sprintf("Unsubscribed this channel from **%s** / **%s** updates (removed %d subscription(s) in this channel).", gameID, serverKey, deleted))
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
		return h.discordResponse("No subscriptions found for this guild.")
	}

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
		content = content[:1900] + "\n\n(truncated)"
	}
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
	msg := strings.Join([]string{
		"**ServersUp Discord Bot — Help**",
		"",
		"**Commands**",
		"- `/subscribe game:<game> server:<server> [role:<role>]` — subscribe this channel to server status updates",
		"- `/unsubscribe game:<game> server:<server>` — unsubscribe this channel (removes *all* subscriptions for that server in the current channel)",
		"- `/subscriptions` — list all subscriptions in this guild, grouped by channel",
		"- `/games` — list supported games",
		"- `/servers game:<game>` — list servers for a game",
		"",
		"**Tips**",
		"- For `/subscribe` and `/unsubscribe`, start typing in **game** or **server** to search matching options (up to 25 suggestions). Pick **role** from Discord’s role picker when needed.",
		"- Game + server names are case-insensitive. Spaces/underscores are treated like hyphens (e.g. `Area 52` → `area-52`).",
		"- Before unsubscribing, consider running `/subscriptions` to review what’s set up in each channel.",
		"- If a server list is very large, I’ll truncate it—use a more specific server name based on the list.",
	}, "\n")
	return h.discordResponse(msg)
}

func (h *Handler) handleListGames(ctx context.Context) (events.LambdaFunctionURLResponse, error) {
	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	games := mapping.ListGames()
	if len(games) == 0 {
		return h.discordResponse("No games are currently configured.")
	}

	sort.Strings(games)
	lines := make([]string, 0, len(games))
	for _, g := range games {
		lines = append(lines, fmt.Sprintf("- `%s`", g))
	}

	content := "**Supported games**\n" + strings.Join(lines, "\n")
	return h.discordResponse(content)
}

func (h *Handler) handleListServers(ctx context.Context, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	gameName := servermap.NormalizeKey(h.getOption(data.Options, "game"))
	if gameName == "" {
		return h.discordResponse("Missing `game`. Try `/servers game:wow` or run `/games` to see supported games.")
	}

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	servers, err := mapping.ListServers(gameName)
	if err != nil {
		if errors.Is(err, servermap.ErrUnknownGame) {
			return h.discordResponse(fmt.Sprintf("Unknown game `%s`. Use `/games` to see supported games.", gameName))
		}
		return h.discordResponse("Unable to list servers right now.")
	}
	if len(servers) == 0 {
		return h.discordResponse(fmt.Sprintf("No servers configured for `%s`.", gameName))
	}

	// Discord hard-limits message length (2000 chars). We try progressively more compact renderings,
	// and if it still won't fit we log the full list and return instructions.
	const maxChars = 1900

	// 1) Most compact: comma-separated (no bullet markup).
	joinedComma := strings.Join(servers, ", ")
	content := fmt.Sprintf("**Servers for `%s`** (%d total)\n%s", gameName, len(servers), joinedComma)
	if len(content) <= maxChars {
		return h.discordResponse(content)
	}

	// 2) Next: newline-separated in a code block (still readable, often smaller than backticked bullets).
	joinedNewline := strings.Join(servers, "\n")
	content = fmt.Sprintf("**Servers for `%s`** (%d total)\n```%s```", gameName, len(servers), joinedNewline)
	if len(content) <= maxChars {
		return h.discordResponse(content)
	}

	// 3) Still too large for Discord message limits.
	return h.discordResponse(fmt.Sprintf(
		"Sorry — there are too many servers to display for `%s` (%d total).",
		gameName,
		len(servers),
	))
}

func (h *Handler) formatLookupError(action string, mapping servermap.Mapping, err error, rawGame, rawServer string) string {
	switch {
	case errors.Is(err, servermap.ErrMissingGame):
		return fmt.Sprintf("Missing `game`. Try `/%s game:wow server:illidan` or run `/games`.", action)
	case errors.Is(err, servermap.ErrMissingServer):
		if rawGame == "" {
			return fmt.Sprintf("Missing `server`. Try `/%s game:wow server:illidan`.", action)
		}
		return fmt.Sprintf("Missing `server`. Run `/servers game:%s` to see valid server names.", servermap.NormalizeKey(rawGame))
	case errors.Is(err, servermap.ErrUnknownGame):
		games := mapping.ListGames()
		if len(games) == 0 {
			return fmt.Sprintf("Unknown game `%s`.", servermap.NormalizeKey(rawGame))
		}
		if len(games) > 10 {
			games = games[:10]
		}
		return fmt.Sprintf("Unknown game `%s`. Try `/games` (examples: %s).", servermap.NormalizeKey(rawGame), strings.Join(wrapBackticks(games), ", "))
	case errors.Is(err, servermap.ErrUnknownServer):
		return fmt.Sprintf("Unknown server `%s` for game `%s`. Run `/servers game:%s` to see valid server names.", servermap.NormalizeKey(rawServer), servermap.NormalizeKey(rawGame), servermap.NormalizeKey(rawGame))
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
