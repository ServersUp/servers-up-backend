package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// scopeStateAPI is satisfied by *dynamodb.Client.
type scopeStateAPI interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// ScopeStateStore persists aggregate scope state in the ScopeState table.
type ScopeStateStore struct {
	client    scopeStateAPI
	tableName string
}

// NewScopeStateStore creates a ScopeState store bound to a table name.
func NewScopeStateStore(client scopeStateAPI, tableName string) *ScopeStateStore {
	return &ScopeStateStore{client: client, tableName: tableName}
}

// Get returns the scope state for a wildcard key, or nil if none exists.
func (s *ScopeStateStore) Get(ctx context.Context, scopeKey string) (*models.ScopeState, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"scopeKey": &types.AttributeValueMemberS{Value: scopeKey},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get scope state %q: %w", scopeKey, err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var st models.ScopeState
	if err := attributevalue.UnmarshalMap(out.Item, &st); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scope state %q: %w", scopeKey, err)
	}
	return &st, nil
}

// Put writes a scope state row.
func (s *ScopeStateStore) Put(ctx context.Context, st models.ScopeState) error {
	item, err := attributevalue.MarshalMap(st)
	if err != nil {
		return fmt.Errorf("failed to marshal scope state: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to save scope state %q: %w", st.ScopeKey, err)
	}
	return nil
}

// List returns all scope state rows. The table is tiny: one row per currently
// subscribed wildcard scope.
func (s *ScopeStateStore) List(ctx context.Context) ([]models.ScopeState, error) {
	var out []models.ScopeState
	var startKey map[string]types.AttributeValue

	for {
		scanOut, err := s.client.Scan(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(s.tableName),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to scan scope states: %w", err)
		}
		for _, item := range scanOut.Items {
			var st models.ScopeState
			if err := attributevalue.UnmarshalMap(item, &st); err != nil {
				return nil, fmt.Errorf("failed to unmarshal scope state row: %w", err)
			}
			out = append(out, st)
		}
		if scanOut.LastEvaluatedKey == nil {
			break
		}
		startKey = scanOut.LastEvaluatedKey
	}

	return out, nil
}

// Delete removes the scope state row for a wildcard key.
func (s *ScopeStateStore) Delete(ctx context.Context, scopeKey string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"scopeKey": &types.AttributeValueMemberS{Value: scopeKey},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete scope state %q: %w", scopeKey, err)
	}
	return nil
}

// ClaimNotify atomically marks an aggregate episode as notified. It succeeds
// only if the scope is still in the same terminal state from the same
// stateSince and this episode has not been notified yet. Returns true when the
// caller won the claim (and should enqueue notifications); false when another
// runner already claimed the episode or the state moved on.
func (s *ScopeStateStore) ClaimNotify(ctx context.Context, st models.ScopeState, episode string) (bool, error) {
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"scopeKey": &types.AttributeValueMemberS{Value: st.ScopeKey},
		},
		UpdateExpression: aws.String("SET lastNotifiedEpisode = :ep, updatedAt = :now"),
		ConditionExpression: aws.String("state = :state AND stateSince = :since " +
			"AND (attribute_not_exists(lastNotifiedEpisode) OR lastNotifiedEpisode <> :ep)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":ep":    &types.AttributeValueMemberS{Value: episode},
			":state": &types.AttributeValueMemberS{Value: st.State},
			":since": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", st.StateSince)},
			":now":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Unix())},
		},
	})
	if err != nil {
		var cfe *types.ConditionalCheckFailedException
		if errors.As(err, &cfe) {
			return false, nil
		}
		return false, fmt.Errorf("failed to claim scope episode %s: %w", episode, err)
	}
	return true, nil
}
