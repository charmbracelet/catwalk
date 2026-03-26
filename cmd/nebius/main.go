// Package main provides a command-line tool to fetch models from Nebius Tokenfactory
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
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// Model represents a model from the Nebius Token Factory API.
type Model struct {
	ID            string  `json:"id"`
	DisplayName   string  `json:"display_name"`
	ContextLength int64   `json:"context_length"`
	MaxOutput     int64   `json:"max_output"`
	Reasoning     bool    `json:"reasoning"`
	Pricing       Pricing `json:"pricing"`
}

// Pricing contains the pricing information for a model.
type Pricing struct {
	InputPerMillion     float64 `json:"input_per_million"`
	OutputPerMillion    float64 `json:"output_per_million"`
	CacheReadPerMillion float64 `json:"cache_read_per_million"`
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
		"https://api.tokenfactory.nebius.com/v1/models",
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
		APIKey:              os.Getenv("NEBIUS_API_KEY"),
		APIEndpoint:         "https://api.tokenfactory.nebius.com/v1",
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

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.DisplayName,
			CostPer1MIn:            model.Pricing.InputPerMillion,
			CostPer1MOut:           model.Pricing.OutputPerMillion,
			CostPer1MInCached:      model.Pricing.CacheReadPerMillion,
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.MaxOutput,
			CanReason:              model.Reasoning,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         false,
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
