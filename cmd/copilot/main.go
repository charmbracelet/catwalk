// Package main implements a tool to fetch GitHub Copilot models and generate a Catwalk provider configuration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

type Response struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type APITokenResponse struct {
	Token     string                    `json:"token"`
	ExpiresAt int64                     `json:"expires_at"`
	Endpoints APITokenResponseEndpoints `json:"endpoints"`
}

type APITokenResponseEndpoints struct {
	API string `json:"api"`
}

type APIToken struct {
	APIKey      string
	ExpiresAt   time.Time
	APIEndpoint string
}

type Model struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Version            string     `json:"version"`
	Vendor             string     `json:"vendor"`
	Preview            bool       `json:"preview"`
	ModelPickerEnabled bool       `json:"model_picker_enabled"`
	Capabilities       Capability `json:"capabilities"`
	Policy             *Policy    `json:"policy,omitempty"`
}

type Capability struct {
	Family    string   `json:"family"`
	Type      string   `json:"type"`
	Tokenizer string   `json:"tokenizer"`
	Limits    Limits   `json:"limits"`
	Supports  Supports `json:"supports"`
}

type Limits struct {
	MaxContextWindowTokens int `json:"max_context_window_tokens,omitempty"`
	MaxOutputTokens        int `json:"max_output_tokens,omitempty"`
	MaxPromptTokens        int `json:"max_prompt_tokens,omitempty"`
}

type Supports struct {
	ToolCalls         bool `json:"tool_calls,omitempty"`
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	Vision            bool `json:"vision,omitempty"`
}

type Policy struct {
	State string `json:"state"`
	Terms string `json:"terms"`
}

var versionedModelRegexp = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	copilotModels, err := fetchCopilotModels()
	if err != nil {
		return err
	}

	// NOTE(@andreynering): Exclude versioned models and keep only the main version of each.
	copilotModels = slices.DeleteFunc(copilotModels, func(m Model) bool {
		return m.ID != m.Version || versionedModelRegexp.MatchString(m.ID) || strings.Contains(m.ID, "embedding")
	})

	catwalkModels := modelsToCatwalk(copilotModels)
	slices.SortStableFunc(catwalkModels, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	provider := catwalk.Provider{
		ID:                  catwalk.InferenceProviderCopilot,
		Name:                "GitHub Copilot",
		Models:              catwalkModels,
		APIEndpoint:         "https://api.githubcopilot.com",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-opus-4.6",
		DefaultSmallModelID: "claude-haiku-4.5",
	}
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to marshal json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile("internal/providers/configs/copilot.json", data, 0o600); err != nil {
		return fmt.Errorf("unable to write copilog.json: %w", err)
	}
	return nil
}

func fetchCopilotModels() ([]Model, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	oauthToken := copilotToken()
	if oauthToken == "" {
		return nil, fmt.Errorf("no OAuth token available")
	}

	// Step 1: Fetch API token from the token endpoint
	tokenURL := "https://api.github.com/copilot_internal/v2/token" //nolint:gosec
	tokenReq, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create token request: %w", err)
	}
	tokenReq.Header.Set("Accept", "application/json")
	tokenReq.Header.Set("Authorization", fmt.Sprintf("token %s", oauthToken))

	// Use approved integration ID to bypass client check
	tokenReq.Header.Set("Copilot-Integration-Id", "vscode-chat")
	tokenReq.Header.Set("User-Agent", "GitHubCopilotChat/0.1")

	client := &http.Client{}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("unable to make token request: %w", err)
	}
	defer tokenResp.Body.Close() //nolint:errcheck

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read token response body: %w", err)
	}

	if tokenResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from token endpoint: %d", tokenResp.StatusCode)
	}

	var tokenData APITokenResponse
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil {
		return nil, fmt.Errorf("unable to unmarshal token response: %w", err)
	}

	// Convert to APIToken
	expiresAt := time.Unix(tokenData.ExpiresAt, 0)
	apiToken := APIToken{
		APIKey:      tokenData.Token,
		ExpiresAt:   expiresAt,
		APIEndpoint: tokenData.Endpoints.API,
	}

	// Step 2: Use the dynamic endpoint from the token to fetch models
	modelsURL := apiToken.APIEndpoint + "/models"
	modelsReq, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create models request: %w", err)
	}
	modelsReq.Header.Set("Accept", "application/json")
	modelsReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken.APIKey))
	modelsReq.Header.Set("Copilot-Integration-Id", "vscode-chat")
	modelsReq.Header.Set("User-Agent", "GitHubCopilotChat/0.1")

	modelsResp, err := client.Do(modelsReq)
	if err != nil {
		return nil, fmt.Errorf("unable to make models request: %w", err)
	}
	defer modelsResp.Body.Close() //nolint:errcheck

	modelsBody, err := io.ReadAll(modelsResp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read models response body: %w", err)
	}

	if modelsResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from models endpoint: %d", modelsResp.StatusCode)
	}

	// for debugging
	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/copilot-response.json", modelsBody, 0o600)

	var data Response
	if err := json.Unmarshal(modelsBody, &data); err != nil {
		return nil, fmt.Errorf("unable to unmarshal json: %w", err)
	}
	return data.Data, nil
}

func modelsToCatwalk(m []Model) []catwalk.Model {
	models := make([]catwalk.Model, 0, len(m))
	for _, model := range m {
		models = append(models, modelToCatwalk(model))
	}
	return models
}

func modelToCatwalk(m Model) catwalk.Model {
	return catwalk.Model{
		ID:               m.ID,
		Name:             m.Name,
		DefaultMaxTokens: int64(m.Capabilities.Limits.MaxOutputTokens),
		ContextWindow:    int64(m.Capabilities.Limits.MaxContextWindowTokens),
		SupportsImages:   m.Capabilities.Supports.Vision,
	}
}

func copilotToken() string {
	if token := os.Getenv("COPILOT_TOKEN"); token != "" {
		return token
	}
	return tokenFromDisk()
}

func tokenFromDisk() string {
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		return ""
	}
	var content map[string]struct {
		User        string `json:"user"`
		OAuthToken  string `json:"oauth_token"`
		GitHubAppID string `json:"githubAppId"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return ""
	}
	if app, ok := content["github.com:Iv1.b507a08c87ecfe98"]; ok {
		return app.OAuthToken
	}
	return ""
}

func tokenFilePath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "github-copilot/apps.json")
	default:
		return filepath.Join(os.Getenv("HOME"), ".config/github-copilot/apps.json")
	}
}
