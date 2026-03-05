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

	"charm.land/catwalk/pkg/catwalk"
)

// Model represents a model from the Vercel API.
type Model struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	Created       int64    `json:"created"`
	OwnedBy       string   `json:"owned_by"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ContextWindow int64    `json:"context_window"`
	MaxTokens     int64    `json:"max_tokens"`
	Type          string   `json:"type"`
	Tags          []string `json:"tags"`
	Pricing       Pricing  `json:"pricing"`
}

// Pricing contains the pricing information for a model.
type Pricing struct {
	Input           string `json:"input,omitempty"`
	Output          string `json:"output,omitempty"`
	InputCacheRead  string `json:"input_cache_read,omitempty"`
	InputCacheWrite string `json:"input_cache_write,omitempty"`
	WebSearch       string `json:"web_search,omitempty"`
	Image           string `json:"image,omitempty"`
}

// ModelsResponse is the response structure for the Vercel models API.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func fetchVercelModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://ai-gateway.vercel.sh/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func main() {
	modelsResp, err := fetchVercelModels()
	if err != nil {
		log.Fatal("Error fetching Vercel models:", err)
	}

	vercelProvider := catwalk.Provider{
		Name:                "Vercel",
		ID:                  catwalk.InferenceProviderVercel,
		APIKey:              "$VERCEL_API_KEY",
		APIEndpoint:         "https://ai-gateway.vercel.sh/v1",
		Type:                catwalk.TypeVercel,
		DefaultLargeModelID: "anthropic/claude-sonnet-4",
		DefaultSmallModelID: "anthropic/claude-haiku-4.5",
		Models:              []catwalk.Model{},
		DefaultHeaders: map[string]string{
			"HTTP-Referer": "https://charm.land",
			"X-Title":      "Crush",
		},
	}

	for _, model := range modelsResp.Data {
		// Only include language models, skip embedding and image models
		if model.Type != "language" {
			continue
		}

		// Skip models without tool support
		if !slices.Contains(model.Tags, "tool-use") {
			continue
		}

		// Parse pricing
		costPer1MIn := 0.0
		costPer1MOut := 0.0
		costPer1MInCached := 0.0
		costPer1MOutCached := 0.0

		if model.Pricing.Input != "" {
			costPrompt, err := strconv.ParseFloat(model.Pricing.Input, 64)
			if err == nil {
				costPer1MIn = costPrompt * 1_000_000
			}
		}

		if model.Pricing.Output != "" {
			costCompletion, err := strconv.ParseFloat(model.Pricing.Output, 64)
			if err == nil {
				costPer1MOut = costCompletion * 1_000_000
			}
		}

		if model.Pricing.InputCacheRead != "" {
			costCached, err := strconv.ParseFloat(model.Pricing.InputCacheRead, 64)
			if err == nil {
				costPer1MInCached = costCached * 1_000_000
			}
		}

		if model.Pricing.InputCacheWrite != "" {
			costCacheWrite, err := strconv.ParseFloat(model.Pricing.InputCacheWrite, 64)
			if err == nil {
				costPer1MOutCached = costCacheWrite * 1_000_000
			}
		}

		// Check if model supports reasoning
		canReason := slices.Contains(model.Tags, "reasoning")

		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			// Base reasoning levels supported by most providers
			reasoningLevels = []string{"low", "medium", "high"}
			// Anthropic models support extended Vercel reasoning levels
			if strings.HasPrefix(model.ID, "anthropic/") {
				reasoningLevels = []string{"none", "minimal", "low", "medium", "high", "xhigh"}
			}
			defaultReasoning = "medium"
		}

		// Check if model supports images
		supportsImages := slices.Contains(model.Tags, "vision")

		// Calculate default max tokens
		defaultMaxTokens := model.MaxTokens
		if defaultMaxTokens == 0 {
			defaultMaxTokens = model.ContextWindow / 10
		}
		if defaultMaxTokens > 8000 {
			defaultMaxTokens = 8000
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			CostPer1MOutCached:     costPer1MOutCached,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		vercelProvider.Models = append(vercelProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, model.ContextWindow)
	}

	slices.SortFunc(vercelProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/vercel.json
	data, err := json.MarshalIndent(vercelProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Vercel provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/vercel.json", data, 0o600); err != nil {
		log.Fatal("Error writing Vercel provider config:", err)
	}

	fmt.Printf("Generated vercel.json with %d models\n", len(vercelProvider.Models))
}
