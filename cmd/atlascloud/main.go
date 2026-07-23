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

	"charm.land/catwalk/pkg/catwalk"
)

// PricingTier represents a single pricing tier. Atlas Cloud returns pricing
// as either a single object or an array of tiered objects; we always pick
// the first (base) tier.
type PricingTier struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	Image          string `json:"image"`
	Request        string `json:"request"`
	InputCacheRead string `json:"input_cache_read"`
}

// Model represents a model from the Atlas Cloud API.
type Model struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	IsReady           *bool           `json:"is_ready,omitempty"`
	InputModalities   []string        `json:"input_modalities"`
	OutputModalities  []string        `json:"output_modalities"`
	ContextLength     int64           `json:"context_length"`
	MaxOutputLength   int64           `json:"max_output_length,omitempty"`
	SupportedFeatures []string        `json:"supported_features,omitempty"`
	DiscountToUser    float64         `json:"discount_to_user,omitempty"`
	Pricing           json.RawMessage `json:"pricing"`
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

// extractBasePricing returns the base pricing tier regardless of whether the
// API returned a single object or a tiered array.
func extractBasePricing(raw json.RawMessage) (PricingTier, error) {
	// Try single object first.
	var single PricingTier
	if err := json.Unmarshal(raw, &single); err == nil && single.Prompt != "" {
		return single, nil
	}

	// Otherwise expect an array of tiers and use the first one.
	var tiers []PricingTier
	if err := json.Unmarshal(raw, &tiers); err != nil {
		return PricingTier{}, fmt.Errorf("decoding pricing: %w", err)
	}
	if len(tiers) == 0 {
		return PricingTier{}, nil
	}
	return tiers[0], nil
}

func fetchAtlasCloudModels(apiEndpoint string) (*ModelsResponse, error) {
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
	atlasCloudProvider := catwalk.Provider{
		Name:                "Atlas Cloud",
		ID:                  catwalk.InferenceProviderAtlasCloud,
		APIKey:              "$ATLASCLOUD_API_KEY",
		APIEndpoint:         "https://api.atlascloud.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
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

		// Skip models with small context windows.
		if model.ContextLength < 20000 {
			continue
		}

		// Skip non-text models.
		if !slices.Contains(model.InputModalities, "text") ||
			!slices.Contains(model.OutputModalities, "text") {
			continue
		}

		// Require tool support.
		if !slices.Contains(model.SupportedFeatures, "tools") {
			continue
		}
		if model.DiscountToUser < 0 || model.DiscountToUser > 1 {
			fmt.Printf("Skipping model %s: invalid user discount %g\n", model.ID, model.DiscountToUser)
			continue
		}

		pricing, err := extractBasePricing(model.Pricing)
		if err != nil {
			fmt.Printf("Skipping model %s: %v\n", model.ID, err)
			continue
		}

		priceMultiplier := 1 - model.DiscountToUser
		costPer1MIn := roundCost(parsePrice(pricing.Prompt) * priceMultiplier * 1_000_000)
		costPer1MOut := roundCost(parsePrice(pricing.Completion) * priceMultiplier * 1_000_000)
		costPer1MCacheRead := roundCost(parsePrice(pricing.InputCacheRead) * priceMultiplier * 1_000_000)

		supportsImages := slices.Contains(model.InputModalities, "image")

		// DefaultMaxTokens: use half of max_output_length when available,
		// capped at 15% of context_length; otherwise 10% of context_length.
		var defaultMaxTokens int64
		if model.MaxOutputLength > 0 {
			maxFromOutput := model.MaxOutputLength / 2
			maxAt15Pct := (model.ContextLength * 15) / 100
			if maxFromOutput <= maxAt15Pct {
				defaultMaxTokens = maxFromOutput
			} else {
				defaultMaxTokens = model.ContextLength / 10
			}
		} else {
			defaultMaxTokens = model.ContextLength / 10
		}

		m := catwalk.Model{
			ID:                 model.ID,
			Name:               model.Name,
			CostPer1MIn:        costPer1MIn,
			CostPer1MOut:       costPer1MOut,
			CostPer1MOutCached: costPer1MCacheRead,
			ContextWindow:      model.ContextLength,
			DefaultMaxTokens:   defaultMaxTokens,
			SupportsImages:     supportsImages,
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
