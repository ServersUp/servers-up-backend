package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

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
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	ConnectedRealmID string `json:"connected_realm_id"`
}

type BNetTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
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
	log.Printf("Processing request for ID: %s", event.ID)

	jsonBytes, err := json.Marshal(event)

	if err != nil {
		log.Printf("ERROR marshaling event to JSON: %v", err)
		return "", err
	}

	clientIDParameterPath := os.Getenv("BNET_CLIENT_ID_PATH")
	clientSecretParameterPath := os.Getenv("BNET_CLIENT_SECRET_PATH")

	clientIdParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(clientIDParameterPath),
		WithDecryption: aws.Bool(true),
	})

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client ID Parameter: %v", err)
		return "", err
	}

	clientSecretParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(clientSecretParameterPath),
		WithDecryption: aws.Bool(true),
	})

	clientID := *clientIdParameterOutput.Parameter.Value
	clientSecret := *clientSecretParameterOutput.Parameter.Value

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

	cfg := &Config{}
	if err := json.Unmarshal(bodyBytes, &cfg); err != nil {
		return "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	token, err := getBNetToken(ctx, clientID, clientSecret)
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	httpClient := &http.Client{}

	for _, realm := range cfg.Realms {
		// Namespace is required! For US it's 'dynamic-us'
		url := fmt.Sprintf("https://us.api.blizzard.com/data/wow/connected-realm/%s?namespace=dynamic-us&locale=%s", realm.ConnectedRealmID, cfg.Locale)

		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Printf("❌ Failed to poll %s: %v", realm.Name, err)
			continue
		}
		defer resp.Body.Close()

		log.Printf("✅ Polled %s: Status %d", realm.Name, resp.StatusCode)
	}

	jsonString := string(jsonBytes)

	return "Successfully processed event with data: " + jsonString, nil
}

func getBNetToken(ctx context.Context, id, secret string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth.battle.net/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 4. Decode the result
	authResp := &BNetTokenResponse{}

	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", err
	}

	return authResp.AccessToken, nil
}

func main() {
	lambda.Start(HandleRequest)
}
