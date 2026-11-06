// Package foreverpoller polls WoW Forever ruleset status (Battle.net, undocumented
// dynamic-forever-<region> namespace) and records it in the shared status table.
package foreverpoller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/ServersUp/servers-up-backend/internal/snapshotnotify"
	"github.com/aws/aws-lambda-go/events"
)

const (
	// GameID and Provider match the server-mapping.json entry. Forever shares the
	// battlenet provider; ruleset IDs must not collide with WoW connected-realm IDs.
	GameID   = "forever"
	Provider = "battlenet"

	pollConcurrency = 12
)

// Config is the poller config stored in S3 (one entry per Battle.net region).
type Config struct {
	Regions map[string]RegionConfig `json:"regions"`
}

type RegionConfig struct {
	Locale   string          `json:"locale"`
	Rulesets []RulesetConfig `json:"rulesets"`
}

type RulesetConfig struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	RulesetID int    `json:"ruleset_id"`
}

type statusDB interface {
	SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error
}

type bnetClient interface {
	Authenticate(ctx context.Context) error
	GetRulesetStatus(ctx context.Context, region string, rulesetID int, locale string) (*bnet.RulesetResponse, error)
}

type configLoader interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

// Deps holds all dependencies for a Handler. Use LoadFromEnv for AWS wiring.
type Deps struct {
	ConfigLoader     configLoader
	StatusDB         statusDB
	BnetClientID     string
	BnetClientSecret string
	ConfigBucket     string
	ConfigKey        string
	SnapshotNotify   snapshotnotify.Publisher
}

// Handler polls every configured ruleset on each invocation.
type Handler struct {
	configBucket   string
	configKey      string
	configProvider configLoader
	database       statusDB
	bnetClientID   string
	bnetSecret     string
	snapshotNotify snapshotnotify.Publisher
}

func New(deps Deps) (*Handler, error) {
	if deps.ConfigLoader == nil {
		return nil, errors.New("foreverpoller: ConfigLoader is required")
	}
	if deps.StatusDB == nil {
		return nil, errors.New("foreverpoller: StatusDB is required")
	}
	if deps.BnetClientID == "" || deps.BnetClientSecret == "" {
		return nil, errors.New("foreverpoller: BnetClientID and BnetClientSecret are required")
	}
	if deps.ConfigBucket == "" || deps.ConfigKey == "" {
		return nil, errors.New("foreverpoller: ConfigBucket and ConfigKey are required")
	}
	notify := deps.SnapshotNotify
	if notify == nil {
		notify = snapshotnotify.Nop{}
	}
	return &Handler{
		configBucket:   deps.ConfigBucket,
		configKey:      deps.ConfigKey,
		configProvider: deps.ConfigLoader,
		database:       deps.StatusDB,
		bnetClientID:   deps.BnetClientID,
		bnetSecret:     deps.BnetClientSecret,
		snapshotNotify: notify,
	}, nil
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	return h.handleRequestWithClient(ctx, event, bnet.NewClient(h.bnetClientID, h.bnetSecret))
}

func (h *Handler) handleRequestWithClient(ctx context.Context, event events.CloudWatchEvent, client bnetClient) (string, error) {
	slog.Info("Starting WoW Forever polling execution", "eventID", event.ID)

	var cfg Config
	if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.configKey, &cfg); err != nil {
		return "", err
	}

	summary, err := h.poll(ctx, client, cfg)
	if err != nil {
		return "", err
	}

	slog.Info("Polling Summary",
		"successful", summary.Successful,
		"changed", summary.Changed,
		"up", summary.Up,
		"down", summary.Down,
		"errors", summary.Errors,
		"rulesets", summary.Rulesets,
		"pollDurationMs", summary.PollDurationMs,
	)

	dims := map[string]string{"gameId": GameID}
	metrics.EmitCount(metrics.Namespace, "PollRealmSuccess", dims, int64(summary.Successful))
	if summary.Errors > 0 {
		metrics.EmitCount(metrics.Namespace, "PollRealmError", dims, int64(summary.Errors))
	}
	metrics.EmitMilliseconds(metrics.Namespace, "PollDurationMs", dims, summary.PollDurationMs)

	if err := h.snapshotNotify.NotifyChanged(ctx, int(summary.Changed)); err != nil {
		slog.Error("status snapshot notify failed", "error", err, "changed", summary.Changed)
	}

	return "Polling completed successfully", nil
}

type pollSummary struct {
	Successful     int32
	Changed        int32
	Up             int32
	Down           int32
	Errors         int32
	Rulesets       int32
	PollDurationMs int64
}

func (h *Handler) poll(ctx context.Context, client bnetClient, cfg Config) (pollSummary, error) {
	if err := client.Authenticate(ctx); err != nil {
		slog.Error("failed to authenticate with Battle.net", "error", err)
		return pollSummary{}, err
	}

	start := time.Now()
	sem := make(chan struct{}, pollConcurrency)
	var wg sync.WaitGroup
	var s pollSummary

	for region, rc := range cfg.Regions {
		for _, rs := range rc.Rulesets {
			s.Rulesets++
			wg.Add(1)
			go func(region, locale string, rs RulesetConfig) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				resp, err := client.GetRulesetStatus(ctx, region, rs.RulesetID, locale)
				if err != nil {
					// Never record a failed fetch (403/404/timeout) as DOWN.
					slog.Error("failed to poll ruleset", "region", region, "ruleset", rs.Name, "error", err)
					atomic.AddInt32(&s.Errors, 1)
					return
				}

				statusType := resp.Status.Type
				switch statusType {
				case "UP":
					atomic.AddInt32(&s.Up, 1)
				case "DOWN":
					atomic.AddInt32(&s.Down, 1)
				default:
					slog.Error("unexpected ruleset status", "region", region, "ruleset", rs.Name, "status", statusType)
					atomic.AddInt32(&s.Errors, 1)
					return
				}

				if saveErr := h.database.SaveServerStatus(ctx, GameID, Provider, region, rs.RulesetID, statusType); saveErr != nil {
					if errors.Is(saveErr, db.ErrStatusUnchanged) {
						atomic.AddInt32(&s.Successful, 1)
						return
					}
					slog.Error("failed to save ruleset status", "region", region, "ruleset", rs.Name, "error", saveErr)
					atomic.AddInt32(&s.Errors, 1)
					return
				}
				atomic.AddInt32(&s.Successful, 1)
				atomic.AddInt32(&s.Changed, 1)
			}(region, rc.Locale, rs)
		}
	}

	wg.Wait()
	s.PollDurationMs = time.Since(start).Milliseconds()
	return s, nil
}
