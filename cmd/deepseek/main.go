// Package main provides a command-line tool to fetch models from DeepSeek
// and generate a configuration file for the provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// APIResponse represents the response from the DeepSeek models endpoint.
type APIResponse struct {
	Object string      `json:"object"`
	Data   []APIModel `json:"data"`
}

// APIModel represents a model from the DeepSeek API.
type APIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelInfo contains the static configuration for a DeepSeek model.
type ModelInfo struct {
	ContextWindow          int64
	MaxOutputTokens        int64
	CostPer1MIn            float64
	CostPer1MOut           float64
	CostPer1MInCached      float64
	CostPer1MOutCached     float64
	CanReason              bool
	DefaultReasoningEffort string
	ReasoningLevels        []string
}

var modelConfigs = map[string]ModelInfo{
	"deepseek-v4-flash": {
		ContextWindow:      1_000_000,
		MaxOutputTokens:    384_000,
		CostPer1MIn:        0.14,
		CostPer1MOut:       0.28,
		CostPer1MInCached:  0.028,
		CostPer1MOutCached: 0,
		CanReason:          true,
		DefaultReasoningEffort: "high",
		ReasoningLevels:        []string{"high", "max"},
	},
	"deepseek-v4-pro": {
		ContextWindow:      1_000_000,
		MaxOutputTokens:    384_000,
		CostPer1MIn:        0.435,
		CostPer1MOut:       0.87,
		CostPer1MInCached:  0.03625,
		CostPer1MOutCached: 0,
		CanReason:          true,
		DefaultReasoningEffort: "high",
		ReasoningLevels:        []string{"high", "max"},
	},
	"deepseek-chat": {
		ContextWindow:      1_000_000,
		MaxOutputTokens:    384_000,
		CostPer1MIn:        0.14,
		CostPer1MOut:       0.28,
		CostPer1MInCached:  0.028,
		CostPer1MOutCached: 0,
		CanReason:          false,
	},
	"deepseek-reasoner": {
		ContextWindow:      1_000_000,
		MaxOutputTokens:    384_000,
		CostPer1MIn:        0.14,
		CostPer1MOut:       0.28,
		CostPer1MInCached:  0.028,
		CostPer1MOutCached: 0,
		CanReason:          true,
		DefaultReasoningEffort: "high",
		ReasoningLevels:        []string{"high", "max"},
	},
}

var prettyNames = map[string]string{
	"deepseek-v4-flash":  "DeepSeek-V4-Flash",
	"deepseek-v4-pro":     "DeepSeek-V4-Pro",
	"deepseek-chat":       "DeepSeek-V3.2 (Non-thinking Mode)",
	"deepseek-reasoner":   "DeepSeek-V3.2 (Thinking Mode)",
}

var deprecatedModels = map[string]bool{
	"deepseek-chat":     true,
	"deepseek-reasoner": true,
}

func fetchDeepSeekModels(apiKey string) (*APIResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.deepseek.com/v1/models",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/deepseek-response.json", body, 0o600)

	var ar APIResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &ar, nil
}

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY environment variable is not set")
	}

	apiResp, err := fetchDeepSeekModels(apiKey)
	if err != nil {
		log.Fatal("Error fetching DeepSeek models:", err)
	}

	// Build a set of known model IDs from the API response
	apiModelIDs := make(map[string]bool)
	for _, m := range apiResp.Data {
		apiModelIDs[m.ID] = true
	}

	provider := catwalk.Provider{
		Name:                "DeepSeek",
		ID:                  "deepseek",
		APIKey:              "$DEEPSEEK_API_KEY",
		APIEndpoint:         "https://api.deepseek.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek-v4-flash",
		DefaultSmallModelID: "deepseek-chat",
	}

	// Build models list from our static config, keeping only models
	// that are confirmed active in the API response.
	var modelIDs []string
	for id := range modelConfigs {
		if apiModelIDs[id] {
			modelIDs = append(modelIDs, id)
		}
	}
	slices.Sort(modelIDs)

	for _, id := range modelIDs {
		cfg := modelConfigs[id]
		name := prettyNames[id]
		if name == "" {
			name = id
		}

		defaultMaxTokens := cfg.MaxOutputTokens / 10
		if cfg.CanReason {
			defaultMaxTokens = cfg.MaxOutputTokens / 12
		}

		m := catwalk.Model{
			ID:                     id,
			Name:                   name,
			CostPer1MIn:            cfg.CostPer1MIn,
			CostPer1MOut:           cfg.CostPer1MOut,
			CostPer1MInCached:      cfg.CostPer1MInCached,
			CostPer1MOutCached:     cfg.CostPer1MOutCached,
			ContextWindow:          cfg.ContextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              cfg.CanReason,
			DefaultReasoningEffort: cfg.DefaultReasoningEffort,
			ReasoningLevels:        cfg.ReasoningLevels,
			SupportsImages:         false,
		}

		provider.Models = append(provider.Models, m)
		deprecated := ""
		if deprecatedModels[id] {
			deprecated = " (deprecated)"
		}
		fmt.Printf("Added model %s%s\n", id, deprecated)
	}

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling DeepSeek provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/deepseek.json", data, 0o600); err != nil {
		log.Fatal("Error writing DeepSeek provider config:", err)
	}

	fmt.Printf("\nGenerated deepseek.json with %d models\n", len(provider.Models))
}
