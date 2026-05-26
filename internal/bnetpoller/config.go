package bnetpoller

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
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// RuntimeConfig holds resolved credentials and wired dependencies for a
// Battle.net polling Lambda. Construct via LoadFromEnv.
type RuntimeConfig struct {
	configLoader configLoader
	statusDB     statusDB
	clientID     string
	clientSecret string
	configBucket string
	configKey    string
}

// LoadFromEnv reads required environment variables, wires AWS clients, resolves
// SSM secrets, and returns a RuntimeConfig ready to build a Handler. It returns
// an error (never calls os.Exit) so cmd entrypoints can handle failures cleanly.
//
// Required variables: CONFIG_BUCKET, BNET_SERVER_CONFIG_PATH,
// BNET_CLIENT_ID_PATH, BNET_CLIENT_SECRET_PATH, DDB_TABLE_NAME.
func LoadFromEnv(ctx context.Context) (*RuntimeConfig, error) {
	configBucket := os.Getenv("CONFIG_BUCKET")
	configKey := os.Getenv("BNET_SERVER_CONFIG_PATH")
	clientIDPath := os.Getenv("BNET_CLIENT_ID_PATH")
	clientSecretPath := os.Getenv("BNET_CLIENT_SECRET_PATH")
	ddbTable := os.Getenv("DDB_TABLE_NAME")

	var missing []string
	if configBucket == "" {
		missing = append(missing, "CONFIG_BUCKET")
	}
	if configKey == "" {
		missing = append(missing, "BNET_SERVER_CONFIG_PATH")
	}
	if clientIDPath == "" {
		missing = append(missing, "BNET_CLIENT_ID_PATH")
	}
	if clientSecretPath == "" {
		missing = append(missing, "BNET_CLIENT_SECRET_PATH")
	}
	if ddbTable == "" {
		missing = append(missing, "DDB_TABLE_NAME")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("bnetpoller: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("bnetpoller: unable to load AWS SDK config: %w", err)
	}

	provider := config.NewProvider(ssm.NewFromConfig(cfg), s3.NewFromConfig(cfg))

	clientID, err := provider.GetSecret(ctx, clientIDPath)
	if err != nil {
		return nil, fmt.Errorf("bnetpoller: failed to load bnet client ID: %w", err)
	}
	clientSecret, err := provider.GetSecret(ctx, clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("bnetpoller: failed to load bnet client secret: %w", err)
	}

	database := db.NewDatabase(dynamodb.NewFromConfig(cfg), ddbTable)

	return &RuntimeConfig{
		configLoader: provider,
		statusDB:     database,
		clientID:     clientID,
		clientSecret: clientSecret,
		configBucket: configBucket,
		configKey:    configKey,
	}, nil
}

// Deps builds a Deps struct from the resolved RuntimeConfig for use with New.
func (c *RuntimeConfig) Deps() Deps {
	return Deps{
		ConfigLoader:     c.configLoader,
		StatusDB:         c.statusDB,
		BnetClientID:     c.clientID,
		BnetClientSecret: c.clientSecret,
		ConfigBucket:     c.configBucket,
		ConfigKey:        c.configKey,
	}
}

// Handler constructs and returns a ready-to-use Handler from the RuntimeConfig.
func (c *RuntimeConfig) Handler() (*Handler, error) {
	return New(c.Deps())
}
