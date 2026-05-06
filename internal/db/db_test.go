package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDDB struct {
	updateIn  *dynamodb.UpdateItemInput
	updateOut *dynamodb.UpdateItemOutput
	updateErr error
}

func (f *fakeDDB) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateIn = params
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updateOut != nil {
		return f.updateOut, nil
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func (f *fakeDDB) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	panic("not used in these tests")
}
func (f *fakeDDB) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	panic("not used in these tests")
}
func (f *fakeDDB) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	panic("not used in these tests")
}

func TestSaveServerStatus_setsConditionalUpdate(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if err != nil {
		t.Fatal(err)
	}
	if f.updateIn == nil {
		t.Fatal("expected UpdateItem to be called")
	}
	if f.updateIn.ConditionExpression == nil || *f.updateIn.ConditionExpression == "" {
		t.Fatal("expected ConditionExpression to be set")
	}
	if got := *f.updateIn.ConditionExpression; got != "attribute_not_exists(#status) OR #status <> :s" {
		t.Fatalf("unexpected ConditionExpression: %q", got)
	}
	if f.updateIn.Key == nil || f.updateIn.Key["gameId"] == nil || f.updateIn.Key["serverId"] == nil {
		t.Fatalf("expected Key to include gameId and serverId, got %#v", f.updateIn.Key)
	}
}

func TestSaveServerStatus_returnsErrStatusUnchanged(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{updateErr: &types.ConditionalCheckFailedException{}}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if !errors.Is(err, ErrStatusUnchanged) {
		t.Fatalf("expected ErrStatusUnchanged, got %v", err)
	}
}

func TestSaveServerStatus_wrapsOtherErrors(t *testing.T) {
	t.Parallel()

	f := &fakeDDB{updateErr: errors.New("boom")}
	db := NewDatabase(f, "GameServerStatus")

	err := db.SaveServerStatus(context.Background(), "wow", "battlenet", "us", 57, "UP")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrStatusUnchanged) {
		t.Fatal("did not expect ErrStatusUnchanged")
	}
}

