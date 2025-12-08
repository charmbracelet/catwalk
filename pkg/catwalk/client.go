package catwalk

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const defaultURL = "http://localhost:8080"

// Client represents a client for the catwalk service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new client instance
// Uses CATWALK_URL environment variable or falls back to localhost:8080.
func New() *Client {
	return &Client{
		baseURL:    cmp.Or(os.Getenv("CATWALK_URL"), defaultURL),
		httpClient: &http.Client{},
	}
}

// NewWithURL creates a new client with a specific URL.
func NewWithURL(url string) *Client {
	return &Client{
		baseURL:    url,
		httpClient: &http.Client{},
	}
}

// ErrNotModified happens when the given ETag matches the server, so no update
// is needed.
var ErrNotModified = fmt.Errorf("not modified")

// GetProviders retrieves all available providers from the service.
func (c *Client) GetProviders(ctx context.Context, etag string) ([]Provider, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/v2/providers", c.baseURL),
		nil,
	)
	if err != nil {
		return nil, err
	}

	if etag != "" {
		req.Header.Add("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var providers []Provider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return providers, nil
}
