// Package main provides a command-line tool to fetch models from Wallaby
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

// PricingFile mirrors the public pricing sheet published at
// https://wallabytoken.com/pricing.json.
type PricingFile struct {
	Models []PricingModel `json:"models"`
}

// PricingModel is a single model entry in the pricing sheet. Prices are in
// USD per 1M tokens.
type PricingModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Ours  struct {
		Input         float64 `json:"input"`
		Output        float64 `json:"output"`
		CacheHitInput float64 `json:"cacheHitInput"`
	} `json:"ours"`
}

// modelCapabilities carries metadata that the pricing sheet does not publish
// (context window, output limit, reasoning and attachment support). Values
// are taken from the upstream Moonshot model metadata, matching this repo's
// own moonshot.json entries for the same underlying models.
type modelCapabilities struct {
	contextWindow       int64
	defaultMaxTokens    int64
	canReason           bool
	supportsAttachments bool
}

var knownCapabilities = map[string]modelCapabilities{
	"kimi-k3": {
		contextWindow:       1048576,
		defaultMaxTokens:    131072,
		canReason:           true,
		supportsAttachments: true,
	},
}

const (
	pricingURL        = "https://wallabytoken.com/pricing.json"
	defaultLargeModel = "kimi-k3"
	defaultSmallModel = "kimi-k3"
)

func fetchPricing() (*PricingFile, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		pricingURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching pricing: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var pf PricingFile
	if err := json.NewDecoder(resp.Body).Decode(&pf); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &pf, nil
}

func main() {
	pricing, err := fetchPricing()
	if err != nil {
		log.Fatal("Error fetching Wallaby pricing:", err)
	}

	wallabyProvider := catwalk.Provider{
		Name:                "Wallaby",
		ID:                  catwalk.InferenceProviderWallaby,
		APIKey:              "$WALLABY_API_KEY",
		APIEndpoint:         "https://api.wallabytoken.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: defaultLargeModel,
		DefaultSmallModelID: defaultSmallModel,
	}

	for _, model := range pricing.Models {
		caps, ok := knownCapabilities[model.ID]
		if !ok {
			log.Printf("Skipping %s: no verified capability metadata", model.ID)
			continue
		}

		wallabyProvider.Models = append(wallabyProvider.Models, catwalk.Model{
			ID:                 model.ID,
			Name:               model.Label,
			CostPer1MIn:        model.Ours.Input,
			CostPer1MOut:       model.Ours.Output,
			CostPer1MInCached:  0,
			CostPer1MOutCached: model.Ours.CacheHitInput,
			ContextWindow:      caps.contextWindow,
			DefaultMaxTokens:   caps.defaultMaxTokens,
			CanReason:          caps.canReason,
			SupportsImages:     caps.supportsAttachments,
		})
	}

	if len(wallabyProvider.Models) == 0 {
		log.Fatal("No models found or no models met the criteria")
	}

	slices.SortFunc(wallabyProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(wallabyProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Wallaby provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/wallaby.json", data, 0o600); err != nil {
		log.Fatal("Error writing Wallaby provider config:", err)
	}

	fmt.Printf("\nSuccessfully wrote %d models to internal/providers/configs/wallaby.json\n", len(wallabyProvider.Models))
}
