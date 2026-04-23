package main

import (
	"context"
	"log"
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
		log.Fatalf("unable to load AWS SDK config: %v", err)
	}

	return &Handler{
		configProvider: config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg)),
		database:       db.NewDatabase(dynamodb.NewFromConfig(cfg), os.Getenv("DDB_TABLE_NAME")),
	}
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	log.Printf("Starting polling execution for ID: %s", event.ID)

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
		log.Printf("failed to authenticate with Battle.net: %v", err)
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
				log.Printf("failed to poll realm %s: %v", r.Name, err)
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
				log.Printf("failed to save status for realm %s: %v", r.Name, err)
			}

			atomic.AddInt32(&successCount, 1)
		}(realm)
	}

	wg.Wait()

	log.Printf("Polling Summary | Successful: %d | UP: %d | DOWN: %d | Errors: %d",
		atomic.LoadInt32(&successCount),
		atomic.LoadInt32(&upCount),
		atomic.LoadInt32(&downCount),
		atomic.LoadInt32(&errorCount),
	)

	return "Polling completed successfully", nil
}

func main() {
	handler := NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
