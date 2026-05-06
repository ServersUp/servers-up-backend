package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// Handler manages the dependencies and lifecycle of the polling request.
type Handler struct {
	configProvider *config.Provider
	database       *db.Database
}

func NewHandler(ctx context.Context) *Handler {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	return &Handler{
		configProvider: config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg)),
		database:       db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_TABLE_NAME")),
	}
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	slog.Info("Starting polling execution", "eventID", event.ID)

	// Fetch credentials and server configuration from AWS.
	clientID, err := h.configProvider.GetSecret(ctx, os.Getenv("BNET_CLIENT_ID_PATH"))
	if err != nil {
		return "", err
	}

	clientSecret, err := h.configProvider.GetSecret(ctx, os.Getenv("BNET_CLIENT_SECRET_PATH"))
	if err != nil {
		return "", err
	}

	var bnetConfig bnet.Config
	err = h.configProvider.LoadJSONFromS3(ctx, os.Getenv("CONFIG_BUCKET"), os.Getenv("BNET_SERVER_CONFIG_PATH"), &bnetConfig)
	if err != nil {
		return "", err
	}

	// Initialize the Battle.net client and authenticate.
	bnetClient := bnet.NewClient(clientID, clientSecret)
	if err := bnetClient.Authenticate(ctx); err != nil {
		slog.Error("failed to authenticate with Battle.net", "error", err)
		return "", err
	}

	// Process realms in parallel to minimize total execution time.
	// A semaphore is used to limit concurrent connections to the Blizzard API.
	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup

	var (
		successCount int32
		errorCount   int32
		upCount      int32
		downCount    int32
	)

	for _, realm := range bnetConfig.Realms {
		wg.Add(1)
		go func(r bnet.RealmConfig) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			realmStatus, err := bnetClient.GetConnectedRealmStatus(ctx, bnetConfig.Region, r.ConnectedRealmID, bnetConfig.Locale)
			if err != nil {
				slog.Error("failed to poll realm", "realm", r.Name, "error", err)
				atomic.AddInt32(&errorCount, 1)
				return
			}

			statusType := realmStatus.Status.Type
			if statusType == "UP" {
				atomic.AddInt32(&upCount, 1)
			} else if statusType == "DOWN" {
				atomic.AddInt32(&downCount, 1)
			}

			// Store the status using the generalized database layer.
			if err := h.database.SaveServerStatus(ctx, "wow", "battlenet", bnetConfig.Region, r.ConnectedRealmID, statusType); err != nil {
				if err == db.ErrStatusUnchanged {
					// Status is unchanged; avoid counting/logging this as an error.
					atomic.AddInt32(&successCount, 1)
					return
				}
				slog.Error("failed to save status for realm", "realm", r.Name, "error", err)
				atomic.AddInt32(&errorCount, 1)
				return
			}

			atomic.AddInt32(&successCount, 1)
		}(realm)
	}

	wg.Wait()

	slog.Info("Polling Summary",
		"successful", atomic.LoadInt32(&successCount),
		"up", atomic.LoadInt32(&upCount),
		"down", atomic.LoadInt32(&downCount),
		"errors", atomic.LoadInt32(&errorCount),
	)

	return "Polling completed successfully", nil
}

func main() {
	// Configure slog to output JSON to stdout. This is the best practice for 
	// AWS Lambda as it allows CloudWatch Insights to parse logs automatically.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
