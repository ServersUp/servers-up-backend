package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/aggregate"
	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"golang.org/x/sync/errgroup"
)

var (
	roleMentionPattern = regexp.MustCompile(`<@&(\d+)>`)
	rawRolePattern     = regexp.MustCompile(`^\d+$`)
)

const (
	defaultSettleWindowMinutes = 5
	maxConcurrentSQSSends      = 32
)

type subscriptionLister interface {
	ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error)
}

type statusLister interface {
	ListServerStatusesByGame(ctx context.Context, gameID string) ([]models.GameServerStatus, error)
}

type messageSender interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// scopeStore is satisfied by *db.ScopeStateStore.
type scopeStore interface {
	List(ctx context.Context) ([]models.ScopeState, error)
	Get(ctx context.Context, scopeKey string) (*models.ScopeState, error)
	Put(ctx context.Context, st models.ScopeState) error
	Delete(ctx context.Context, scopeKey string) error
	ClaimNotify(ctx context.Context, st models.ScopeState, episode string) (bool, error)
}

// Handler aggregates the status of wildcard subscription scopes and enqueues
// aggregate notifications when an entire scope transitions to all-UP or
// all-DOWN (after a settle window), once per episode.
type Handler struct {
	scopes         scopeStore
	subs           subscriptionLister
	statuses       statusLister
	configProvider *config.Provider
	configBucket   string
	mappingKey     string
	queueURL       string
	sqs            messageSender
	settleWindow   time.Duration
	// mapping overrides the S3-backed mapping loader (used by tests).
	mapping *servermap.Mapping
}

// NewHandler loads AWS clients from the environment; on failure it logs and
// exits (see main).
func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	scopeTable := os.Getenv("DDB_SCOPE_STATE_TABLE_NAME")
	if scopeTable == "" {
		slog.Error("missing required env DDB_SCOPE_STATE_TABLE_NAME")
		os.Exit(1)
	}
	subsTable := os.Getenv("DDB_SUBSCRIPTIONS_TABLE_NAME")
	if subsTable == "" {
		slog.Error("missing required env DDB_SUBSCRIPTIONS_TABLE_NAME")
		os.Exit(1)
	}
	statusTable := os.Getenv("DDB_GAME_SERVER_STATUS_TABLE_NAME")
	if statusTable == "" {
		slog.Error("missing required env DDB_GAME_SERVER_STATUS_TABLE_NAME")
		os.Exit(1)
	}
	queueURL := os.Getenv("GUILD_NOTIFY_JOBS_QUEUE_URL")
	if queueURL == "" {
		slog.Error("missing required env GUILD_NOTIFY_JOBS_QUEUE_URL")
		os.Exit(1)
	}
	configBucket := os.Getenv("CONFIG_BUCKET")
	if configBucket == "" {
		configBucket = "serversup-config"
	}
	mappingKey := os.Getenv("SERVER_MAPPING_KEY")
	if mappingKey == "" {
		mappingKey = "server-mapping.json"
	}

	settleWindow := time.Duration(defaultSettleWindowMinutes) * time.Minute
	if v := os.Getenv("SETTLE_WINDOW_MINUTES"); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			settleWindow = time.Duration(m) * time.Minute
		}
	}

	ddbClient := dynamodb.NewFromConfig(cfg)
	return &Handler{
		scopes:         db.NewScopeStateStore(ddbClient, scopeTable),
		subs:           db.NewDatabase(ddbClient, subsTable),
		statuses:       db.NewDatabase(ddbClient, statusTable),
		configProvider: config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg)),
		configBucket:   configBucket,
		mappingKey:     mappingKey,
		queueURL:       queueURL,
		sqs:            sqs.NewFromConfig(cfg),
		settleWindow:   settleWindow,
	}
}

func main() {
	logsetup.ConfigureDefaultFromEnv()
	lambda.Start(NewHandler(context.Background()).HandleRequest)
}

