package bnet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client handles communication with the Battle.net APIs.
type Client struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	token        string
	authURL      string
	apiURL       string
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			// Poller runs many concurrent requests to one regional API host; default
			// MaxIdleConnsPerHost (2) prevents meaningful keep-alive reuse.
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		httpClient:   defaultHTTPClient(),
		clientID:     clientID,
		clientSecret: clientSecret,
		authURL:      "https://oauth.battle.net/token",
		apiURL:       "https://%s.api.blizzard.com",
	}
}

// Authenticate obtains an OAuth2 access token from Battle.net.
// Game data requests must pass the token via Authorization: Bearer, not query params.
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

// RegionEndpoint describes how to reach the WoW Game Data API for a Battle.net region.
type RegionEndpoint struct {
	Scheme       string // https in production; http for tests
	APIHost      string // regional host, e.g. kr.api.blizzard.com
	APISubregion string // optional &region= when routing via us.api (legacy); prefer regional APIHost
	Namespace    string // e.g. dynamic-eu
	Locale       string // e.g. en_GB
}

func (ep RegionEndpoint) apiScheme() string {
	if ep.Scheme == "http" {
		return "http"
	}
	return "https"
}

// DefaultWoWRegionEndpoints matches Blizzard's regional API subdomains for retail WoW.
func DefaultWoWRegionEndpoints() map[string]RegionEndpoint {
	return map[string]RegionEndpoint{
		"us": {APIHost: "us.api.blizzard.com", Namespace: "dynamic-us", Locale: "en_US"},
		"eu": {APIHost: "eu.api.blizzard.com", Namespace: "dynamic-eu", Locale: "en_GB"},
		"kr": {APIHost: "kr.api.blizzard.com", Namespace: "dynamic-kr", Locale: "ko_KR"},
		"tw": {APIHost: "tw.api.blizzard.com", Namespace: "dynamic-tw", Locale: "zh_TW"},
	}
}

// ConnectedRealmsIndex is the connected-realm index API response.
type ConnectedRealmsIndex struct {
	ConnectedRealms []ConnectedRealmRef `json:"connected_realms"`
}

type ConnectedRealmRef struct {
	Href string `json:"href"`
}

// ConnectedRealmDetail is a connected-realm payload with member realm slugs.
type ConnectedRealmDetail struct {
	ID     int            `json:"id"`
	Realms []RealmSummary `json:"realms"`
}

type RealmSummary struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// BuildRealmConfigs walks connected-realm index + detail for one region.
// Realm names follow the endpoint locale (e.g. ko_KR, zh_TW).
func (c *Client) BuildRealmConfigs(ctx context.Context, ep RegionEndpoint) ([]RealmConfig, error) {
	if c.token == "" {
		return nil, fmt.Errorf("not authenticated")
	}

	index, err := c.getConnectedRealmIndex(ctx, ep)
	if err != nil {
		return nil, err
	}
	if index == nil || len(index.ConnectedRealms) == 0 {
		return nil, nil
	}

	bySlug := make(map[string]RealmConfig)
	for _, ref := range index.ConnectedRealms {
		if ref.Href == "" {
			continue
		}
		detail, err := c.getConnectedRealmByHref(ctx, ep, ref.Href)
		if err != nil {
			continue
		}
		if detail == nil || detail.ID == 0 {
			continue
		}
		for _, realm := range detail.Realms {
			slug := strings.TrimSpace(realm.Slug)
			if slug == "" {
				continue
			}
			name := strings.TrimSpace(realm.Name)
			if name == "" {
				name = slug
			}
			bySlug[slug] = RealmConfig{
				Name:             name,
				Slug:             slug,
				ConnectedRealmID: detail.ID,
			}
		}
	}

	realms := make([]RealmConfig, 0, len(bySlug))
	for _, r := range bySlug {
		realms = append(realms, r)
	}
	sortRealmConfigs(realms)
	return realms, nil
}

func sortRealmConfigs(realms []RealmConfig) {
	sort.Slice(realms, func(i, j int) bool {
		if realms[i].ConnectedRealmID != realms[j].ConnectedRealmID {
			return realms[i].ConnectedRealmID < realms[j].ConnectedRealmID
		}
		return realms[i].Slug < realms[j].Slug
	})
}

func (c *Client) getConnectedRealmIndex(ctx context.Context, ep RegionEndpoint) (*ConnectedRealmsIndex, error) {
	u := url.URL{
		Scheme: ep.apiScheme(),
		Host:   ep.APIHost,
		Path:   "/data/wow/connected-realm/index",
	}
	q := url.Values{}
	q.Set("namespace", ep.Namespace)
	q.Set("locale", ep.Locale)
	if ep.APISubregion != "" {
		q.Set("region", ep.APISubregion)
	}
	u.RawQuery = q.Encode()

	var out ConnectedRealmsIndex
	if err := c.getJSON(ctx, u.String(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getConnectedRealmByHref(ctx context.Context, ep RegionEndpoint, href string) (*ConnectedRealmDetail, error) {
	u, err := url.Parse(href)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Del("access_token")
	if q.Get("locale") == "" {
		q.Set("locale", ep.Locale)
	}
	if ep.APISubregion != "" && q.Get("region") == "" {
		q.Set("region", ep.APISubregion)
	}
	u.RawQuery = q.Encode()

	var out ConnectedRealmDetail
	if err := c.getJSON(ctx, u.String(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return err
	}
	return nil
}
