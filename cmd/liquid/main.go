// Package main provides a command-line tool to fetch the router models from
// Liquid Inference and generate a configuration file for the provider.
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

// Model represents a model from the Liquid models API.
type Model struct {
	ID            string   `json:"id"`
	MatchType     string   `json:"match_type"`
	ContextLength int64    `json:"context_length"`
	MaxTokens     int64    `json:"max_tokens"`
	Parameters    []string `json:"liquid_supported_parameters"`
}

// ModelsResponse is the response structure for the Liquid models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func fetchLiquidModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://router.inference.ai.exchange/v1/models",
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
	modelsResp, err := fetchLiquidModels()
	if err != nil {
		log.Fatal("Error fetching Liquid models:", err)
	}

	liquidProvider := catwalk.Provider{
		Name:                "Liquid Inference",
		ID:                  catwalk.InferenceProviderLiquid,
		APIKey:              "$LIQUID_API_KEY",
		APIEndpoint:         "https://router.inference.ai.exchange/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "liquid.auto",
		DefaultSmallModelID: "liquid.auto",
		Models:              []catwalk.Model{},
	}

	// The exchange lists every seller model. Only the router models are
	// included: each one picks the cheapest eligible offer per request, so
	// their price is not fixed and the cost fields stay at zero.
	for _, model := range modelsResp.Data {
		if model.MatchType != "router" {
			continue
		}
		canReason := slices.Contains(model.Parameters, "reasoning.effort")
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		liquidProvider.Models = append(liquidProvider.Models, catwalk.Model{
			ID:                     model.ID,
			Name:                   modelName(model.ID),
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.MaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         slices.Contains(model.Parameters, "input.image"),
		})
	}

	slices.SortFunc(liquidProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(liquidProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Liquid provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/liquid.json", data, 0o600); err != nil {
		log.Fatal("Error writing Liquid provider config:", err)
	}

	fmt.Printf("Generated liquid.json with %d models\n", len(liquidProvider.Models))
}

func modelName(id string) string {
	suffix := strings.TrimPrefix(id, "liquid.")
	return "Liquid " + strings.ToUpper(suffix[:1]) + suffix[1:]
}
