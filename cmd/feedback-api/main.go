package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/ServersUp/servers-up-backend/internal/feedback"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("load AWS config", "error", err)
		os.Exit(1)
	}

	fromEmail := os.Getenv("SES_FROM_EMAIL")
	toEmail := os.Getenv("SES_TO_EMAIL")

	sesClient := ses.NewFromConfig(awsCfg)

	mailer, err := feedback.NewSESMailer(sesClient, fromEmail, toEmail)
	if err != nil {
		logger.Error("create SES mailer", "error", err)
		os.Exit(1)
	}

	handler := feedback.NewHTTPHandler(mailer)

	logger.Info("feedback-api starting")
	lambda.Start(handler.HandleRequest)
}
