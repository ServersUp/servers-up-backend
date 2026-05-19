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
	"strconv"
	"strings"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
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
	discord          DiscordClient
	configProvider   *config.Provider
	mappingCache     *servermap.CachedMapping
	configBucket     string
	serverMappingKey string
}

func NewHandler() *Handler {
	tokenPath := os.Getenv("DISCORD_BOT_TOKEN_PATH")
	if tokenPath == "" {
		slog.Error("missing required env DISCORD_BOT_TOKEN_PATH")
		os.Exit(1)
	}

	dlqURL := os.Getenv("GUILD_NOTIFY_JOBS_DLQ_URL")
	if dlqURL == "" {
		slog.Error("missing required env GUILD_NOTIFY_JOBS_DLQ_URL")
		os.Exit(1)
	}
	if name := sqsQueueNameFromURL(dlqURL); name != "" {
		slog.Info("guild notify jobs DLQ configured", "queueName", name)
	} else {
		slog.Info("guild notify jobs DLQ configured")
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
		discord: &discordHTTPClient{
			httpClient: &http.Client{Timeout: 10 * time.Second},
			baseURL:    "https://discord.com/api/v10",
			botToken:   token,
		},
		configProvider:   provider,
		mappingCache:     servermap.NewCachedMapping(servermap.CacheTTLFromEnv()),
		configBucket:     bucket,
		serverMappingKey: key,
	}
}

func sqsQueueNameFromURL(queueURL string) string {
	if i := strings.LastIndex(queueURL, "/"); i >= 0 && i < len(queueURL)-1 {
		return queueURL[i+1:]
	}
	return ""
}

func emitNotifySendError(statusCode int) {
	if statusCode < 400 || statusCode >= 600 {
		return
	}
	metrics.EmitCount(metrics.Namespace, "NotifySendError", map[string]string{
		"discordStatus": strconv.Itoa(statusCode),
	}, 1)
}

func (h *Handler) HandleRequest(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse

	for _, rec := range event.Records {
		err := h.processRecord(ctx, rec)
		if err == nil {
			continue
		}

		slog.Error("failed to process SQS record", "error", err, "messageId", rec.MessageId, "eventSource", rec.EventSource)
		resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{
			ItemIdentifier: rec.MessageId,
		})
	}

	return resp, nil
}

func (h *Handler) processRecord(ctx context.Context, rec events.SQSMessage) error {
	var job models.GuildNotifyJob
	if err := json.Unmarshal([]byte(rec.Body), &job); err != nil {
		slog.Warn("invalid guild notify job payload; ack-deleting",
			"error", err,
			"messageId", rec.MessageId,
			"bodySnippet", bodySnippet(rec.Body, 256),
		)
		return nil
	}

	if job.ServerID == "" || job.Status == "" || job.ChannelID == "" {
		slog.Warn("guild notify job missing required fields; ack-deleting",
			"messageId", rec.MessageId,
			"serverID", job.ServerID,
			"status", job.Status,
			"channelID", job.ChannelID,
			"guildID", job.GuildID,
		)
		return nil
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
		return h.handleDiscordSendError(job, rec.MessageId, err)
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

func (h *Handler) handleDiscordSendError(job models.GuildNotifyJob, messageID string, err error) error {
	if apiErr, ok := discord.AsAPIError(err); ok {
		emitNotifySendError(apiErr.StatusCode)
		if apiErr.Permanent() {
			slog.Warn("permanent discord send failure; ack-deleting",
				"error", err,
				"discordStatus", apiErr.StatusCode,
				"serverID", job.ServerID,
				"status", job.Status,
				"guildID", job.GuildID,
				"channelID", job.ChannelID,
				"messageId", messageID,
			)
			return nil
		}
		if apiErr.Retryable() {
			slog.Error("retryable discord send failure",
				"error", err,
				"discordStatus", apiErr.StatusCode,
				"serverID", job.ServerID,
				"guildID", job.GuildID,
				"channelID", job.ChannelID,
				"messageId", messageID,
			)
			return fmt.Errorf("send discord message: %w", err)
		}
	}

	slog.Error("discord send failed",
		"error", err,
		"serverID", job.ServerID,
		"guildID", job.GuildID,
		"channelID", job.ChannelID,
		"messageId", messageID,
	)
	return fmt.Errorf("send discord message: %w", err)
}

func bodySnippet(body string, maxLen int) string {
	if len(body) <= maxLen {
		return body
	}
	return body[:maxLen] + "…"
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
	mapping, err := h.loadServerMapping(ctx)
	if err != nil {
		slog.Warn("failed to load server mapping; falling back to technical server id", "error", err)
		return technicalServerID
	}
	return mapping.HumanLabel(technicalServerID)
}

func (h *Handler) loadServerMapping(ctx context.Context) (servermap.Mapping, error) {
	return h.mappingCache.Get(ctx, func(ctx context.Context) (servermap.Mapping, error) {
		if h.configProvider == nil {
			return servermap.Mapping{}, fmt.Errorf("missing config provider")
		}
		var m servermap.Mapping
		if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.serverMappingKey, &m); err != nil {
			return servermap.Mapping{}, err
		}
		return m, nil
	})
}

type discordHTTPClient struct {
	httpClient *http.Client
	baseURL    string
	botToken   string
}

type discordMessageRequest struct {
	Content         string                 `json:"content"`
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
	return &discord.APIError{StatusCode: res.StatusCode, Body: string(body)}
}

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := NewHandler()
	lambda.Start(handler.HandleRequest)
}
