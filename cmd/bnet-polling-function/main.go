package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type statusDB interface {
	SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error
}

type bnetClient interface {
	Authenticate(ctx context.Context) error
	GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*bnet.ConnectedRealmResponse, error)
}

// Handler manages the dependencies and lifecycle of the polling request.
type Handler struct {
	configProvider *config.Provider
	database       statusDB
	bnetClientID   string
	bnetSecret     string
}

// NewHandler loads AWS clients and secrets; on failure it logs and exits (see main).
func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	provider := config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg))

	clientIDPath := os.Getenv("BNET_CLIENT_ID_PATH")
	clientSecretPath := os.Getenv("BNET_CLIENT_SECRET_PATH")
	if clientIDPath == "" || clientSecretPath == "" {
		slog.Error("missing BNET_CLIENT_ID_PATH or BNET_CLIENT_SECRET_PATH")
		os.Exit(1)
	}

	clientID, err := provider.GetSecret(ctx, clientIDPath)
	if err != nil {
		slog.Error("failed to load bnet client id", "error", err)
		os.Exit(1)
	}
	clientSecret, err := provider.GetSecret(ctx, clientSecretPath)
	if err != nil {
		slog.Error("failed to load bnet client secret", "error", err)
		os.Exit(1)
	}

	return &Handler{
		configProvider: provider,
		database:       db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_TABLE_NAME")),
		bnetClientID:   clientID,
		bnetSecret:     clientSecret,
	}
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	slog.Info("Starting polling execution", "eventID", event.ID)

	var bnetConfig bnet.Config
	if err := h.configProvider.LoadJSONFromS3(ctx, os.Getenv("CONFIG_BUCKET"), os.Getenv("BNET_SERVER_CONFIG_PATH"), &bnetConfig); err != nil {
		return "", err
	}

	bnetClient := bnet.NewClient(h.bnetClientID, h.bnetSecret)
	summary, err := h.pollRealms(ctx, bnetClient, bnetConfig)
	if err != nil {
		return "", err
	}

	slog.Info("Polling Summary",
		"successful", summary.Successful,
		"up", summary.Up,
		"down", summary.Down,
		"errors", summary.Errors,
	)

	metrics.EmitCount(metrics.Namespace, "PollRealmSuccess", map[string]string{"gameId": "wow"}, int64(summary.Successful))
	if summary.Errors > 0 {
		metrics.EmitCount(metrics.Namespace, "PollRealmError", map[string]string{"gameId": "wow"}, int64(summary.Errors))
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

	semaphore := make(chan struct{}, 10)
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

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
