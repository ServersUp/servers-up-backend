package bnet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client handles communication with the Battle.net APIs.
type Client struct {
	httpClient *http.Client
	clientID     string
	clientSecret string
	token        string
	authURL      string
	apiURL       string
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		clientID:     clientID,
		clientSecret: clientSecret,
		authURL:      "https://oauth.battle.net/token",
		apiURL:       "https://%s.api.blizzard.com",
	}
}

// Authenticate obtains an OAuth2 access token from Battle.net.
// This token is required for all subsequent data requests.
func (c *Client) Authenticate(ctx context.Context) error {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, "POST", c.authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to authenticate: status %d", resp.StatusCode)
	}

	var authResp BNetTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return err
	}

	c.token = authResp.AccessToken
	return nil
}

// GetConnectedRealmStatus fetches the current status of a WoW connected realm.
// The namespace and locale are passed to support multi-region and multi-language polling.
func (c *Client) GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*ConnectedRealmResponse, error) {
	if c.token == "" {
		return nil, fmt.Errorf("not authenticated")
	}

	// Namespaces are region-specific in the Blizzard Data API (e.g., dynamic-us).
	namespace := fmt.Sprintf("dynamic-%s", region)
	baseURL := fmt.Sprintf(c.apiURL, region)
	url := fmt.Sprintf("%s/data/wow/connected-realm/%d?namespace=%s&locale=%s", baseURL, connectedRealmID, namespace, locale)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Connection", "close")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get realm status: status %d", resp.StatusCode)
	}

	var connectedRealmResponse ConnectedRealmResponse
	if err := json.NewDecoder(resp.Body).Decode(&connectedRealmResponse); err != nil {
		return nil, err
	}

	return &connectedRealmResponse, nil
}
