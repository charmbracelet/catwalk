// Package main provides a command-line tool to fetch models from Vercel AI Gateway
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
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

// Pricing represents pricing information from the API.
type Pricing struct {
	Input          string `json:"input"`
	Output         string `json:"output"`
	InputCacheRead string `json:"input_cache_read"`
}

// VercelModel represents a model from the Vercel AI Gateway /v1/models endpoint.
type VercelModel struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	Created       int64    `json:"created"`
	OwnedBy       string   `json:"owned_by"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ContextWindow int64    `json:"context_window"`
	MaxTokens     int64    `json:"max_tokens"`
	Type          string   `json:"type"` // "language", "embedding", "image"
	Tags          []string `json:"tags"` // e.g., ["reasoning", "tool-use", "vision"]
	Pricing       Pricing  `json:"pricing"`
}

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []VercelModel `json:"data"`
}

func fetchAIGatewayModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://ai-gateway.vercel.sh/v1/models",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &mr, nil
}

func parsePricing(pricing Pricing) (costIn, costOut, costInCached, costOutCached float64) {
	// API returns per-token prices, we need per-1M tokens
	const perMillion = 1_000_000

	if pricing.Input != "" {
		if v, err := strconv.ParseFloat(pricing.Input, 64); err == nil {
			costIn = v * perMillion
		}
	}

	if pricing.Output != "" {
		if v, err := strconv.ParseFloat(pricing.Output, 64); err == nil {
			costOut = v * perMillion
		}
	}

	if pricing.InputCacheRead != "" {
		if v, err := strconv.ParseFloat(pricing.InputCacheRead, 64); err == nil {
			costInCached = v * perMillion
			costOutCached = v * perMillion // Use same value for output cached
		}
	}

	return costIn, costOut, costInCached, costOutCached
}

func hasTag(tags []string, tag string) bool {
	return slices.Contains(tags, tag)
}

func main() {
	modelsResp, err := fetchAIGatewayModels()
	if err != nil {
		log.Fatal("Error fetching AI Gateway models: ", err)
	}

	if len(modelsResp.Data) == 0 {
		log.Fatal("No models returned from AI Gateway API")
	}

	provider := catwalk.Provider{
		Name:                "Vercel AI Gateway",
		ID:                  "ai-gateway",
		APIKey:              "$AI_GATEWAY_API_KEY",
		APIEndpoint:         "https://ai-gateway.vercel.sh/v1",
		Type:                catwalk.TypeAIGateway,
		DefaultLargeModelID: "anthropic/claude-sonnet-4",
		DefaultSmallModelID: "anthropic/claude-3.5-haiku",
		Models:              []catwalk.Model{},
	}

	skippedCount := 0
	for _, model := range modelsResp.Data {
		// Skip stealth models
		if strings.HasPrefix(model.ID, "stealth/") {
			skippedCount++
			continue
		}

		// Skip non-language models (embedding, image)
		if model.Type != "language" {
			skippedCount++
			continue
		}

		// Skip models without tool-use capability (needed for Crush)
		if !hasTag(model.Tags, "tool-use") {
			skippedCount++
			continue
		}

		// Skip models with context window less than 20k
		if model.ContextWindow < 20000 {
			skippedCount++
			continue
		}

		// Parse pricing from API
		costIn, costOut, costInCached, costOutCached := parsePricing(model.Pricing)

		// Determine capabilities from tags
		canReason := hasTag(model.Tags, "reasoning")
		supportsImages := hasTag(model.Tags, "vision")

		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// Use max_tokens for default_max_tokens, with reasonable default
		defaultMaxTokens := model.MaxTokens
		if defaultMaxTokens <= 0 {
			defaultMaxTokens = model.ContextWindow / 10
		}
		// Cap at reasonable value
		if defaultMaxTokens > 100000 {
			defaultMaxTokens = 100000
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            costIn,
			CostPer1MOut:           costOut,
			CostPer1MInCached:      costInCached,
			CostPer1MOutCached:     costOutCached,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		provider.Models = append(provider.Models, m)
	}

	// Sort models by name
	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Check if default models exist
	hasLarge := false
	hasSmall := false
	for _, m := range provider.Models {
		if m.ID == provider.DefaultLargeModelID {
			hasLarge = true
		}
		if m.ID == provider.DefaultSmallModelID {
			hasSmall = true
		}
	}

	// Fall back to first model if defaults not found
	if !hasLarge && len(provider.Models) > 0 {
		provider.DefaultLargeModelID = provider.Models[0].ID
		fmt.Printf("Warning: Default large model not found, using %s\n", provider.DefaultLargeModelID)
	}
	if !hasSmall && len(provider.Models) > 0 {
		provider.DefaultSmallModelID = provider.Models[0].ID
		fmt.Printf("Warning: Default small model not found, using %s\n", provider.DefaultSmallModelID)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider: ", err)
	}

	// Write to file
	if err := os.WriteFile("internal/providers/configs/ai-gateway.json", data, 0o600); err != nil {
		log.Fatal("Error writing provider config: ", err)
	}

	fmt.Printf("Successfully generated ai-gateway.json with %d models (skipped %d)\n",
		len(provider.Models), skippedCount)
}
