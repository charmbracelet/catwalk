package catwalk

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/charmbracelet/catwalk/internal/etag"
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

// Etag returns the ETag for the given data.
func Etag(data []byte) string { return etag.Of(data) }

// GetProviders retrieves all available providers from the service.
func (c *Client) GetProviders(ctx context.Context, etag string) ([]Provider, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/v2/providers", c.baseURL),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	if etag != "" {
		// It needs to be quoted:
		req.Header.Add("If-None-Match", fmt.Sprintf(`"%s"`, etag))
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
