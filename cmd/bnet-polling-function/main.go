package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

var ssmClient *ssm.Client

func init() {
	// Initialize the SDK once during the "cold start"
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	ssmClient = ssm.NewFromConfig(cfg)
}

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

	clientIDPath := "/serversup/bnet/client_id"

	// 2. Call SSM for the Client ID
	out, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(clientIDPath),
		WithDecryption: aws.Bool(true),
	})

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client Parameter: %v", err)
		return "", err
	}

	val := *out.Parameter.Value
	log.Printf("Successfully fetched Client ID: %s", val)

	// 3. Convert the byte slice to a string
	jsonString := string(jsonBytes)

	return "Successfully processed event with data: " + jsonString, nil
}

func main() {
	lambda.Start(HandleRequest)
}
