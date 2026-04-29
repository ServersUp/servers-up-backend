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
	DeleteSubscriptionByChannel(ctx context.Context, serverID, channelID string) (bool, error)
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
}

const (
	defaultConfigBucket     = "serversup-config"
	defaultServerMappingKey = "server-mapping.json"
)

func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	return &Handler{
		database:         db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")),
		configProvider:   config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg)),
		discordPublicKey: os.Getenv("DISCORD_API_PUBLIC_KEY"),
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
		case "help":
			return h.handleHelp()
		case "games":
			return h.handleListGames(ctx)
		case "servers":
			return h.handleListServers(ctx, data)
		default:
			return h.discordResponse("Unknown command. Use `/help` to see what I can do.")
		}
	}

	return h.discordResponse("Unsupported interaction type.")
}

func (h *Handler) handleSubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	gameName := servermap.NormalizeKey(h.getOption(data.Options, "game"))
	serverName := servermap.NormalizeKey(h.getOption(data.Options, "server"))
	roleID := h.getOption(data.Options, "role")

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, game, serverKey, server, lookupErr := mapping.Lookup(gameName, serverName)
	if lookupErr != nil {
		return h.discordResponse(h.formatLookupError("subscribe", mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	mention := ""
	if roleID != "" {
		mention = fmt.Sprintf("<@&%s>", roleID)
	}

	sub := models.Subscription{
		ServerID:       technicalID,
		SubscriptionID: uuid.New().String(),
		GuildID:        interaction.GuildID,
		ChannelID:      interaction.ChannelID,
		Mention:        mention,
	}

	if err := h.database.AddSubscription(ctx, sub); err != nil {
		slog.Error("failed to add subscription", "error", err, "serverID", technicalID)
		return h.discordResponse("Failed to create subscription. Please try again later.")
	}

	channelMention := fmt.Sprintf("<#%s>", interaction.ChannelID)
	if mention != "" {
		return h.discordResponse(fmt.Sprintf("Subscribed %s to **%s** / **%s** updates in %s.", mention, gameID, serverKey, channelMention))
	}
	return h.discordResponse(fmt.Sprintf("Subscribed this channel to **%s** / **%s** updates.", gameID, serverKey))
}

func (h *Handler) handleUnsubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	gameName := servermap.NormalizeKey(h.getOption(data.Options, "game"))
	serverName := servermap.NormalizeKey(h.getOption(data.Options, "server"))

	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration right now. Please try again in a bit.")
	}

	gameID, game, serverKey, server, lookupErr := mapping.Lookup(gameName, serverName)
	if lookupErr != nil {
		return h.discordResponse(h.formatLookupError("unsubscribe", mapping, lookupErr, gameName, serverName))
	}

	technicalID := serverid.Generate(game.Provider, server.Region, server.Identifier)

	found, err := h.database.DeleteSubscriptionByChannel(ctx, technicalID, interaction.ChannelID)
	if err != nil {
		slog.Error("failed to delete subscription", "error", err, "serverID", technicalID, "channelID", interaction.ChannelID)
		return h.discordResponse("An error occurred while trying to unsubscribe.")
	}

	if !found {
		return h.discordResponse(fmt.Sprintf("No subscription found for **%s** / **%s** in this channel.", gameID, serverKey))
	}

	return h.discordResponse(fmt.Sprintf("Unsubscribed this channel from **%s** / **%s** updates.", gameID, serverKey))
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
		"- `/unsubscribe game:<game> server:<server>` — unsubscribe this channel",
		"- `/games` — list supported games",
		"- `/servers game:<game>` — list servers for a game",
		"",
		"**Tips**",
		"- Game + server names are case-insensitive. Spaces/underscores are treated like hyphens (e.g. `Area 52` → `area-52`).",
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

	// Build a Discord-safe message (<= 2000 chars).
	const maxChars = 1900
	const maxItems = 80

	lines := make([]string, 0, minInt(len(servers), maxItems))
	for i, s := range servers {
		if i >= maxItems {
			break
		}
		lines = append(lines, fmt.Sprintf("- `%s`", s))
	}

	content := fmt.Sprintf("**Servers for `%s`** (%d total)\n%s", gameName, len(servers), strings.Join(lines, "\n"))
	if len(content) > maxChars {
		// Very defensive: shrink list until we fit.
		for len(lines) > 0 && len(content) > maxChars {
			lines = lines[:len(lines)-1]
			content = fmt.Sprintf("**Servers for `%s`** (%d total)\n%s", gameName, len(servers), strings.Join(lines, "\n"))
		}
	}
	if len(servers) > len(lines) {
		content += fmt.Sprintf("\n\nShowing %d of %d. (List truncated)", len(lines), len(servers))
	}
	return h.discordResponse(content)
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
