package main

import (
	"context"

	"github.com/ServersUp/servers-up-backend/internal/discordbot"
	"github.com/ServersUp/servers-up-backend/internal/logsetup"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	logsetup.ConfigureDefaultFromEnv()
	handler := discordbot.NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
