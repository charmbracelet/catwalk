// Package main provides a command-line tool to fetch models from Atlas Cloud
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

	"charm.land/catwalk/v3/pkg/catwalk"
)

// PriceEntry is a single price line attached to a modality. Atlas Cloud
// quotes prices in USD per token (e.g. "0.00000026").
type PriceEntry struct {
	Type    string `json:"type"`
	Unit    string `json:"unit"`
	CostUSD string `json:"cost_usd"`
}

// TokenLimit is a token limit together with its unit.
type TokenLimit struct {
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
}

// InputModality describes one supported input type with its context limit
// and pricing.
type InputModality struct {
	Type            string `json:"type"`
	SupportedInputs struct {
		MaxContextLength *TokenLimit `json:"max_context_length,omitempty"`
	} `json:"supported_inputs"`
	Pricing []PriceEntry `json:"pricing,omitempty"`
}

// OutputModality describes one supported output type with its max length,
// supported parameters, and pricing.
type OutputModality struct {
	Type                string      `json:"type"`
	MaxLength           *TokenLimit `json:"max_length,omitempty"`
	SupportedParameters map[string]struct {
		Type string `json:"type"`
	} `json:"supported_parameters,omitempty"`
	Pricing []PriceEntry `json:"pricing,omitempty"`
}

// Model represents a model from the Atlas Cloud API.
type Model struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	IsReady          *bool            `json:"is_ready,omitempty"`
	InputModalities  []InputModality  `json:"input_modalities"`
	OutputModalities []OutputModality `json:"output_modalities"`
	DiscountToUser   float64          `json:"discount_to_user,omitempty"`
}

// ModelsResponse is the response structure for the Atlas Cloud models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

// parsePrice extracts a float from Atlas Cloud's price format (e.g. "0.00000055").
func parsePrice(s string) float64 {
	s = strings.TrimPrefix(s, "$")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return v
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// findPrice returns the cost_usd of the price entry with the given type.
func findPrice(entries []PriceEntry, typ string) string {
	for _, p := range entries {
		if p.Type == typ {
			return p.CostUSD
		}
	}
	return ""
}

// textModalities returns the text input and output modalities of a model,
// or nil pointers when the model does not handle text.
func textModalities(m Model) (*InputModality, *OutputModality) {
	var textIn *InputModality
	var textOut *OutputModality
	for i := range m.InputModalities {
		if m.InputModalities[i].Type == "text" {
			textIn = &m.InputModalities[i]
			break
		}
	}
	for i := range m.OutputModalities {
		if m.OutputModalities[i].Type == "text" {
			textOut = &m.OutputModalities[i]
			break
		}
	}
	return textIn, textOut
}

func fetchAtlasCloudModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating models request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk-Client/1.0")
	if apiKey := strings.TrimSpace(os.Getenv("ATLASCLOUD_API_KEY")); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

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
	atlasCloudProvider := catwalk.Provider{
		Name:                "Atlas Cloud",
		ID:                  catwalk.InferenceProviderAtlasCloud,
		APIKey:              "$ATLASCLOUD_API_KEY",
		APIEndpoint:         "https://api.atlascloud.ai/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: "zai-org/glm-5.2",
		DefaultSmallModelID: "deepseek-ai/deepseek-v4-flash",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchAtlasCloudModels(atlasCloudProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Atlas Cloud models:", err)
	}

	for _, model := range modelsResp.Data {
		// Atlas Cloud omits is_ready for available models but explicitly marks
		// models that are not yet available as false.
		if model.IsReady != nil && !*model.IsReady {
			continue
		}

		textIn, textOut := textModalities(model)
		if textIn == nil || textOut == nil {
			continue
		}

		// Tools support is reported per output modality as a supported
		// parameter of type "boolean".
		if textOut.SupportedParameters["tools"].Type != "boolean" {
			continue
		}

		var contextLength int64
		if textIn.SupportedInputs.MaxContextLength != nil {
			contextLength = textIn.SupportedInputs.MaxContextLength.Value
		}
		// Skip models with small context windows.
		if contextLength < 20000 {
			continue
		}

		if model.DiscountToUser < 0 || model.DiscountToUser > 1 {
			fmt.Printf("Skipping model %s: invalid user discount %g\n", model.ID, model.DiscountToUser)
			continue
		}

		promptPrice := findPrice(textIn.Pricing, "prompt")
		completionPrice := findPrice(textOut.Pricing, "completion")
		if promptPrice == "" || completionPrice == "" {
			fmt.Printf("Skipping model %s: missing prompt or completion pricing\n", model.ID)
			continue
		}

		priceMultiplier := 1 - model.DiscountToUser
		costPerTokenIn := roundCost(parsePrice(promptPrice) * priceMultiplier * 1_000_000)
		costPerTokenOut := roundCost(parsePrice(completionPrice) * priceMultiplier * 1_000_000)
		costPerTokenCacheRead := roundCost(parsePrice(findPrice(textIn.Pricing, "cached_prompt")) * priceMultiplier * 1_000_000)
		costPerTokenCacheWrite := roundCost(parsePrice(findPrice(textIn.Pricing, "cache_write")) * priceMultiplier * 1_000_000)

		supportsImages := slices.ContainsFunc(model.InputModalities, func(im InputModality) bool {
			return im.Type == "image"
		})

		// DefaultMaxTokens: use half of max_length when available,
		// capped at 15% of context_length; otherwise 10% of context_length.
		var maxOutputLength int64
		if textOut.MaxLength != nil {
			maxOutputLength = textOut.MaxLength.Value
		}
		var defaultMaxTokens int64
		if maxOutputLength > 0 {
			maxFromOutput := maxOutputLength / 2
			maxAt15Pct := (contextLength * 15) / 100
			if maxFromOutput <= maxAt15Pct {
				defaultMaxTokens = maxFromOutput
			} else {
				defaultMaxTokens = contextLength / 10
			}
		} else {
			defaultMaxTokens = contextLength / 10
		}

		m := catwalk.Model{
			ID:   model.ID,
			Name: model.Name,
			Pricing: catwalk.Pricing{
				Input:       costPerTokenIn,
				Output:      costPerTokenOut,
				CacheCreate: costPerTokenCacheWrite,
				CacheHit:    costPerTokenCacheRead,
			},
			ContextWindow:    contextLength,
			DefaultMaxTokens: defaultMaxTokens,
			Reasoning:        catwalk.Reasoning{Thinking: catwalk.ThinkingNever},
			Capabilities:     catwalk.Capabilities{Vision: supportsImages},
		}

		atlasCloudProvider.Models = append(atlasCloudProvider.Models, m)
	}

	slices.SortFunc(atlasCloudProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(atlasCloudProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Atlas Cloud provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/atlascloud.json", data, 0o600); err != nil {
		log.Fatal("Error writing Atlas Cloud provider config:", err)
	}

	fmt.Printf("Generated atlascloud.json with %d models\n", len(atlasCloudProvider.Models))
}
