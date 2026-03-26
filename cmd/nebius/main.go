// Package main provides a command-line tool to fetch models from Nebius Token Factory
// and generate a configuration file for the provider.
package main

import (
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

// Model represents a model from the Nebius Token Factory API.
type Model struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"name"`
	ContextLength     int64    `json:"context_length"`
	MaxOutput         int64    `json:"max_output"`
	Reasoning         bool     `json:"reasoning"`
	SupportedFeatures []string `json:"supported_features,omitempty"`
	Pricing           Pricing  `json:"pricing"`
	Architecture      struct {
		Modality string `json:"modality"`
	} `json:"architecture,omitempty"`
}

// Pricing contains the pricing information for a model from the Nebius API.
type Pricing struct {
	Prompt              string `json:"prompt"`
	Completion          string `json:"completion"`
	Image               string `json:"image"`
	PricePerVideoSecond string `json:"price_per_video_second"`
	Request             string `json:"request"`
	PricePerMinute      string `json:"price_per_minute"`
}

// ModelsResponse is the response structure for the Nebius Token Factory models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func fetchNebiusModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.tokenfactory.nebius.com/v1/models?verbose=true",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	// Read API key from environment variable
	apiKey := os.Getenv("NEBIUS_API_KEY")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

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
	modelsResp, err := fetchNebiusModels()
	if err != nil {
		log.Fatal("Error fetching Nebius models:", err)
	}

	nebiusProvider := catwalk.Provider{
		Name:                "Nebius Token Factory",
		ID:                  catwalk.InferenceProviderNebius,
		APIKey:              "$NEBIUS_API_KEY",
		APIEndpoint:         "https://api.tokenfactory.nebius.com/v1", // this is their default region, eu-north1
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "Qwen/Qwen3-Coder-30B-A3B-Instruct",
		DefaultSmallModelID: "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		var reasoningLevels []string
		var defaultReasoning string
		if model.Reasoning {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// Convert pricing from string to float64
		var costPer1MIn, costPer1MOut, costPer1MInCached float64

		// Handle prompt price conversion
		promptPrice, err := strconv.ParseFloat(model.Pricing.Prompt, 64)
		if err != nil {
			promptPrice = 0.0
		}
		costPer1MIn = math.Round(promptPrice*1_000_000*100) / 100 // Round to 2 decimal places

		// Handle completion price conversion
		completionPrice, err := strconv.ParseFloat(model.Pricing.Completion, 64)
		if err != nil {
			completionPrice = 0.0
		}
		costPer1MOut = math.Round(completionPrice*1_000_000*100) / 100 // Round to 2 decimal places

		// Cache reading is typically charged similar to input
		cacheReadPrice, err := strconv.ParseFloat(model.Pricing.Request, 64)
		if err != nil {
			cacheReadPrice = 0.0
		}
		costPer1MInCached = math.Round(cacheReadPrice*1_000_000*100) / 100 // Round to 2 decimal places

		// Determine if reasoning is supported based on supported_features or the legacy Reasoning field
		canReason := model.Reasoning
		if !canReason && model.SupportedFeatures != nil {
			for _, feature := range model.SupportedFeatures {
				if feature == "reasoning" {
					canReason = true
					break
				}
			}
		}

		// Determine if model supports images based on modality
		supportsImages := false
		if model.Architecture.Modality != "" {
			// Check if the modality contains "image" anywhere in the string
			supportsImages = strings.Contains(strings.ToLower(model.Architecture.Modality), "image")
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.DisplayName,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.MaxOutput,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		nebiusProvider.Models = append(nebiusProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, model.ContextLength)
	}

	slices.SortFunc(nebiusProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/nebius.json
	data, err := json.MarshalIndent(nebiusProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Nebius provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/nebius.json", data, 0o600); err != nil {
		log.Fatal("Error writing Nebius provider config:", err)
	}

	fmt.Printf("Generated nebius.json with %d models\n", len(nebiusProvider.Models))
}
