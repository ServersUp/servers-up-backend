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
	log.Printf("Processing request for ID: %s", event.ID)
	// Example of using shared common logic
	user, _ := common.GetData(event.ID)
	return "Successfully processed user: " + user.Email, nil
}

func main() {
	lambda.Start(HandleRequest)
}
