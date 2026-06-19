package bnetpoller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/aws/aws-lambda-go/events"
)

// pollRealmConcurrency limits parallel Battle.net API and DynamoDB calls per poll.
const pollRealmConcurrency = 20

type statusDB interface {
	SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error
}

type bnetClient interface {
	Authenticate(ctx context.Context) error
	GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*bnet.ConnectedRealmResponse, error)
}

type configLoader interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

// Deps holds all dependencies for a Battle.net polling handler. AWS wiring and
// secret resolution are handled by LoadFromEnv; use New directly for pure injection.
type Deps struct {
	ConfigLoader     configLoader
	StatusDB         statusDB
	BnetClientID     string
	BnetClientSecret string
	ConfigBucket     string
	ConfigKey        string
}

// Handler manages dependencies and lifecycle for a Battle.net polling Lambda.
type Handler struct {
	configBucket   string
	configKey      string
	configProvider configLoader
	database       statusDB
	bnetClientID   string
	bnetSecret     string
}

// New constructs a Handler from fully-resolved dependencies. It returns an error
// rather than calling os.Exit so cmd callers can handle failures cleanly.
func New(deps Deps) (*Handler, error) {
	if deps.ConfigLoader == nil {
		return nil, errors.New("bnetpoller: ConfigLoader is required")
	}
	if deps.StatusDB == nil {
		return nil, errors.New("bnetpoller: StatusDB is required")
	}
	if deps.BnetClientID == "" || deps.BnetClientSecret == "" {
		return nil, errors.New("bnetpoller: BnetClientID and BnetClientSecret are required")
	}
	if deps.ConfigBucket == "" || deps.ConfigKey == "" {
		return nil, errors.New("bnetpoller: ConfigBucket and ConfigKey are required")
	}
	return &Handler{
		configBucket:   deps.ConfigBucket,
		configKey:      deps.ConfigKey,
		configProvider: deps.ConfigLoader,
		database:       deps.StatusDB,
		bnetClientID:   deps.BnetClientID,
		bnetSecret:     deps.BnetClientSecret,
	}, nil
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	return h.handleRequestWithClient(ctx, event, bnet.NewClient(h.bnetClientID, h.bnetSecret))
}

func (h *Handler) handleRequestWithClient(ctx context.Context, event events.CloudWatchEvent, client bnetClient) (string, error) {
	slog.Info("Starting polling execution", "eventID", event.ID)

	var bnetConfig bnet.Config
	if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.configKey, &bnetConfig); err != nil {
		return "", err
	}

	summary, err := h.pollRealms(ctx, client, bnetConfig)
	if err != nil {
		return "", err
	}

	slog.Info("Polling Summary",
		"successful", summary.Successful,
		"up", summary.Up,
		"down", summary.Down,
		"errors", summary.Errors,
		"bnetRegion", bnetConfig.Region,
	)

	metrics.EmitCount(metrics.Namespace, "PollRealmSuccess", map[string]string{"gameId": "wow", "bnetRegion": bnetConfig.Region}, int64(summary.Successful))
	if summary.Errors > 0 {
		metrics.EmitCount(metrics.Namespace, "PollRealmError", map[string]string{"gameId": "wow", "bnetRegion": bnetConfig.Region}, int64(summary.Errors))
	}

	return "Polling completed successfully", nil
}

type pollSummary struct {
	Successful int32
	Up         int32
	Down       int32
	Errors     int32
}

func (h *Handler) pollRealms(ctx context.Context, client bnetClient, bnetConfig bnet.Config) (pollSummary, error) {
	if err := client.Authenticate(ctx); err != nil {
		slog.Error("failed to authenticate with Battle.net", "error", err)
		return pollSummary{}, err
	}

	semaphore := make(chan struct{}, pollRealmConcurrency)
	var wg sync.WaitGroup

	var summary pollSummary

	for _, realm := range bnetConfig.Realms {
		wg.Add(1)
		go func(r bnet.RealmConfig) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			realmStatus, err := client.GetConnectedRealmStatus(ctx, bnetConfig.Region, r.ConnectedRealmID, bnetConfig.Locale)
			if err != nil {
				slog.Error("failed to poll realm", "realm", r.Name, "error", err)
				atomic.AddInt32(&summary.Errors, 1)
				return
			}

			statusType := realmStatus.Status.Type
			if statusType == "UP" {
				atomic.AddInt32(&summary.Up, 1)
			} else if statusType == "DOWN" {
				atomic.AddInt32(&summary.Down, 1)
			}

			if err := h.database.SaveServerStatus(ctx, "wow", "battlenet", bnetConfig.Region, r.ConnectedRealmID, statusType); err != nil {
				if errors.Is(err, db.ErrStatusUnchanged) {
					atomic.AddInt32(&summary.Successful, 1)
					return
				}
				slog.Error("failed to save status for realm", "realm", r.Name, "error", err)
				atomic.AddInt32(&summary.Errors, 1)
				return
			}

			atomic.AddInt32(&summary.Successful, 1)
		}(realm)
	}

	wg.Wait()
	return summary, nil
}
