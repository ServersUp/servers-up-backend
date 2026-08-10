package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/aggregate"
	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

var webhookURLPattern = regexp.MustCompile(`^https://discord\.com/api/webhooks/[0-9]+/[A-Za-z0-9_-]+$`)

const allowedOrigin = "https://serversup.armasn.dev"

type subscribeRequest struct {
	WebhookURL string `json:"webhookUrl"`
	RoleID     string `json:"roleId,omitempty"`
	Game       string `json:"game"`
	Region     string `json:"region"`
	Server     string `json:"server"`
	Honeypot   string `json:"honeypot,omitempty"`
}

type SubscriptionStore interface {
	ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error)
	AddSubscription(ctx context.Context, subscription models.Subscription) error
}

// scopeStateStore persists aggregate scope state.
type scopeStateStore interface {
	Get(ctx context.Context, scopeKey string) (*models.ScopeState, error)
	Put(ctx context.Context, st models.ScopeState) error
}

// gameStatusLister reads current status rows for a game.
type gameStatusLister interface {
	ListServerStatusesByGame(ctx context.Context, gameID string) ([]models.GameServerStatus, error)
}

// mappingLoader loads configuration from S3; satisfied by *config.Provider.
type mappingLoader interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

type apiHandler struct {
	db               SubscriptionStore
	mappingCache     *servermap.CachedMapping
	configProvider   mappingLoader
	configBucket     string
	serverMappingKey string
	httpClient       *http.Client
	resolveServer    func(ctx context.Context, game, region, server string) (string, string, error)
	scopes           scopeStateStore
	gameStatuses     gameStatusLister
}

func NewHandler() *apiHandler {
	tableName := os.Getenv("TABLE_NAME")
	if tableName == "" {
		slog.Error("missing required env TABLE_NAME")
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

	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	provider := config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg))
	mappingCache := servermap.NewCachedMapping(servermap.CacheTTLFromEnv())

	ddbClient := dynamodb.NewFromConfig(cfg)
	h := &apiHandler{
		db:               db.NewDatabase(ddbClient, tableName),
		mappingCache:     mappingCache,
		configProvider:   provider,
		configBucket:     bucket,
		serverMappingKey: key,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
	}
	h.resolveServer = h.resolveServerViaMapping
	if statusTable := os.Getenv("DDB_GAME_SERVER_STATUS_TABLE_NAME"); statusTable != "" {
		h.gameStatuses = db.NewDatabase(ddbClient, statusTable)
	} else {
		slog.Warn("DDB_GAME_SERVER_STATUS_TABLE_NAME not set; scope baselines will use an empty status snapshot")
	}
	if scopeTable := os.Getenv("DDB_SCOPE_STATE_TABLE_NAME"); scopeTable != "" {
		h.scopes = db.NewScopeStateStore(ddbClient, scopeTable)
	} else {
		slog.Warn("DDB_SCOPE_STATE_TABLE_NAME not set; wildcard (ALL) subscriptions will be unavailable")
	}
	return h
}

func (h *apiHandler) HandleRequest(ctx context.Context, event events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	headers := map[string]string{
		"Content-Type":                 "application/json",
		"Access-Control-Allow-Origin":  allowedOrigin,
		"Access-Control-Allow-Methods": "POST, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type",
	}

	if event.RequestContext.HTTP.Method == "OPTIONS" {
		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Headers:    headers,
			Body:       "",
		}, nil
	}
	if event.RequestContext.HTTP.Method != "POST" {
		return errResponse(405, "method not allowed", headers), nil
	}

	origin := ""
	if event.Headers != nil {
		origin = event.Headers["origin"]
		if origin == "" {
			origin = event.Headers["Origin"]
		}
	}
	if origin != "" && origin != allowedOrigin {
		return errResponse(403, "origin not allowed", headers), nil
	}

	body := event.Body
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return errResponse(400, "bad request body", headers), nil
		}
		body = string(decoded)
	}

	var req subscribeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return errResponse(400, "invalid json", headers), nil
	}

	if req.Honeypot != "" {
		return okResponse(headers), nil
	}

	webhookURL := strings.TrimSpace(req.WebhookURL)
	if !webhookURLPattern.MatchString(webhookURL) {
		return errResponse(400, "invalid webhook url", headers), nil
	}

	pk, label, scopeType, gameID, regionKey, err := h.resolveTarget(ctx, req.Game, req.Region, req.Server)
	if err != nil {
		return errResponse(400, err.Error(), headers), nil
	}

	if err := h.proveWebhook(ctx, webhookURL, req.RoleID); err != nil {
		slog.Warn("webhook ownership proof failed", "error", err)
		return errResponse(400, "webhook ownership proof failed", headers), nil
	}

	existing, err := h.db.ListSubscriptionsByServer(ctx, pk)
	if err != nil {
		slog.Error("failed to list subscriptions", "error", err)
		return errResponse(500, "failed to check existing subscriptions", headers), nil
	}
	for _, sub := range existing {
		if sub.TargetType == "webhook" && normalizeWebhookURL(sub.WebhookURL) == normalizeWebhookURL(webhookURL) {
			return errResponse(409, "already subscribed", headers), nil
		}
	}

	subscription := models.Subscription{
		ServerID:       pk,
		SubscriptionID: fmt.Sprintf("%d", time.Now().UnixNano()),
		Mention:        req.RoleID,
		ServerLabel:    label,
		TargetType:     "webhook",
		WebhookURL:     webhookURL,
		Scope:          scopeType,
		GameID:         gameID,
		Region:         regionKey,
	}

	if err := h.db.AddSubscription(ctx, subscription); err != nil {
		slog.Error("failed to add subscription", "error", err)
		return errResponse(500, "failed to save subscription", headers), nil
	}

	if scopeType != "" {
		if err := h.ensureScopeBaseline(ctx, gameID, regionKey, pk); err != nil {
			// The aggregator defensively baselines on its next run; never fail
			// the subscription for a baseline hiccup.
			slog.Warn("failed to ensure scope baseline", "error", err, "scopeKey", pk)
		}
	}

	metrics.EmitCount(metrics.Namespace, "SubscriptionWrite", map[string]string{"target": "webhook", "scope": scopeType}, 1)

	return okResponse(headers), nil
}