// delivery is a deduplicated aggregate notification pending delivery.
type delivery struct {
	job models.GuildNotifyJob
}

// HandleRequest runs one aggregate pass. It is invoked on a schedule (EventBridge
// Scheduler, roughly every minute).
func (h *Handler) HandleRequest(ctx context.Context) error {
	scopes, err := h.scopes.List(ctx)
	if err != nil {
		return err
	}
	if len(scopes) == 0 {
		slog.Info("no active wildcard scopes; skipping aggregate run")
		return nil
	}

	mapping, err := h.loadMapping(ctx)
	if err != nil {
		return err
	}

	gameSet := make(map[string]struct{})
	for _, st := range scopes {
		if g := scope.GameID(st.ScopeKey); g != "" {
			gameSet[g] = struct{}{}
		}
	}
	statusByServer := make(map[string]string)
	for g := range gameSet {
		rows, err := h.statuses.ListServerStatusesByGame(ctx, g)
		if err != nil {
			return fmt.Errorf("list statuses for %s: %w", g, err)
		}
		for _, row := range rows {
			statusByServer[row.ServerID] = row.Status
		}
	}

	now := time.Now().Unix()
	pending := make(map[string]*delivery)
	for i := range scopes {
		if err := h.processScope(ctx, mapping, statusByServer, &scopes[i], now, pending); err != nil {
			return err
		}
	}

	return h.enqueuePending(ctx, pending)
}

func (h *Handler) loadMapping(ctx context.Context) (servermap.Mapping, error) {
	if h.mapping != nil {
		return *h.mapping, nil
	}
	var m servermap.Mapping
	if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.mappingKey, &m); err != nil {
		return servermap.Mapping{}, fmt.Errorf("load server mapping: %w", err)
	}
	return m, nil
}

// processScope advances one scope's aggregate state and, on a won claim,
// collects its deliveries into pending (deduplicated per delivery target).
func (h *Handler) processScope(ctx context.Context, mapping servermap.Mapping, statusByServer map[string]string, cur *models.ScopeState, now int64, pending map[string]*delivery) error {
	gameID := scope.GameID(cur.ScopeKey)
	region := scope.Region(cur.ScopeKey)
	total, up := aggregate.Counts(mapping, statusByServer, gameID, region)
	newState := aggregate.DeriveState(total, up)
	episode := aggregate.Episode(cur.State, cur.StateSince)

	if cur.StateSince == 0 {
		// Defensive baseline: normally created at subscribe time. Never notify
		// on first observation.
		cur.UpCount = up
		cur.DownCount = total - up
		cur.TotalCount = total
		cur.State = newState
		cur.StateSince = now
		cur.LastNotifiedEpisode = aggregate.Episode(newState, now)
		cur.UpdatedAt = now
		slog.Info("scope state baseline initialized", "scopeKey", cur.ScopeKey, "state", newState, "total", total, "up", up)
		return h.scopes.Put(ctx, *cur)
	}

	if newState != cur.State {
		cur.UpCount = up
		cur.DownCount = total - up
		cur.TotalCount = total
		cur.State = newState
		cur.StateSince = now
		cur.UpdatedAt = now
		slog.Info("scope state changed",
			"scopeKey", cur.ScopeKey, "oldState", episode, "newState", newState, "total", total, "up", up)
		return h.scopes.Put(ctx, *cur)
	}

	if !aggregate.IsTerminal(cur.State) {
		if cur.UpCount != up || cur.DownCount != total-up || cur.TotalCount != total {
			cur.UpCount = up
			cur.DownCount = total - up
			cur.TotalCount = total
			cur.UpdatedAt = now
			return h.scopes.Put(ctx, *cur)
		}
		return nil
	}

	if now-cur.StateSince < int64(h.settleWindow.Seconds()) {
		return nil
	}
	if cur.LastNotifiedEpisode == episode {
		return nil
	}

	claimed, err := h.scopes.ClaimNotify(ctx, *cur, episode)
	if err != nil {
		return err
	}
	if !claimed {
		slog.Info("scope episode already claimed by another run; skipping", "scopeKey", cur.ScopeKey, "episode", episode)
		return nil
	}

	return h.collectDeliveries(ctx, cur, pending)
}

