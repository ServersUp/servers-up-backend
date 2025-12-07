package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	// 1. First log message (visible)
	log.Printf("Processing request for ID: %s", event.ID)

	// 1. Use json.Marshal to convert the struct into a byte slice
	// We use the event struct directly.
	jsonBytes, err := json.Marshal(event)

	if err != nil {
		log.Printf("ERROR marshaling event to JSON: %v", err)
		return "", err
	}

	// 2. Convert the byte slice to a string
	jsonString := string(jsonBytes)

	return "Successfully processed event with data: " + jsonString, nil
}

func main() {
	lambda.Start(HandleRequest)
}
