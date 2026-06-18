// Package main provides a command-line tool to fetch models from ZenMux.ai
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

// ModelsResponse is the OpenAI-compatible /v1/models response.
type ModelsResponse struct {
	Data []ZenMuxModel `json:"data"`
}

// ZenMuxModel represents a single model entry in the /v1/models response.
type ZenMuxModel struct {
	ID               string           `json:"id"`
	DisplayName      string           `json:"display_name"`
	InputModalities  []string         `json:"input_modalities"`
	OutputModalities []string         `json:"output_modalities"`
	Capabilities     ModelCapabilities `json:"capabilities"`
	ContextLength    int64            `json:"context_length"`
	Pricings         ModelPricings    `json:"pricings"`
}

// ModelCapabilities describes what the model can do.
type ModelCapabilities struct {
	Reasoning bool `json:"reasoning"`
}

// ModelPricings holds pricing arrays for different operations.
type ModelPricings struct {
	Prompt         []PricingEntry `json:"prompt"`
	Completion     []PricingEntry `json:"completion"`
	InputCacheRead []PricingEntry `json:"input_cache_read,omitempty"`
}

// PricingEntry is a single pricing tier.
type PricingEntry struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// firstPrice returns the first pricing entry's value, or 0 if empty.
func firstPrice(entries []PricingEntry) float64 {
	if len(entries) == 0 {
		return 0
	}
	return roundCost(entries[0].Value)
}

func fetchZenMuxModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://zenmux.ai/api/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/zenmux-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func main() {
	modelsResp, err := fetchZenMuxModels()
	if err != nil {
		log.Fatal("Error fetching ZenMux models:", err)
	}

	provider := catwalk.Provider{
		Name:                "ZenMux",
		ID:                  catwalk.InferenceProviderZenMux,
		APIKey:              "$ZENMUX_API_KEY",
		APIEndpoint:         "https://zenmux.ai/api/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "anthropic/claude-sonnet-4.5",
		DefaultSmallModelID: "anthropic/claude-haiku-4.5",
	}

	for _, model := range modelsResp.Data {
		// Only include text-in / text-out chat models.
		if !slices.Contains(model.InputModalities, "text") ||
			!slices.Contains(model.OutputModalities, "text") {
			continue
		}

		// Skip models with very small context windows.
		if model.ContextLength < 20000 {
			continue
		}

		canReason := model.Capabilities.Reasoning
		supportsImages := slices.Contains(model.InputModalities, "image")

		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.DisplayName,
			CostPer1MIn:            firstPrice(model.Pricings.Prompt),
			CostPer1MOut:           firstPrice(model.Pricings.Completion),
			CostPer1MInCached:      firstPrice(model.Pricings.InputCacheRead),
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.ContextLength / 10,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		provider.Models = append(provider.Models, m)
		fmt.Printf("Added model %s\n", model.ID)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling ZenMux provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/zenmux.json", data, 0o600); err != nil {
		log.Fatal("Error writing ZenMux provider config:", err)
	}

	fmt.Printf("Generated zenmux.json with %d models\n", len(provider.Models))
}
