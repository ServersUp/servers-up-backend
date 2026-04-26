package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/google/uuid"
)


// ServerMapping defines the structure of the S3 mapping file.
type ServerMapping struct {
	Games map[string]struct {
		Provider string `json:"provider"`
		Servers  map[string]struct {
			Region     string `json:"region"`
			Identifier any    `json:"identifier"`
		} `json:"servers"`
	} `json:"games"`
}

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
			return h.errorResponse("failed to parse command data")
		}

		slog.Info("Handling Slash Command", "command", data.Name, "guild", interaction.GuildID)

		switch data.Name {
		case "subscribe":
			return h.handleSubscribe(ctx, interaction, data)
		case "unsubscribe":
			return h.handleUnsubscribe(ctx, interaction, data)
		default:
			return h.errorResponse("unknown command")
		}
	}

	return h.errorResponse("unsupported interaction type")
}

func (h *Handler) handleSubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	gameName := h.getOption(data.Options, "game")
	serverName := h.getOption(data.Options, "server")
	roleID := h.getOption(data.Options, "role")

	// 1. Load mapping from S3
	var mapping ServerMapping
	err := h.configProvider.LoadJSONFromS3(ctx, os.Getenv("CONFIG_BUCKET"), os.Getenv("SERVER_MAPPING_PATH"), &mapping)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration.")
	}

	// 2. Perform translation
	game, ok := mapping.Games[gameName]
	if !ok {
		return h.discordResponse(fmt.Sprintf("Unsupported game: %s", gameName))
	}

	server, ok := game.Servers[serverName]
	if !ok {
		return h.discordResponse(fmt.Sprintf("Unknown server '%s' for game %s", serverName, gameName))
	}

	// Construct the technical server ID (e.g., battlenet#us#11)
	technicalID := fmt.Sprintf("%s#%s#%v", game.Provider, server.Region, server.Identifier)

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

	return h.discordResponse(fmt.Sprintf("Successfully subscribed to updates for **%s** (%s)!", serverName, gameName))
}

func (h *Handler) handleUnsubscribe(ctx context.Context, interaction discord.Interaction, data discord.InteractionData) (events.LambdaFunctionURLResponse, error) {
	gameName := h.getOption(data.Options, "game")
	serverName := h.getOption(data.Options, "server")

	// 1. Load mapping from S3
	var mapping ServerMapping
	err := h.configProvider.LoadJSONFromS3(ctx, os.Getenv("CONFIG_BUCKET"), os.Getenv("SERVER_MAPPING_PATH"), &mapping)
	if err != nil {
		slog.Error("failed to load server mapping", "error", err)
		return h.discordResponse("System error: Unable to load server configuration.")
	}

	// 2. Perform translation
	game, ok := mapping.Games[gameName]
	if !ok {
		return h.discordResponse(fmt.Sprintf("Unsupported game: %s", gameName))
	}

	server, ok := game.Servers[serverName]
	if !ok {
		return h.discordResponse(fmt.Sprintf("Unknown server '%s' for game %s", serverName, gameName))
	}

	technicalID := fmt.Sprintf("%s#%s#%v", game.Provider, server.Region, server.Identifier)

	found, err := h.database.DeleteSubscriptionByChannel(ctx, technicalID, interaction.ChannelID)
	if err != nil {
		slog.Error("failed to delete subscription", "error", err, "serverID", technicalID, "channelID", interaction.ChannelID)
		return h.discordResponse("An error occurred while trying to unsubscribe.")
	}

	if !found {
		return h.discordResponse(fmt.Sprintf("No subscription found for **%s** in this channel.", serverName))
	}

	return h.discordResponse(fmt.Sprintf("Successfully unsubscribed from **%s** updates.", serverName))
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

func (h *Handler) errorResponse(msg string) (events.LambdaFunctionURLResponse, error) {
	return events.LambdaFunctionURLResponse{StatusCode: http.StatusBadRequest, Body: msg}, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
