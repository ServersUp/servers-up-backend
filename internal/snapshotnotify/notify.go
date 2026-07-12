package snapshotnotify

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// Publisher notifies the status snapshot Lambda when pollers persist status changes.
type Publisher interface {
	NotifyChanged(ctx context.Context, changed int) error
}

// Nop is a no-op Publisher (tests / unset STATUS_SNAPSHOT_FUNCTION_NAME).
type Nop struct{}

func (Nop) NotifyChanged(context.Context, int) error { return nil }

type lambdaAPI interface {
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

// LambdaPublisher async-invokes StatusSnapshotLambda.
type LambdaPublisher struct {
	client       lambdaAPI
	functionName string
}

// NewLambdaPublisher returns a Publisher that Event-invokes functionName.
func NewLambdaPublisher(client lambdaAPI, functionName string) *LambdaPublisher {
	return &LambdaPublisher{client: client, functionName: functionName}
}

// NotifyChanged async-invokes the snapshot function when changed > 0.
func (p *LambdaPublisher) NotifyChanged(ctx context.Context, changed int) error {
	if p == nil || p.client == nil || p.functionName == "" || changed <= 0 {
		return nil
	}
	_, err := p.client.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(p.functionName),
		InvocationType: types.InvocationTypeEvent,
	})
	if err != nil {
		return fmt.Errorf("snapshotnotify: invoke %s: %w", p.functionName, err)
	}
	slog.Info("requested status snapshot republish", "function", p.functionName, "changed", changed)
	return nil
}

// FromEnv builds a Publisher from STATUS_SNAPSHOT_FUNCTION_NAME.
// Empty env defaults to StatusSnapshotLambda (production). Set to "-" or "disabled" for Nop.
func FromEnv(client lambdaAPI) Publisher {
	name := strings.TrimSpace(os.Getenv("STATUS_SNAPSHOT_FUNCTION_NAME"))
	switch name {
	case "-", "disabled":
		return Nop{}
	case "":
		name = "StatusSnapshotLambda"
	}
	return NewLambdaPublisher(client, name)
}
