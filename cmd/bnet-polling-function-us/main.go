package main

import (
	"context"

	"github.com/ServersUp/servers-up-backend/internal/bnetpoller"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	bnetpoller.ConfigureLogging()
	handler := bnetpoller.NewHandler(context.Background())
	lambda.Start(handler.HandleRequest)
}
