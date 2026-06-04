package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDDB struct {
	getIn      *dynamodb.GetItemInput
	getOut     *dynamodb.GetItemOutput
	getErr     error
	putIn      *dynamodb.PutItemInput
	putOut     *dynamodb.PutItemOutput
	putErr     error
	queryOuts  []*dynamodb.QueryOutput
	queryErr   error
	queryCalls int
	deleteIn   *dynamodb.DeleteItemInput
	deleteErr  error
}

func (f *fakeDDB) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.getIn = params
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getOut != nil {
		return f.getOut, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (f *fakeDDB) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.putIn = params
	if f.putErr != nil {
		return nil, f.putErr
	}
	if f.putOut != nil {
		return f.putOut, nil
	}
	return &dynamodb.PutItemOutput{}, nil
}
func (f *fakeDDB) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.queryCalls++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.queryOuts) == 0 {
		return &dynamodb.QueryOutput{}, nil
	}
	out := f.queryOuts[0]
	f.queryOuts = f.queryOuts[1:]
	return out, nil
}

func (f *fakeDDB) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.deleteIn = params
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func TestSaveServerStatus_readsThenPuts(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if err != nil {
		t.Fatal(err)
	}
	if f.getIn == nil {
		t.Fatal("expected GetItem to be called")
	}
	if f.putIn == nil {
		t.Fatal("expected PutItem to be called")
	}
	if f.putIn.Item == nil {
		t.Fatal("expected PutItem Item to be set")
	}
}

func TestSaveServerStatus_returnsErrStatusUnchanged(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{
		getOut: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"status": &types.AttributeValueMemberS{Value: "UP"},
			},
		},
	}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if !errors.Is(err, ErrStatusUnchanged) {
		t.Fatalf("expected ErrStatusUnchanged, got %v", err)
	}
	if f.putIn != nil {
		t.Fatal("expected PutItem NOT to be called when status unchanged")
	}
}

func TestSaveServerStatus_wrapsOtherErrors(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{getErr: errors.New("boom")}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrStatusUnchanged) {
		t.Fatal("did not expect ErrStatusUnchanged")
	}
}

func TestSaveServerStatus_writesWhenStatusChanges(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{
		getOut: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"status": &types.AttributeValueMemberS{Value: "UP"},
			},
		},
	}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "DOWN")
	if err != nil {
		t.Fatal(err)
	}
	if f.putIn == nil {
		t.Fatal("expected PutItem to be called when status changes")
	}
}

func TestGetServerStatus_found(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{
		getOut: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"gameId":        &types.AttributeValueMemberS{Value: "wow"},
				"serverId":      &types.AttributeValueMemberS{Value: "battlenet#us#57"},
				"status":        &types.AttributeValueMemberS{Value: "UP"},
				"lastUpdatedAt": &types.AttributeValueMemberN{Value: "1710000000"},
			},
		},
	}
	db := NewDatabase(f, "GameServerStatus")

	got, err := db.GetServerStatus(context.Background(), "wow", "battlenet#us#57")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "UP" || got.GameID != "wow" {
		t.Fatalf("unexpected status: %#v", got)
	}
}

func TestGetServerStatus_notFound(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{}
	db := NewDatabase(f, "GameServerStatus")

	_, err := db.GetServerStatus(context.Background(), "wow", "battlenet#us#999")
	if !errors.Is(err, ErrServerStatusNotFound) {
		t.Fatalf("expected ErrServerStatusNotFound, got %v", err)
	}
}

func TestListSubscriptionsByGuild_returnsRows(t *testing.T) {
	t.Parallel()

	sub := models.Subscription{
		ServerID:       "battlenet#us#57",
		SubscriptionID: "sub-1",
		GuildID:        "guild-1",
		ChannelID:      "chan-1",
	}
	item, err := attributevalue.MarshalMap(sub)
	if err != nil {
		t.Fatal(err)
	}

	f := &fakeDDB{
		queryOuts: []*dynamodb.QueryOutput{{Items: []map[string]types.AttributeValue{item}}},
	}
	db := NewDatabase(f, "Subscriptions")

	got, err := db.ListSubscriptionsByGuild(context.Background(), "guild-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SubscriptionID != "sub-1" {
		t.Fatalf("unexpected subs: %#v", got)
	}
}

func TestListSubscriptionsByGuild_corruptRowFails(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{
		queryOuts: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{
				{"guildId": &types.AttributeValueMemberBOOL{Value: true}},
			},
		}},
	}
	db := NewDatabase(f, "Subscriptions")

	_, err := db.ListSubscriptionsByGuild(context.Background(), "guild-1")
	if !errors.Is(err, ErrCorruptSubscriptionRows) {
		t.Fatalf("expected ErrCorruptSubscriptionRows, got %v", err)
	}
}

