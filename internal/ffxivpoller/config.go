package ffxivpoller

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

// RuntimeConfig holds resolved dependencies for the FFXIV polling Lambda.
type RuntimeConfig struct {
	configLoader configLoader
	statusDB     statusDB
	configBucket string
	configKey    string
}

// LoadFromEnv reads required environment variables, wires AWS clients, and returns
// a RuntimeConfig ready to build a Handler.
//
// Required variables: CONFIG_BUCKET, FFXIV_LODESTONE_CONFIG_PATH, DDB_TABLE_NAME.
func LoadFromEnv(ctx context.Context) (*RuntimeConfig, error) {
	configBucket := os.Getenv("CONFIG_BUCKET")
	configKey := os.Getenv("FFXIV_LODESTONE_CONFIG_PATH")
	ddbTable := os.Getenv("DDB_TABLE_NAME")

	var missing []string
	if configBucket == "" {
		missing = append(missing, "CONFIG_BUCKET")
	}
	if configKey == "" {
		missing = append(missing, "FFXIV_LODESTONE_CONFIG_PATH")
	}
	if ddbTable == "" {
		missing = append(missing, "DDB_TABLE_NAME")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("ffxivpoller: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("ffxivpoller: unable to load AWS SDK config: %w", err)
	}

	provider := config.NewProvider(nil, s3.NewFromConfig(cfg))
	database := db.NewDatabase(dynamodb.NewFromConfig(cfg), ddbTable)

	return &RuntimeConfig{
		configLoader: provider,
		statusDB:     database,
		configBucket: configBucket,
		configKey:    configKey,
	}, nil
}

// Deps builds a Deps struct from the resolved RuntimeConfig.
func (c *RuntimeConfig) Deps() Deps {
	return Deps{
		ConfigLoader: c.configLoader,
		StatusDB:     c.statusDB,
		ConfigBucket: c.configBucket,
		ConfigKey:    c.configKey,
	}
}

// Handler constructs a Handler from the RuntimeConfig.
func (c *RuntimeConfig) Handler() (*Handler, error) {
	return New(c.Deps())
}
