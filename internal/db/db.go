package db

import (
	"context"
	"fmt"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type Database struct {
	client    *dynamodb.Client
	tableName string
}

func NewDatabase(client *dynamodb.Client, tableName string) *Database {
	return &Database{
		client:    client,
		tableName: tableName,
	}
}

// SaveServerStatus persists the current status of a game server to DynamoDB.
// It accepts gameID and provider to ensure the database layer remains agnostic of specific integrations.
func (db *Database) SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error {
	now := time.Now().Unix()
	
	// ServerID is constructed to be unique across all providers and regions.
	serverID := fmt.Sprintf("%s#%s#%v", provider, region, identifier)
	
	serverStatus := models.GameServerStatus{
		GameID:        gameID,
		ServerID:      serverID,
		Provider:      provider,
		Region:        region,
		Status:        status,
		LastUpdatedAt: now, // Status changes tracking logic should be implemented by the caller or a separate service.
		PolledAt:      now,
	}

	item, err := attributevalue.MarshalMap(serverStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal status for %s: %w", serverID, err)
	}

	_, err = db.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(db.tableName),
		Item:      item,
	})

	if err != nil {
		return fmt.Errorf("dynamodb put error for %s: %w", serverID, err)
	}

	return nil
}
