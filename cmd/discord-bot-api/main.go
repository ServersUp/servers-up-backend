package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/ServersUp/servers-up-backend/internal/discordbot"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	handler := discordbot.NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