func TestListSubscriptionsByServer_corruptRowFails(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{
		queryOuts: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{
				{"serverId": &types.AttributeValueMemberBOOL{Value: true}},
			},
		}},
	}
	db := NewDatabase(f, "Subscriptions")

	_, err := db.ListSubscriptionsByServer(context.Background(), "battlenet#us#57")
	if !errors.Is(err, ErrCorruptSubscriptionRows) {
		t.Fatalf("expected ErrCorruptSubscriptionRows, got %v", err)
	}
}

func TestDeleteSubscription_success(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{}
	db := NewDatabase(f, "Subscriptions")

	err := db.DeleteSubscription(context.Background(), "guild-1", "chan-1", "battlenet#us#57", "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if f.deleteIn == nil {
		t.Fatal("expected DeleteItem")
	}
}

func TestDeleteSubscription_notFound(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{deleteErr: &types.ConditionalCheckFailedException{}}
	db := NewDatabase(f, "Subscriptions")

	err := db.DeleteSubscription(context.Background(), "guild-1", "chan-1", "battlenet#us#57", "sub-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrCorruptSubscriptionRows) {
		t.Fatal("unexpected corrupt rows error")
	}
}

func TestListSubscriptionsByGuild_paginates(t *testing.T) {
	t.Parallel()

	sub1 := models.Subscription{ServerID: "battlenet#us#1", SubscriptionID: "sub-1", GuildID: "guild-1", ChannelID: "chan-1"}
	sub2 := models.Subscription{ServerID: "battlenet#us#2", SubscriptionID: "sub-2", GuildID: "guild-1", ChannelID: "chan-2"}
	item1, err := attributevalue.MarshalMap(sub1)
	if err != nil {
		t.Fatal(err)
	}
	item2, err := attributevalue.MarshalMap(sub2)
	if err != nil {
		t.Fatal(err)
	}

	f := &fakeDDB{
		queryOuts: []*dynamodb.QueryOutput{
			{
				Items:            []map[string]types.AttributeValue{item1},
				LastEvaluatedKey: map[string]types.AttributeValue{"serverId": &types.AttributeValueMemberS{Value: "token"}},
			},
			{
				Items: []map[string]types.AttributeValue{item2},
			},
		},
	}
	db := NewDatabase(f, "Subscriptions")

	got, err := db.ListSubscriptionsByGuild(context.Background(), "guild-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subscriptions across pages, got %d", len(got))
	}
	if f.queryCalls != 2 {
		t.Fatalf("expected 2 Query calls for pagination, got %d", f.queryCalls)
	}
	ids := map[string]bool{got[0].SubscriptionID: true, got[1].SubscriptionID: true}
	if !ids["sub-1"] || !ids["sub-2"] {
		t.Fatalf("unexpected subscription IDs: %v", got)
	}
}

func TestListSubscriptionsByServer_paginates(t *testing.T) {
	t.Parallel()

	sub1 := models.Subscription{ServerID: "battlenet#us#99", SubscriptionID: "sub-a", GuildID: "g1", ChannelID: "c1"}
	sub2 := models.Subscription{ServerID: "battlenet#us#99", SubscriptionID: "sub-b", GuildID: "g2", ChannelID: "c2"}
	item1, err := attributevalue.MarshalMap(sub1)
	if err != nil {
		t.Fatal(err)
	}
	item2, err := attributevalue.MarshalMap(sub2)
	if err != nil {
		t.Fatal(err)
	}

	f := &fakeDDB{
		queryOuts: []*dynamodb.QueryOutput{
			{
				Items:            []map[string]types.AttributeValue{item1},
				LastEvaluatedKey: map[string]types.AttributeValue{"subscriptionId": &types.AttributeValueMemberS{Value: "tok"}},
			},
			{
				Items: []map[string]types.AttributeValue{item2},
			},
		},
	}
	db := NewDatabase(f, "Subscriptions")

	got, err := db.ListSubscriptionsByServer(context.Background(), "battlenet#us#99")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subscriptions across pages, got %d", len(got))
	}
	if f.queryCalls != 2 {
		t.Fatalf("expected 2 Query calls for pagination, got %d", f.queryCalls)
	}
}

func TestSaveServerStatus_putError(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{putErr: errors.New("dynamodb put error")}
	db := NewDatabase(f, "GameServerStatus")

	// GetItem returns empty (no existing item), so we proceed to PutItem which fails.
	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if err == nil {
		t.Fatal("expected error from PutItem failure")
	}
	if errors.Is(err, ErrStatusUnchanged) {
		t.Fatal("expected a real error, not ErrStatusUnchanged")
	}
}

