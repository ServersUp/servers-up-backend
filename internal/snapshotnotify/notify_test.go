package snapshotnotify

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeLambda struct {
	calls int
	err   error
	last  *lambda.InvokeInput
}

func (f *fakeLambda) Invoke(_ context.Context, params *lambda.InvokeInput, _ ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	f.calls++
	f.last = params
	if f.err != nil {
		return nil, f.err
	}
	return &lambda.InvokeOutput{}, nil
}

func TestNotifyChanged_skipsWhenUnchanged(t *testing.T) {
	t.Parallel()
	f := &fakeLambda{}
	p := NewLambdaPublisher(f, "StatusSnapshotLambda")
	if err := p.NotifyChanged(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("expected no invoke, got %d", f.calls)
	}
}

func TestNotifyChanged_invokesAsync(t *testing.T) {
	t.Parallel()
	f := &fakeLambda{}
	p := NewLambdaPublisher(f, "StatusSnapshotLambda")
	if err := p.NotifyChanged(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("calls: %d", f.calls)
	}
	if *f.last.FunctionName != "StatusSnapshotLambda" {
		t.Fatalf("function: %v", f.last.FunctionName)
	}
	if f.last.InvocationType != types.InvocationTypeEvent {
		t.Fatalf("invocation type: %v", f.last.InvocationType)
	}
}

func TestNotifyChanged_wrapsError(t *testing.T) {
	t.Parallel()
	f := &fakeLambda{err: errors.New("boom")}
	p := NewLambdaPublisher(f, "StatusSnapshotLambda")
	if err := p.NotifyChanged(context.Background(), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestFromEnv_emptyDefaultsToStatusSnapshot(t *testing.T) {
	t.Setenv("STATUS_SNAPSHOT_FUNCTION_NAME", "")
	f := &fakeLambda{}
	p := FromEnv(f)
	if err := p.NotifyChanged(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || *f.last.FunctionName != "StatusSnapshotLambda" {
		t.Fatalf("calls=%d last=%v", f.calls, f.last)
	}
}

func TestFromEnv_disabledIsNop(t *testing.T) {
	t.Setenv("STATUS_SNAPSHOT_FUNCTION_NAME", "disabled")
	p := FromEnv(nil)
	if err := p.NotifyChanged(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
}
