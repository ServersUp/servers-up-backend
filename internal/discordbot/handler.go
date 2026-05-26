package discordbot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// logInteractionMarshalErr logs failures to serialize a Discord interaction response;
// the client still receives resp (typically HTTP 500) and AWS Lambda sees a successful invoke.
func logInteractionMarshalErr(ctx context.Context, resp events.LambdaFunctionURLResponse, err error) (events.LambdaFunctionURLResponse, error) {
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal interaction response", "error", err)
	}
	return resp, nil
}

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
	statusStore      StatusStore
	configProvider   ConfigProvider
	discordPublicKey string
	httpClient       *http.Client
	discordBotToken  string

	mappingCache  *servermap.CachedMapping
	statusLimiter *statusRateLimiter
	statusCache   *statusResultCache

	channelNamesMu    sync.RWMutex
	channelNamesGuild string
	channelNamesByID  map[string]string
	channelNamesAt    time.Time
}

const (
	defaultConfigBucket     = "serversup-config"
	defaultServerMappingKey = "server-mapping.json"
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

	ddbClient := dynamodb.NewFromConfig(cfg)
	h := &Handler{
		database:         db.NewDatabase(ddbClient, os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")),
		configProvider:   provider,
		discordPublicKey: publicKey,
		httpClient:       httpClient,
		discordBotToken:  botToken,
		mappingCache:     servermap.NewCachedMapping(servermap.CacheTTLFromEnv()),
		statusLimiter:    newStatusRateLimiter(),
		statusCache:      newStatusResultCache(),
	}
	if statusTable := os.Getenv("DDB_GAME_SERVER_STATUS_TABLE_NAME"); statusTable != "" {
		h.statusStore = db.NewDatabase(ddbClient, statusTable)
	} else {
		slog.Warn("DDB_GAME_SERVER_STATUS_TABLE_NAME not set; /status will be unavailable")
	}
	return h
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

	slog.LogAttrs(ctx, slog.LevelDebug, "discord request received", LambdaFunctionURLDebugAttrs(request, len(bodyBytes))...)

	if err := discord.ValidateSignatureTimestamp(timestamp, time.Now(), discord.DefaultSignatureMaxSkew); err != nil {
		slog.Warn("Invalid or replayed discord request timestamp", "error", err)
		return events.LambdaFunctionURLResponse{StatusCode: http.StatusUnauthorized, Body: "Invalid signature"}, nil
	}

	if err := discord.VerifySignature(h.discordPublicKey, signature, timestamp, string(bodyBytes)); err != nil {
		slog.Warn("Invalid request signature", "error", err)
		return events.LambdaFunctionURLResponse{StatusCode: http.StatusUnauthorized, Body: "Invalid signature"}, nil
	}

	// 2. Parse Interaction
	var interaction discord.Interaction
	if err := json.Unmarshal(bodyBytes, &interaction); err != nil {
		slog.Error("failed to unmarshal interaction", "error", err)
		return events.LambdaFunctionURLResponse{StatusCode: http.StatusBadRequest, Body: "Invalid JSON"}, nil
	}

	slog.Debug("discord interaction", "type", interaction.Type, "id", interaction.ID, "guildId", interaction.GuildID)

	// 3. Handle Interaction Types
	switch interaction.Type {
	case discord.InteractionTypePing:
		slog.Info("Handling Discord Ping (Type 1)")
		resp, err := h.jsonResponse(http.StatusOK, discord.InteractionResponse{Type: discord.InteractionResponseTypePong})
		return logInteractionMarshalErr(ctx, resp, err)

	case discord.InteractionTypeApplicationCommand:
		var data discord.InteractionData
		if err := json.Unmarshal(interaction.Data, &data); err != nil {
			resp, derr := h.discordResponse("Sorry — I couldn’t parse that command payload. Please try again.")
			return logInteractionMarshalErr(ctx, resp, derr)
		}

		slog.Info("Handling Slash Command", "command", data.Name, "interactionId", interaction.ID, "guild", interaction.GuildID)

		switch data.Name {
		case "subscribe":
			resp, err := h.handleSubscribe(ctx, interaction, data)
			return logInteractionMarshalErr(ctx, resp, err)
		case "unsubscribe":
			resp, err := h.handleUnsubscribe(ctx, interaction, data)
			return logInteractionMarshalErr(ctx, resp, err)
		case "subscriptions":
			resp, err := h.handleListSubscriptions(ctx, interaction)
			return logInteractionMarshalErr(ctx, resp, err)
		case "games":
			resp, err := h.handleGames(ctx)
			return logInteractionMarshalErr(ctx, resp, err)
		case "regions":
			resp, err := h.handleRegions(ctx, data)
			return logInteractionMarshalErr(ctx, resp, err)
		case "servers":
			resp, err := h.handleServers(ctx, data)
			return logInteractionMarshalErr(ctx, resp, err)
		case "status":
			resp, err := h.handleStatus(ctx, interaction, data)
			return logInteractionMarshalErr(ctx, resp, err)
		case "help":
			resp, err := h.handleHelp()
			return logInteractionMarshalErr(ctx, resp, err)
		default:
			resp, err := h.discordResponse("Unknown command. Use `/help` to see what I can do.")
			return logInteractionMarshalErr(ctx, resp, err)
		}

	case discord.InteractionTypeApplicationCommandAutocomplete:
		var data discord.InteractionData
		if err := json.Unmarshal(interaction.Data, &data); err != nil {
			slog.Warn("autocomplete: failed to parse interaction data", "error", err)
			resp, aerr := h.autocompleteResponse(nil)
			return logInteractionMarshalErr(ctx, resp, aerr)
		}
		resp, err := h.handleAutocomplete(ctx, interaction, data)
		return logInteractionMarshalErr(ctx, resp, err)
	}

	resp, err := h.discordResponse("Unsupported interaction type.")
	return logInteractionMarshalErr(ctx, resp, err)
}
