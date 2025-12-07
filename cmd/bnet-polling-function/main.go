package main

import (
	"context"
	"log"

	"github.com/ServersUp/servers-up-backend/internal/common"
	"github.com/aws/aws-lambda-go/lambda"
)

type Event struct {
	ID string `json:"id"`
}

func HandleRequest(ctx context.Context, event Event) (string, error) {
	// 1. First log message (visible)
	log.Printf("Processing request for ID: %s", event.ID)

	user, err := common.GetData(event.ID)

	// 2. CHECK FOR ERROR
	if err != nil {
		// Now you will see this error message in your logs!
		log.Printf("ERROR: Failed to retrieve data for ID %s: %v", event.ID, err)

		// This causes the Lambda to fail gracefully and logs the complete output
		return "", err
	}

	// 3. Success log (now this will be reached on success)
	log.Printf("Successfully retrieved user: %s", user.Email)

	return "Successfully processed user with data: " + user.Email, nil
}

func main() {
	lambda.Start(HandleRequest)
}
