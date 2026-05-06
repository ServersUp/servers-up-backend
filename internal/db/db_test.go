package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDDB struct {
	getIn   *dynamodb.GetItemInput
	getOut  *dynamodb.GetItemOutput
	getErr  error
	putIn   *dynamodb.PutItemInput
	putOut  *dynamodb.PutItemOutput
	putErr  error
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
	panic("not used in these tests")
}
func (f *fakeDDB) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	panic("not used in these tests")
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

