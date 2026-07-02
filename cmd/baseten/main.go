// Package main provides a command-line tool to fetch models from Baseten
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

// BasetenModel represents a model from the Baseten Model APIs endpoint.
type BasetenModel struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ContextLength   int64    `json:"context_length"`
	MaxCompletion   int64    `json:"max_completion_tokens"`
	Pricing         Pricing  `json:"pricing"`
	Features        []string `json:"supported_features"`
	InputModalities []string `json:"input_modalities"`
}

// Pricing contains the per-token pricing for a model.
type Pricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

// ModelsResponse is the response structure for the Baseten models API.
type ModelsResponse struct {
	Data []BasetenModel `json:"data"`
}

func fetchBasetenModels() (*ModelsResponse, error) {
	apiKey := os.Getenv("BASETEN_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("BASETEN_API_KEY environment variable is not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://inference.baseten.co/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/baseten-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func hasFeature(m BasetenModel, feature string) bool {
	return slices.Contains(m.Features, feature)
}

func hasModality(m BasetenModel, modality string) bool {
	return slices.Contains(m.InputModalities, modality)
}

// parsePrice converts a per-token price string to cost per 1M tokens.
func parsePrice(perToken string) float64 {
	var v float64
	if err := json.Unmarshal([]byte(perToken), &v); err != nil {
		return 0
	}
	return math.Round(v*1e6*1e5) / 1e5
}

func main() {
	modelsResp, err := fetchBasetenModels()
	if err != nil {
		log.Fatal("Error fetching Baseten models:", err)
	}

	basetenProvider := catwalk.Provider{
		Name:                "Baseten",
		ID:                  catwalk.InferenceProviderBaseten,
		APIKey:              "$BASETEN_API_KEY",
		APIEndpoint:         "https://inference.baseten.co/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek-ai/DeepSeek-V4-Pro",
		DefaultSmallModelID: "openai/gpt-oss-120b",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		if !hasFeature(model, "tools") {
			continue
		}
		if !hasModality(model, "text") {
			continue
		}

		var (
			canReason        = hasFeature(model, "reasoning")
			reasoningLevels  []string
			defaultReasoning string
		)
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            parsePrice(model.Pricing.Prompt),
			CostPer1MOut:           parsePrice(model.Pricing.Completion),
			CostPer1MInCached:      parsePrice(model.Pricing.InputCacheRead),
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.MaxCompletion,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         hasModality(model, "image"),
		}

		basetenProvider.Models = append(basetenProvider.Models, m)
	}

	slices.SortFunc(basetenProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(basetenProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Baseten provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/baseten.json", data, 0o600); err != nil {
		log.Fatal("Error writing Baseten provider config:", err)
	}

	fmt.Printf("Generated baseten.json with %d models\n", len(basetenProvider.Models))
}
