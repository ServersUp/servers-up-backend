package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Database struct {
	client    dynamodbAPI
	tableName string
}

type dynamodbAPI interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

const guildIDIndexName = "GuildIdIndex"

var ErrStatusUnchanged = errors.New("status unchanged")

func NewDatabase(client dynamodbAPI, tableName string) *Database {
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
	serverID := serverid.Generate(provider, region, identifier)

	// Read-before-write to avoid issuing a write request when unchanged.
	getOut, err := db.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(db.tableName),
		Key: map[string]types.AttributeValue{
			"gameId":   &types.AttributeValueMemberS{Value: gameID},
			"serverId": &types.AttributeValueMemberS{Value: serverID},
		},
		ProjectionExpression: aws.String("#status"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ConsistentRead: aws.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("dynamodb get error for %s: %w", serverID, err)
	}

	if len(getOut.Item) > 0 {
		var current struct {
			Status string `dynamodbav:"status"`
		}
		if err := attributevalue.UnmarshalMap(getOut.Item, &current); err == nil && current.Status == status {
			return ErrStatusUnchanged
		}
	}

	serverStatus := models.GameServerStatus{
		GameID:        gameID,
		ServerID:      serverID,
		Provider:      provider,
		Region:        region,
		Status:        status,
		LastUpdatedAt: now,
	}

	item, err := attributevalue.MarshalMap(serverStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal status for %s: %w", serverID, err)
	}

	// Only write when the status changes (or item doesn't exist yet).
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

// ListSubscriptionsByGuild returns every Discord subscription for the given guild ID.
// It queries the GuildIdIndex and paginates until all items are read.
func (db *Database) ListSubscriptionsByGuild(ctx context.Context, guildID string) ([]models.Subscription, error) {
	var out []models.Subscription
	var startKey map[string]types.AttributeValue

	for {
		qout, err := db.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(db.tableName),
			IndexName:              aws.String(guildIDIndexName),
			KeyConditionExpression: aws.String("guildId = :gid"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":gid": &types.AttributeValueMemberS{Value: guildID},
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query subscriptions by guild: %w", err)
		}

		for _, item := range qout.Items {
			var sub models.Subscription
			if err := attributevalue.UnmarshalMap(item, &sub); err != nil {
				continue
			}
			out = append(out, sub)
		}

		if qout.LastEvaluatedKey == nil {
			break
		}
		startKey = qout.LastEvaluatedKey
	}

	return out, nil
}

// DeleteSubscription removes a single subscription row if it belongs to the given guild and channel.
func (db *Database) DeleteSubscription(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
	_, err := db.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(db.tableName),
		Key: map[string]types.AttributeValue{
			"serverId":       &types.AttributeValueMemberS{Value: serverID},
			"subscriptionId": &types.AttributeValueMemberS{Value: subscriptionID},
		},
		ConditionExpression: aws.String("guildId = :g AND channelId = :c"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":g": &types.AttributeValueMemberS{Value: guildID},
			":c": &types.AttributeValueMemberS{Value: channelID},
		},
	})
	if err != nil {
		var cfe *types.ConditionalCheckFailedException
		if errors.As(err, &cfe) {
			return fmt.Errorf("subscription not found for this guild/channel: %w", err)
		}
		return fmt.Errorf("failed to delete subscription: %w", err)
	}
	return nil
}

// ListSubscriptionsByServer returns every Discord subscription for the given server ID.
// It paginates until all items are read.
func (db *Database) ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error) {
	var out []models.Subscription
	var startKey map[string]types.AttributeValue

	for {
		qout, err := db.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(db.tableName),
			KeyConditionExpression: aws.String("serverId = :sid"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":sid": &types.AttributeValueMemberS{Value: serverID},
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query subscriptions: %w", err)
		}

		for _, item := range qout.Items {
			var sub models.Subscription
			if err := attributevalue.UnmarshalMap(item, &sub); err != nil {
				continue
			}
			out = append(out, sub)
		}

		if qout.LastEvaluatedKey == nil {
			break
		}
		startKey = qout.LastEvaluatedKey
	}

	return out, nil
}
