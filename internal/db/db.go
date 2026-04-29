package db

import (
	"context"
	"fmt"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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

// AddSubscription adds a new Discord channel subscription for a specific server.
func (db *Database) AddSubscription(ctx context.Context, sub models.Subscription) error {
	item, err := attributevalue.MarshalMap(sub)
	if err != nil {
		return fmt.Errorf("failed to marshal subscription: %w", err)
	}

	_, err = db.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(db.tableName),
		Item:      item,
	})

	if err != nil {
		return fmt.Errorf("failed to save subscription: %w", err)
	}

	return nil
}

// DeleteSubscriptionByChannel removes a subscription from a channel.
// Since we only have the channel ID from the interaction, we query by server ID first.
func (db *Database) DeleteSubscriptionByChannel(ctx context.Context, serverID, channelID string) (bool, error) {
	// Query all subscriptions for this server.
	out, err := db.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(db.tableName),
		KeyConditionExpression: aws.String("serverId = :sid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sid": &types.AttributeValueMemberS{Value: serverID},
		},
	})

	if err != nil {
		return false, fmt.Errorf("failed to query subscriptions: %w", err)
	}

	var found bool
	for _, item := range out.Items {
		var sub models.Subscription
		if err := attributevalue.UnmarshalMap(item, &sub); err != nil {
			continue
		}

		// Check if this subscription matches the target channel.
		if sub.ChannelID == channelID {
			_, err = db.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(db.tableName),
				Key: map[string]types.AttributeValue{
					"serverId":       &types.AttributeValueMemberS{Value: serverID},
					"subscriptionId": &types.AttributeValueMemberS{Value: sub.SubscriptionID},
				},
			})
			if err != nil {
				return false, fmt.Errorf("failed to delete subscription: %w", err)
			}
			found = true
			break
		}
	}

	return found, nil
}
