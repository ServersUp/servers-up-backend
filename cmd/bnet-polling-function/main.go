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
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

var (
	ssmClient *ssm.Client
	s3Client  *s3.Client
	ddbClient *dynamodb.Client

	BNET_CLIENT_ID_PATH     string
	BNET_CLIENT_SECRET_PATH string
	CONFIG_BUCKET           string
	BNET_SERVER_CONFIG_PATH string
	DDB_TABLE_NAME          string
)

type Config struct {
	Region                 string        `json:"region"`
	Locale                 string        `json:"locale"`
	Realms                 []RealmConfig `json:"realms"`
	PollingIntervalSeconds int           `json:"polling_interval_seconds"`
}

type RealmConfig struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	ConnectedRealmID int    `json:"connected_realm_id"`
}

type BNetTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type ConnectedRealmResponse struct {
	Links              Links              `json:"_links"`
	ID                 int                `json:"id"`
	HasQueue           bool               `json:"has_queue"`
	Status             Status             `json:"status"`
	Population         Population         `json:"population"`
	Realms             []RealmDetail      `json:"realms"`
	MythicLeaderboards MythicLeaderboards `json:"mythic_leaderboards"`
	Auctions           Auctions           `json:"auctions"`
}

type Links struct {
	Self Self `json:"self"`
}

type Self struct {
	Href string `json:"href"`
}

type Status struct {
	Type string `json:"type"` // "UP" or "DOWN"
	Name string `json:"name"`
}

type Population struct {
	Type string `json:"type"` // "FULL", "HIGH", etc.
	Name string `json:"name"`
}

type RealmDetail struct {
	ID             int            `json:"id"`
	Region         Region         `json:"region"`
	ConnectedRealm ConnectedRealm `json:"connected_realm"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	Locale         string         `json:"locale"`
	Timezone       string         `json:"timezone"`
	Type           Type           `json:"type"`
	IsTournament   bool           `json:"is_tournament"`
	Slug           string         `json:"slug"`
}

type Region struct {
	Key  Key    `json:"key"`
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type Key struct {
	Href string `json:"href"`
}

type ConnectedRealm struct {
	Href string `json:"href"`
}

type Type struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type MythicLeaderboards struct {
	Href string `json:"href"`
}

type Auctions struct {
	Href string `json:"href"`
}

type GameServerStatus struct {
	// 🔑 Primary Key
	GameID   string `json:"gameId" dynamodbav:"gameId"`
	ServerID string `json:"serverId" dynamodbav:"serverId"`

	// 🌍 Identity / metadata
	Provider string `json:"provider" dynamodbav:"provider"` // e.g. battlenet
	Region   string `json:"region" dynamodbav:"region"`     // us, eu, kr, etc

	// 📊 State
	Status string `json:"status" dynamodbav:"status"` // UP | DOWN | DEGRADED

	// ⏱ Polling & freshness
	LastUpdatedAt int64 `json:"lastUpdatedAt" dynamodbav:"lastUpdatedAt"` // unix epoch seconds
	PolledAt      int64 `json:"polledAt" dynamodbav:"polledAt"`           // always updated

	// 🧠 Optional / extensible fields
	Meta map[string]any `json:"meta,omitempty" dynamodbav:"meta,omitempty"`
}

func init() {
	// Initialize the SDK once during the "cold start"
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	ssmClient = ssm.NewFromConfig(cfg)
	s3Client = s3.NewFromConfig(cfg)
	ddbClient = dynamodb.NewFromConfig(cfg)

	BNET_CLIENT_ID_PATH = os.Getenv("BNET_CLIENT_ID_PATH")
	BNET_CLIENT_SECRET_PATH = os.Getenv("BNET_CLIENT_SECRET_PATH")
	CONFIG_BUCKET = os.Getenv("CONFIG_BUCKET")
	BNET_SERVER_CONFIG_PATH = os.Getenv("BNET_SERVER_CONFIG_PATH")
	DDB_TABLE_NAME = os.Getenv("DDB_TABLE_NAME")
}

func HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	log.Printf("Processing request for ID: %s", event.ID)

	jsonBytes, err := json.Marshal(event)

	if err != nil {
		log.Printf("ERROR marshaling event to JSON: %v", err)
		return "", err
	}

	clientIdParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(BNET_CLIENT_ID_PATH),
		WithDecryption: aws.Bool(true),
	})

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client ID Parameter: %v", err)
		return "", err
	}

	clientSecretParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(BNET_CLIENT_SECRET_PATH),
		WithDecryption: aws.Bool(true),
	})

	clientID := *clientIdParameterOutput.Parameter.Value
	clientSecret := *clientSecretParameterOutput.Parameter.Value

	if err != nil {
		log.Printf("Couldn't retrieve Battle Net Client Secret Parameter: %v", err)
		return "", err
	}

	obj, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(CONFIG_BUCKET),
		Key:    aws.String(BNET_SERVER_CONFIG_PATH),
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

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for _, realm := range cfg.Realms {
		wg.Add(1)

		// START A GOROUTINE
		go func(r RealmConfig) {
			defer wg.Done()

			// ACQUIRE: This blocks if 5 workers are already active
			semaphore <- struct{}{}
			// RELEASE: This executes as soon as this goroutine finishes
			defer func() { <-semaphore }()

			url := fmt.Sprintf("https://us.api.blizzard.com/data/wow/connected-realm/%d?namespace=dynamic-us&locale=%s", r.ConnectedRealmID, cfg.Locale)

			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Connection", "close") // Use the fix we discussed!

			resp, err := httpClient.Do(req)
			if err != nil {
				log.Printf("❌ Failed to poll %s: %v", r.Name, err)
				return // Use return, not continue, inside a goroutine
			}
			defer resp.Body.Close()

			// ... Decode and process ...
			var connectedRealmResponse ConnectedRealmResponse
			if err := json.NewDecoder(resp.Body).Decode(&connectedRealmResponse); err != nil {
				log.Printf("❌ JSON error for %s: %v", r.Name, err)
				return
			}
			saveToDB(ctx, r.ConnectedRealmID, connectedRealmResponse.Status.Type)

		}(realm) // Pass the realm into the goroutine to avoid closure issues
	}

	wg.Wait() // Now this will correctly wait for all 80+ goroutines to finish

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

func saveToDB(ctx context.Context, connectedID int, status string) {
	now := time.Now().Unix()
	serverID := fmt.Sprintf("battlenet#us#%d", connectedID)
	serverStatus := GameServerStatus{
		GameID:        "wow",    // Partition Key
		ServerID:      serverID, // Sort Key
		Provider:      "battlenet",
		Region:        "us",
		Status:        status,
		LastUpdatedAt: now, // TODO: Update this only if status changes
		PolledAt:      now,
		Meta: map[string]any{
			"env": "production",
		},
	}

	// 2. Marshal the Go struct into a map of AttributeValues
	// This respects your `dynamodbav` tags perfectly.
	item, err := attributevalue.MarshalMap(serverStatus)
	if err != nil {
		log.Printf("❌ Failed to marshal status for %s: %v", serverID, err)
		return
	}

	// 3. PutItem call
	_, err = ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(DDB_TABLE_NAME),
		Item:      item,
	})

	if err != nil {
		log.Printf("❌ DynamoDB Error for server %s: %v", serverID, err)
	}
}

func main() {
	lambda.Start(HandleRequest)
}
