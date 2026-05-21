package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"golang.org/x/sync/errgroup"
)

var roleMentionPattern = regexp.MustCompile(`<@&(\d+)>`)

// maxConcurrentSQSSends bounds in-flight SendMessage calls per stream record to avoid
// spiking connections when a server has many Discord subscriptions.
const maxConcurrentSQSSends = 32

type subscriptionLister interface {
	ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error)
}

type messageSender interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// Handler wires DynamoDB stream events to Discord guild notify SQS jobs.
// NewHandler loads AWS clients; on failure it logs and exits (see main).
type Handler struct {
	list     subscriptionLister
	sqs      messageSender
	queueURL string
}

func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	subsTable := os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")
	if subsTable == "" {
		slog.Error("missing required env DDB_SUBSCRIPTIONS_TABLE_NAME")
		os.Exit(1)
	}

	queueURL := os.Getenv("GUILD_NOTIFY_JOBS_QUEUE_URL")
	if queueURL == "" {
		slog.Error("missing required env GUILD_NOTIFY_JOBS_QUEUE_URL")
		os.Exit(1)
	}

	return &Handler{
		list:     db.NewDatabase(dynamodb.NewFromConfig(cfg), subsTable),
		sqs:      sqs.NewFromConfig(cfg),
		queueURL: queueURL,
	}
}

func (h *Handler) HandleRequest(ctx context.Context, event events.DynamoDBEvent) error {
	for i := range event.Records {
		if err := h.processRecord(ctx, &event.Records[i]); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) processRecord(ctx context.Context, rec *events.DynamoDBEventRecord) error {
	if rec.EventName != string(events.DynamoDBOperationTypeModify) {
		slog.Info("skipping stream record (not MODIFY)", "eventName", rec.EventName, "eventID", rec.EventID)
		return nil
	}

	newImg := rec.Change.NewImage
	oldImg := rec.Change.OldImage

	serverID := stringAttr(newImg, "serverId")
	newStatus := stringAttr(newImg, "status")
	oldStatus := stringAttr(oldImg, "status")

	if serverID == "" || newStatus == "" {
		slog.Warn("MODIFY record missing serverId or new status; skipping", "eventID", rec.EventID)
		return nil
	}

	if oldStatus == "" {
		slog.Info("skipping MODIFY without old status image (cannot confirm transition)", "serverID", serverID, "eventID", rec.EventID)
		return nil
	}

	if oldStatus == newStatus {
		slog.Debug("old and new status match; skipping", "serverID", serverID, "status", newStatus, "eventID", rec.EventID)
		return nil
	}

	slog.Info("server status transition",
		"serverID", serverID,
		"oldStatus", oldStatus,
		"newStatus", newStatus,
		"eventID", rec.EventID,
	)

	subs, err := h.list.ListSubscriptionsByServer(ctx, serverID)
	if err != nil {
		slog.Error("failed to list discord subscriptions", "error", err, "serverID", serverID, "eventID", rec.EventID)
		return fmt.Errorf("list subscriptions for %s: %w", serverID, err)
	}

	if len(subs) == 0 {
		slog.Info("no subscriptions for server; nothing to enqueue", "serverID", serverID, "newStatus", newStatus)
		return nil
	}

	g, sendCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentSQSSends)

	for _, sub := range subs {
		sub := sub
		g.Go(func() error {
			job := models.GuildNotifyJob{
				ServerID:    serverID,
				Status:      newStatus,
				GuildID:     sub.GuildID,
				ChannelID:   sub.ChannelID,
				RoleID:      roleIDFromMention(sub.Mention),
				ServerLabel: sub.ServerLabel,
			}

			body, err := json.Marshal(job)
			if err != nil {
				slog.Error("failed to marshal guild notify job", "error", err, "serverID", serverID, "guildID", sub.GuildID)
				return fmt.Errorf("marshal guild notify job: %w", err)
			}

			_, err = h.sqs.SendMessage(sendCtx, &sqs.SendMessageInput{
				QueueUrl:    aws.String(h.queueURL),
				MessageBody: aws.String(string(body)),
			})
			if err != nil {
				slog.Error("failed to send SQS message", "error", err, "serverID", serverID, "guildID", sub.GuildID, "channelID", sub.ChannelID)
				return fmt.Errorf("sqs send for server %s guild %s: %w", serverID, sub.GuildID, err)
			}

			slog.Info("enqueued guild notify job",
				"serverId", serverID,
				"status", newStatus,
				"guildId", sub.GuildID,
				"channelId", sub.ChannelID,
				"hasRole", job.RoleID != "",
				"eventID", rec.EventID,
			)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	slog.Info("finished processing status transition record",
		"serverID", serverID,
		"newStatus", newStatus,
		"jobsEnqueued", len(subs),
		"eventID", rec.EventID,
	)

	return nil
}

func stringAttr(img map[string]events.DynamoDBAttributeValue, key string) string {
	if img == nil {
		return ""
	}
	av, ok := img[key]
	if !ok || av.DataType() != events.DataTypeString {
		return ""
	}
	return av.String()
}

func roleIDFromMention(mention string) string {
	m := roleMentionPattern.FindStringSubmatch(mention)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
