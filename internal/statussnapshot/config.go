package statussnapshot

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ServersUp/servers-up-backend/internal/config"
	"github.com/ServersUp/servers-up-backend/internal/db"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// RuntimeConfig holds resolved dependencies for the status snapshot Lambda.
type RuntimeConfig struct {
	deps Deps
}

// LoadFromEnv reads required environment variables, wires AWS clients, and returns
// a RuntimeConfig ready to build a Handler.
//
// Required: CONFIG_BUCKET, SERVER_MAPPING_PATH, DDB_TABLE_NAME,
// STATUS_SNAPSHOT_BUCKET, STATUS_SNAPSHOT_KEY.
func LoadFromEnv(ctx context.Context) (*RuntimeConfig, error) {
	configBucket := os.Getenv("CONFIG_BUCKET")
	mappingKey := os.Getenv("SERVER_MAPPING_PATH")
	ddbTable := os.Getenv("DDB_TABLE_NAME")
	snapshotBucket := os.Getenv("STATUS_SNAPSHOT_BUCKET")
	snapshotKey := os.Getenv("STATUS_SNAPSHOT_KEY")

	var missing []string
	if configBucket == "" {
		missing = append(missing, "CONFIG_BUCKET")
	}
	if mappingKey == "" {
		missing = append(missing, "SERVER_MAPPING_PATH")
	}
	if ddbTable == "" {
		missing = append(missing, "DDB_TABLE_NAME")
	}
	if snapshotBucket == "" {
		missing = append(missing, "STATUS_SNAPSHOT_BUCKET")
	}
	if snapshotKey == "" {
		missing = append(missing, "STATUS_SNAPSHOT_KEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("statussnapshot: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("statussnapshot: unable to load AWS SDK config: %w", err)
	}

	provider := config.NewProvider(nil, s3.NewFromConfig(cfg))
	database := db.NewDatabase(dynamodb.NewFromConfig(cfg), ddbTable)
	s3Client := s3.NewFromConfig(cfg)

	return &RuntimeConfig{
		deps: Deps{
			ConfigLoader:     provider,
			StatusDB:         database,
			S3:               s3Client,
			ConfigBucket:     configBucket,
			ServerMappingKey: mappingKey,
			SnapshotBucket:   snapshotBucket,
			SnapshotKey:      snapshotKey,
		},
	}, nil
}

// Handler constructs a Handler from the RuntimeConfig.
func (c *RuntimeConfig) Handler() (*Handler, error) {
	return New(c.deps)
}
