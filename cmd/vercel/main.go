// Package main provides a command-line tool to fetch models from Vercel AI Gateway
// and generate a configuration file for the provider.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
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
		roundCost := func(v float64) float64 { return math.Round(v*1e11) / 1e11 }
		costPerTokenIn := 0.0
		costPerTokenOut := 0.0
		costPerTokenInCached := 0.0
		costPerTokenOutCached := 0.0

		if model.Pricing.Input != "" {
			costPrompt, err := strconv.ParseFloat(model.Pricing.Input, 64)
			if err == nil {
				costPerTokenIn = roundCost(costPrompt)
			}
		}

		if model.Pricing.Output != "" {
			costCompletion, err := strconv.ParseFloat(model.Pricing.Output, 64)
			if err == nil {
				costPerTokenOut = roundCost(costCompletion)
			}
		}

		// NOTE: catwalk's naming is confusing (see providers.go in hyper):
		// - cost_per_token_in_cached  = cache CREATION (write)
		// - cost_per_token_out_cached = cache READ
		// Vercel's API uses the intuitive names, so we map them accordingly.
		if model.Pricing.InputCacheRead != "" {
			costCacheRead, err := strconv.ParseFloat(model.Pricing.InputCacheRead, 64)
			if err == nil {
				costPerTokenOutCached = roundCost(costCacheRead)
			}
		}

		if model.Pricing.InputCacheWrite != "" {
			costCacheWrite, err := strconv.ParseFloat(model.Pricing.InputCacheWrite, 64)
			if err == nil {
				costPerTokenInCached = roundCost(costCacheWrite)
			}
		}

		// Check if model supports reasoning
		canReason := slices.Contains(model.Tags, "reasoning")

		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			switch {
			case strings.HasPrefix(model.ID, "anthropic/"):
				reasoningLevels = []string{"none", "minimal", "low", "medium", "high", "xhigh"}
				defaultReasoning = "medium"
			case strings.HasPrefix(model.ID, "deepseek/deepseek-v4"):
				reasoningLevels = []string{"low", "high", "max"}
				defaultReasoning = "high"
			case model.ID == "zai/glm-5.2":
				reasoningLevels = []string{"high", "xhigh"}
				defaultReasoning = "high"
			default:
				reasoningLevels = []string{"low", "medium", "high"}
				defaultReasoning = "medium"
			}
		}

		// Check if model supports images
		supportsImages := slices.Contains(model.Tags, "vision")

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPerTokenIn:         costPerTokenIn,
			CostPerTokenOut:        costPerTokenOut,
			CostPerTokenInCached:   costPerTokenInCached,
			CostPerTokenOutCached:  costPerTokenOutCached,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       cmp.Or(model.MaxTokens, model.ContextWindow/10),
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		vercelProvider.Models = append(vercelProvider.Models, m)
	}

	slices.SortFunc(vercelProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/vercel.json
	data, err := json.MarshalIndent(vercelProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Vercel provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/vercel.json", data, 0o600); err != nil {
		log.Fatal("Error writing Vercel provider config:", err)
	}

	fmt.Printf("Generated vercel.json with %d models\n", len(vercelProvider.Models))
}