// collectDeliveries loads the scope's subscriptions and adds deliveries for
// them. When the same delivery target has overlapping subscriptions in the
// same tick, the first (most specific) delivery wins for a delivery key.
func (h *Handler) collectDeliveries(ctx context.Context, cur *models.ScopeState, pending map[string]*delivery) error {
	subs, err := h.subs.ListSubscriptionsByServer(ctx, cur.ScopeKey)
	if err != nil {
		return fmt.Errorf("list subscriptions for %s: %w", cur.ScopeKey, err)
	}

	label := scopeLabelFor(cur.ScopeKey)
	statusStr := "DOWN"
	if cur.State == scope.StateAllUp {
		statusStr = "UP"
	}

	for _, sub := range subs {
		key := deliveryKey(&sub)
		if _, ok := pending[key]; ok {
			continue
		}
		pending[key] = &delivery{
			job: models.GuildNotifyJob{
				ServerID:   cur.ScopeKey,
				Status:     statusStr,
				Aggregate:  true,
				ScopeLabel: label,
				Scope:      scope.TypeRegion,
				GuildID:    sub.GuildID,
				ChannelID:  sub.ChannelID,
				RoleID:     resolveRoleID(sub.Mention),
				TargetType: sub.TargetType,
				WebhookURL: sub.WebhookURL,
			},
		}
	}
	return nil
}

// enqueuePending sends all pending aggregate jobs to the guild notify queue.
func (h *Handler) enqueuePending(ctx context.Context, pending map[string]*delivery) error {
	if len(pending) == 0 {
		return nil
	}
	g, sendCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentSQSSends)
	for _, d := range pending {
		d := d
		g.Go(func() error {
			body, err := json.Marshal(d.job)
			if err != nil {
				return fmt.Errorf("marshal aggregate job: %w", err)
			}
			_, err = h.sqs.SendMessage(sendCtx, &sqs.SendMessageInput{
				QueueUrl:    aws.String(h.queueURL),
				MessageBody: aws.String(string(body)),
			})
			if err != nil {
				return fmt.Errorf("sqs send aggregate job for %s: %w", d.job.ScopeLabel, err)
			}
			slog.Info("enqueued aggregate notify job",
				"scope", d.job.Scope,
				"scopeLabel", d.job.ScopeLabel,
				"status", d.job.Status,
				"guildID", d.job.GuildID,
				"channelID", d.job.ChannelID,
				"targetType", d.job.TargetType,
			)
			return nil
		})
	}
	return g.Wait()
}

// scopeLabelFor returns the message scope label for a wildcard key: "WoW EU".
func scopeLabelFor(key string) string {
	g := scope.GameDisplayName(scope.GameID(key))
	if r := scope.Region(key); r != "" {
		return fmt.Sprintf("%s %s", g, strings.ToUpper(r))
	}
	return g
}

// deliveryKey identifies a delivery target: "bot:<guildId>:<channelId>" or
// "webhook:<normalizedURL>".
func deliveryKey(sub *models.Subscription) string {
	if sub.TargetType == "webhook" {
		return "webhook:" + strings.ToLower(strings.TrimSpace(sub.WebhookURL))
	}
	return fmt.Sprintf("bot:%s:%s", sub.GuildID, sub.ChannelID)
}

// resolveRoleID extracts a Discord role ID from either a "<@&id>" mention or a
// raw numeric role id (webhook subscriptions store the raw id).
func resolveRoleID(mention string) string {
	if m := roleMentionPattern.FindStringSubmatch(mention); len(m) == 2 {
		return m[1]
	}
	if rawRolePattern.MatchString(mention) {
		return mention
	}
	return ""
}
