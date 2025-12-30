// Package main provides a command-line tool to fetch models from ZenMux
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

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

// Pricing represents a single pricing entry
type Pricing struct {
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Currency string  `json:"currency"`
}

// ModelPricings contains all pricing information for a model
type ModelPricings struct {
	Prompt              []Pricing `json:"prompt"`
	Completion          []Pricing `json:"completion"`
	InputCacheRead      []Pricing `json:"input_cache_read"`
	InputCacheWrite5Min []Pricing `json:"input_cache_write_5_min"`
	InputCacheWrite1H   []Pricing `json:"input_cache_write_1_h"`
}

// Capabilities represents model capabilities
type Capabilities struct {
	Reasoning bool `json:"reasoning"`
}

// ZenMuxModel represents a model from ZenMux API with full details
type ZenMuxModel struct {
	ID              string        `json:"id"`
	DisplayName     string        `json:"display_name"`
	CreatedAt       string        `json:"created_at"`
	Type            string        `json:"type"`
	InputModalities []string      `json:"input_modalities"`
	OutputModalities []string     `json:"output_modalities"`
	Capabilities    Capabilities  `json:"capabilities"`
	ContextLength   int64         `json:"context_length"`
	Pricings        ModelPricings `json:"pricings"`
}

// ZenMuxModelsResponse is the response from ZenMux /api/anthropic/v1/models endpoint
type ZenMuxModelsResponse struct {
	Data    []ZenMuxModel `json:"data"`
	HasMore bool          `json:"has_more"`
}

func fetchZenMuxModels() (*ZenMuxModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://zenmux.ai/api/anthropic/v1/models",
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Catwalk-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var modelsResp ZenMuxModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, err
	}

	return &modelsResp, nil
}

// getPricing extracts a pricing value, returns 0 if not available
func getPricing(pricings []Pricing) float64 {
	if len(pricings) == 0 {
		return 0
	}
	return pricings[0].Value
}

// supportsImages checks if model supports image input
func supportsImages(modalities []string) bool {
	return slices.Contains(modalities, "image")
}

// estimateMaxTokens provides a reasonable estimate for max output tokens
func estimateMaxTokens(contextLength int64, canReason bool) int64 {
	if canReason {
		// Reasoning models typically allow larger outputs
		return min(contextLength/4, 50000)
	}
	// Standard models
	return min(contextLength/10, 20000)
}

func main() {
	fmt.Println("Fetching models from ZenMux API...")
	modelsResp, err := fetchZenMuxModels()
	if err != nil {
		log.Fatal("Error fetching ZenMux models:", err)
	}

	zenmuxProvider := catwalk.Provider{
		Name:                "ZenMux",
		ID:                  "zenmux",
		APIKey:              "$ZENMUX_API_KEY",
		APIEndpoint:         "https://zenmux.ai/api/anthropic",
		Type:                catwalk.TypeAnthropic,
		DefaultLargeModelID: "anthropic/claude-sonnet-4.5",
		DefaultSmallModelID: "anthropic/claude-3.5-haiku",
		Models:              []catwalk.Model{},
	}

	fmt.Printf("Found %d models from API\n\n", len(modelsResp.Data))

	addedCount := 0
	for _, model := range modelsResp.Data {
		// Filter: require at least 20k context and text I/O
		if model.ContextLength < 20000 {
			continue
		}
		if !slices.Contains(model.InputModalities, "text") ||
			!slices.Contains(model.OutputModalities, "text") {
			continue
		}

		// Extract pricing information
		costIn := getPricing(model.Pricings.Prompt)
		costOut := getPricing(model.Pricings.Completion)
		costInCached := getPricing(model.Pricings.InputCacheRead)
		// Use 5-minute cache write price as it's more commonly used
		costOutCached := getPricing(model.Pricings.InputCacheWrite5Min)

		// Build the model configuration
		m := catwalk.Model{
			ID:                 model.ID,
			Name:               model.DisplayName,
			CostPer1MIn:        costIn,
			CostPer1MOut:       costOut,
			CostPer1MInCached:  costInCached,
			CostPer1MOutCached: costOutCached,
			ContextWindow:      model.ContextLength,
			DefaultMaxTokens:   estimateMaxTokens(model.ContextLength, model.Capabilities.Reasoning),
			CanReason:          model.Capabilities.Reasoning,
			SupportsImages:     supportsImages(model.InputModalities),
		}

		zenmuxProvider.Models = append(zenmuxProvider.Models, m)
		addedCount++

		reasoningStr := ""
		if model.Capabilities.Reasoning {
			reasoningStr = " [Reasoning]"
		}
		visionStr := ""
		if m.SupportsImages {
			visionStr = " [Vision]"
		}
		fmt.Printf("✓ %s%s%s\n", model.DisplayName, reasoningStr, visionStr)
	}

	// Sort by name for better readability
	slices.SortFunc(zenmuxProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("  Total from API: %d\n", len(modelsResp.Data))
	fmt.Printf("  Added to config: %d\n", addedCount)
	fmt.Printf("  Filtered out: %d (context < 20k or no text I/O)\n\n",
		len(modelsResp.Data)-addedCount)

	// Save to JSON file
	data, err := json.MarshalIndent(zenmuxProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling ZenMux provider:", err)
	}

	outputPath := "internal/providers/configs/zenmux.json"
	if err := os.WriteFile(outputPath, data, 0o600); err != nil {
		log.Fatal("Error writing ZenMux provider config:", err)
	}

	fmt.Printf("✅ Successfully generated: %s\n", outputPath)
}
