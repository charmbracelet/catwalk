// Package main provides a command-line tool to fetch models from Obscurify.ai
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
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// Pricing holds per-token prices in USD.
type Pricing struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
}

// Model represents a model from the Obscurify.ai models API.
type Model struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ContextLength int64   `json:"context_length"`
	Tools         bool    `json:"tools"`
	Pricing       Pricing `json:"pricing"`
}

// ModelsResponse is the response structure for the Obscurify.ai models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func fetchObscurifyModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating models request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

func main() {
	obscurifyProvider := catwalk.Provider{
		Name:                "Obscurify.ai",
		ID:                  catwalk.InferenceProviderObscurify,
		APIKey:              "$OBSCURIFY_API_KEY",
		APIEndpoint:         "https://obscurify.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "z-ai/glm-5.3",
		DefaultSmallModelID: "meta-llama/llama-3.3-70b-instruct",
		Models:              []catwalk.Model{},
	}

	// The models endpoint is public, no API key needed.
	modelsResp, err := fetchObscurifyModels(obscurifyProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Obscurify.ai models:", err)
	}

	for _, model := range modelsResp.Data {
		// Crush needs tool calling.
		if !model.Tools {
			continue
		}

		// Skip models with small context windows.
		if model.ContextLength < 20000 {
			continue
		}

		// The API does not publish max output, so use 10% of the context
		// window, capped at 32k.
		defaultMaxTokens := min(model.ContextLength/10, 32768)

		obscurifyProvider.Models = append(obscurifyProvider.Models, catwalk.Model{
			ID:               model.ID,
			Name:             model.Name,
			CostPer1MIn:      roundCost(model.Pricing.Prompt * 1_000_000),
			CostPer1MOut:     roundCost(model.Pricing.Completion * 1_000_000),
			ContextWindow:    model.ContextLength,
			DefaultMaxTokens: defaultMaxTokens,
		})
	}

	slices.SortFunc(obscurifyProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(obscurifyProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Obscurify.ai provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/obscurify.json", data, 0o600); err != nil {
		log.Fatal("Error writing Obscurify.ai provider config:", err)
	}

	fmt.Printf("Generated obscurify.json with %d models\n", len(obscurifyProvider.Models))
}