func okResponse(headers map[string]string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    headers,
		Body:       `{"ok":true}`,
	}
}

func errResponse(status int, msg string, headers map[string]string) events.LambdaFunctionURLResponse {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return events.LambdaFunctionURLResponse{
		StatusCode: status,
		Headers:    headers,
		Body:       string(body),
	}
}

func (h *apiHandler) resolveServerViaMapping(ctx context.Context, game, region, server string) (string, string, error) {
	mapping, err := h.loadMapping(ctx)
	if err != nil {
		return "", "", err
	}
	gameID, regionKey, serverKey, g, s, lookupErr := mapping.Lookup(game, region, server)
	if lookupErr != nil {
		return "", "", fmt.Errorf("unknown server %s/%s/%s", game, region, server)
	}
	techID := serverid.Generate(g.Provider, regionKey, s.Identifier)
	label := servermap.DisplayLabel(gameID, regionKey, serverKey)
	return techID, label, nil
}

// resolveTarget resolves the subscription target from the request fields. A
// server requires a region; with only a region the target is every server in
// that region; with neither the target is every server of the game.
func (h *apiHandler) resolveTarget(ctx context.Context, game, region, server string) (pk, label, scopeType, gameID, regionKey string, err error) {
	gameNorm := servermap.NormalizeKey(game)
	regionNorm := servermap.NormalizeKey(region)
	serverNorm := servermap.NormalizeKey(server)

	if serverNorm != "" && regionNorm == "" {
		return "", "", "", "", "", fmt.Errorf("server requires a region")
	}
	if serverNorm != "" {
		techID, label, err := h.resolveServer(ctx, gameNorm, regionNorm, serverNorm)
		return techID, label, "", gameNorm, regionNorm, err
	}

	mapping, err := h.loadMapping(ctx)
	if err != nil {
		return "", "", "", "", "", err
	}
	if gameNorm == "" {
		return "", "", "", "", "", fmt.Errorf("game is required")
	}
	if _, ok := mapping.Games[gameNorm]; !ok {
		return "", "", "", "", "", fmt.Errorf("unknown game %s", game)
	}

	if regionNorm != "" {
		regions, err := mapping.ListRegions(gameNorm)
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("unknown game %s", game)
		}
		valid := false
		for _, r := range regions {
			if r == regionNorm {
				valid = true
				break
			}
		}
		if !valid {
			return "", "", "", "", "", fmt.Errorf("unknown region %s/%s", game, region)
		}
		return scope.Key(gameNorm, regionNorm), scope.Label(gameNorm, regionNorm), scope.TypeRegion, gameNorm, regionNorm, nil
	}

	return scope.Key(gameNorm, ""), scope.Label(gameNorm, ""), scope.TypeGame, gameNorm, "", nil
}

// ensureScopeBaseline creates the ScopeState row for a wildcard scope on first
// subscribe, computing counts from the catalog and current statuses so the
// current state never triggers a notification.
func (h *apiHandler) ensureScopeBaseline(ctx context.Context, gameID, region, scopeKey string) error {
	if h.scopes == nil {
		return nil
	}
	existing, err := h.scopes.Get(ctx, scopeKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	statusByServer := map[string]string{}
	if h.gameStatuses != nil {
		rows, err := h.gameStatuses.ListServerStatusesByGame(ctx, gameID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			statusByServer[row.ServerID] = row.Status
		}
	}

	mapping, err := h.loadMapping(ctx)
	if err != nil {
		return err
	}
	total, up := aggregate.Counts(mapping, statusByServer, gameID, region)
	now := time.Now().Unix()
	state := aggregate.DeriveState(total, up)
	st := models.ScopeState{
		ScopeKey:            scopeKey,
		UpCount:             up,
		DownCount:           total - up,
		TotalCount:          total,
		State:               state,
		StateSince:          now,
		LastNotifiedEpisode: aggregate.Episode(state, now),
		UpdatedAt:           now,
	}
	return h.scopes.Put(ctx, st)
}

func (h *apiHandler) loadMapping(ctx context.Context) (servermap.Mapping, error) {
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

func (h *apiHandler) proveWebhook(ctx context.Context, webhookURL, roleID string) error {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("wait", "1")
	u.RawQuery = q.Encode()

	content := "ServersUp ownership verification"
	if roleID != "" {
		content = fmt.Sprintf("<@&%s> %s", roleID, content)
	}
	payload := map[string]any{"content": content}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func normalizeWebhookURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if i := strings.Index(raw, "?"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := NewHandler()
	lambda.Start(handler.HandleRequest)
}
