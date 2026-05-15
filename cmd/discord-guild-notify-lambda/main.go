package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type DiscordClient interface {
	SendChannelMessage(ctx context.Context, channelID, content, roleID string) error
}

type Handler struct {
	discord         DiscordClient
	configProvider  *config.Provider
	serverMapping   *servermap.Mapping
	configBucket    string
	serverMappingKey string
}

func NewHandler() *Handler {
	tokenPath := os.Getenv("DISCORD_BOT_TOKEN_PATH")
	if tokenPath == "" {
		slog.Error("missing required env DISCORD_BOT_TOKEN_PATH")
		os.Exit(1)
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	provider := config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg))
	token, err := provider.GetSecret(context.Background(), tokenPath)
	if err != nil {
		slog.Error("failed to load discord bot token from ssm", "error", err, "path", tokenPath)
		os.Exit(1)
	}

	bucket := os.Getenv("CONFIG_BUCKET")
	if bucket == "" {
		slog.Error("missing required env CONFIG_BUCKET")
		os.Exit(1)
	}
	key := os.Getenv("SERVER_MAPPING_PATH")
	if key == "" {
		slog.Error("missing required env SERVER_MAPPING_PATH")
		os.Exit(1)
	}

	return &Handler{
		discord:        &discordHTTPClient{
			httpClient: &http.Client{Timeout: 10 * time.Second},
			baseURL:    "https://discord.com/api/v10",
			botToken:   token,
		},
		configProvider:  provider,
		configBucket:    bucket,
		serverMappingKey: key,
	}
}

func (h *Handler) HandleRequest(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse

	for _, rec := range event.Records {
		err := h.processRecord(ctx, rec)
		if err == nil {
			continue
		}

		slog.Error("failed to process SQS record", "error", err, "messageId", rec.MessageId)
		resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{
			ItemIdentifier: rec.MessageId,
		})
	}

	return resp, nil
}

func (h *Handler) processRecord(ctx context.Context, rec events.SQSMessage) error {
	var job models.GuildNotifyJob
	if err := json.Unmarshal([]byte(rec.Body), &job); err != nil {
		return fmt.Errorf("unmarshal guild notify job: %w", err)
	}

	if job.ServerID == "" || job.Status == "" || job.ChannelID == "" {
		return fmt.Errorf("invalid guild notify job: missing required fields (serverId/status/channelId)")
	}

	serverLabel := h.humanServerName(ctx, job.ServerID)
	content := formatDiscordContent(job, serverLabel)

	slog.Info("sending discord notification",
		"serverID", job.ServerID,
		"status", job.Status,
		"guildID", job.GuildID,
		"channelID", job.ChannelID,
		"hasRole", job.RoleID != "",
		"messageId", rec.MessageId,
	)

	if err := h.discord.SendChannelMessage(ctx, job.ChannelID, content, job.RoleID); err != nil {
		return fmt.Errorf("send discord message: %w", err)
	}

	slog.Info("sent discord notification",
		"serverID", job.ServerID,
		"status", job.Status,
		"guildID", job.GuildID,
		"channelID", job.ChannelID,
		"messageId", rec.MessageId,
	)

	return nil
}

func formatDiscordContent(job models.GuildNotifyJob, serverLabel string) string {
	mention := ""
	if job.RoleID != "" {
		mention = fmt.Sprintf("<@&%s> ", job.RoleID)
	}
	if serverLabel == "" {
		serverLabel = job.ServerID
	}
	return fmt.Sprintf("%sServer **%s** is now **%s**.", mention, serverLabel, job.Status)
}

func (h *Handler) humanServerName(ctx context.Context, technicalServerID string) string {
	// Expected: provider#region#identifier
	parts := strings.Split(technicalServerID, "#")
	if len(parts) != 3 {
		return technicalServerID
	}
	provider := parts[0]
	region := parts[1]
	identifier := parts[2]

	mapping, err := h.getServerMapping(ctx)
	if err != nil {
		slog.Warn("failed to load server mapping; falling back to technical server id", "error", err)
		return technicalServerID
	}

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

func (h *Handler) getServerMapping(ctx context.Context) (*servermap.Mapping, error) {
	if h.serverMapping != nil {
		return h.serverMapping, nil
	}
	if h.configProvider == nil {
		return nil, fmt.Errorf("missing config provider")
	}
	var m servermap.Mapping
	if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.serverMappingKey, &m); err != nil {
		return nil, err
	}
	h.serverMapping = &m
	return h.serverMapping, nil
}

type discordHTTPClient struct {
	httpClient *http.Client
	baseURL    string
	botToken   string
}

type discordMessageRequest struct {
	Content         string               `json:"content"`
	AllowedMentions discordAllowedMentions `json:"allowed_mentions,omitempty"`
}

type discordAllowedMentions struct {
	Parse []string `json:"parse,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

func (c *discordHTTPClient) SendChannelMessage(ctx context.Context, channelID, content, roleID string) error {
	reqBody := discordMessageRequest{
		Content: content,
		AllowedMentions: discordAllowedMentions{
			Parse: []string{}, // don't allow accidental @everyone/@here
		},
	}
	if roleID != "" {
		reqBody.AllowedMentions.Roles = []string{roleID}
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal discord message request: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages", c.baseURL, channelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}

	req.Header.Set("Authorization", "Bot "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(res.Body, 8*1024))
	return fmt.Errorf("discord non-2xx response: %d body=%q", res.StatusCode, string(body))
}

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := NewHandler()
	lambda.Start(handler.HandleRequest)
}
