package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

var (
	ssmClient *ssm.Client
	s3Client  *s3.Client
)

type Config struct {
	Region                 string  `json:"region"`
	Locale                 string  `json:"locale"`
	Realms                 []Realm `json:"realms"`
	PollingIntervalSeconds int     `json:"polling_interval_seconds"`
}

type Realm struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func init() {
	// Initialize the SDK once during the "cold start"
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	ssmClient = ssm.NewFromConfig(cfg)
	s3Client = s3.NewFromConfig(cfg)
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

	clientIDParameterPath := os.Getenv("BNET_CLIENT_ID_PATH")
	clientSecretParameterPath := os.Getenv("BNET_CLIENT_SECRET_PATH")

	// 2. Call SSM for the Client ID
	clientIdParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(clientIDParameterPath),
		WithDecryption: aws.Bool(true),
	})

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client ID Parameter: %v", err)
		return "", err
	}

	val := *clientIdParameterOutput.Parameter.Value
	log.Printf("Successfully fetched Client ID: %s", val)

	clientSecretParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(clientSecretParameterPath),
		WithDecryption: aws.Bool(true),
	})

	_ = clientSecretParameterOutput

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client Secret Parameter: %v", err)
		return "", err
	}

	obj, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(os.Getenv("CONFIG_BUCKET")),
		Key:    aws.String(os.Getenv("BNET_SERVER_CONFIG_PATH")),
	})

	bodyBytes, err := io.ReadAll(obj.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read S3 body: %w", err)
	}
	defer obj.Body.Close()

	// 2. Unmarshal into your Config struct (or a map if you're feeling lazy)
	var cfg Config
	if err := json.Unmarshal(bodyBytes, &cfg); err != nil {
		return "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 3. Marshal it back into a PRETTY byte slice
	// The "" is the prefix, and "  " (two spaces) is the indent
	prettyJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to generate pretty JSON: %w", err)
	}

	// 4. Print that masterpiece! 🎨
	log.Printf("Loaded Config:\n%s", string(prettyJSON))

	// 3. Convert the byte slice to a string
	jsonString := string(jsonBytes)

	return "Successfully processed event with data: " + jsonString, nil
}

func main() {
	lambda.Start(HandleRequest)
}
