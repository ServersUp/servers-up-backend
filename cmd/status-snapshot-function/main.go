package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/ServersUp/servers-up-backend/internal/statussnapshot"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	logsetup.ConfigureDefaultFromEnv()

	ctx := context.Background()

	cfg, err := statussnapshot.LoadFromEnv(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	handler, err := cfg.Handler()
	if err != nil {
		slog.Error("failed to initialise statussnapshot handler", "error", err)
		os.Exit(1)
	}

	lambda.Start(handler.HandleRequest)
}
