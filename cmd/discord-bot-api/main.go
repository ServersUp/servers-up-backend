package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Handler represents the Discord Bot API handler.
type Handler struct {
	// Add dependencies here (e.g., database, config provider)
}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) HandleRequest(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// Log request metadata for easier debugging in CloudWatch/AWS Console
	slog.Info("Received Discord Bot API request",
		"method", request.RequestContext.HTTP.Method,
		"path", request.RawPath,
		"ip", request.RequestContext.HTTP.SourceIP,
	)

	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Body:       "Discord Bot API placeholder - Deployment Successful",
	}, nil
}

func main() {
	// Configure structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	handler := NewHandler()
	lambda.Start(handler.HandleRequest)
}
