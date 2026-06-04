package ffxivlodestone

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultUserAgentConfig = "ServersUp-ConfigGenerator/1.0 (+https://github.com/ServersUp/servers-up-backend)"
	defaultUserAgentPoller = "ServersUp-FFXIVPoller/1.0 (+https://github.com/ServersUp/servers-up-backend)"
)

// FetchWorldStatus downloads the Lodestone world status HTML page.
func FetchWorldStatus(ctx context.Context, url string, client *http.Client) ([]byte, error) {
	return fetchGET(ctx, url, "text/html,application/xhtml+xml", defaultUserAgentConfig, client)
}

// FetchFrontierStatus downloads the frontier world status JSON feed.
func FetchFrontierStatus(ctx context.Context, url string, client *http.Client) ([]byte, error) {
	return fetchGET(ctx, url, "application/json", defaultUserAgentPoller, client)
}

func fetchGET(ctx context.Context, url, accept, userAgent string, client *http.Client) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}
