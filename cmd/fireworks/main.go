// Package main generates the Fireworks provider configuration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

type PricingData struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	CacheRead float64 `json:"cache_read,omitempty"`
}

type ModelLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

type FireworksModel struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Attachment bool        `json:"attachment"`
	Reasoning  bool        `json:"reasoning"`
	Cost       PricingData `json:"cost"`
	Limit      ModelLimit  `json:"limit"`
}

type FireworksProviderData struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	API    string                    `json:"api"`
	Env    []string                  `json:"env"`
	Models map[string]FireworksModel `json:"models"`
}

func fetchFireworksModels() (map[string]FireworksModel, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://models.dev/api.json", nil)
	req.Header.Set("User-Agent", "Catwalk/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var fullData map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&fullData); err != nil {
		return nil, fmt.Errorf("failed to decode api.json: %w", err)
	}

	rawFireworksData, ok := fullData["fireworks-ai"]
	if !ok {
		return nil, fmt.Errorf("fireworks-ai provider not found in models.dev/api.json")
	}

	var fireworksData FireworksProviderData
	if err := json.Unmarshal(rawFireworksData, &fireworksData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fireworks-ai data: %w", err)
	}

	return fireworksData.Models, nil
}

func main() {
	fireworksModels, err := fetchFireworksModels()
	if err != nil {
		log.Fatal("Error fetching Fireworks models:", err)
	}

	provider := catwalk.Provider{
		Name:                "Fireworks",
		ID:                  catwalk.InferenceProviderFireworks,
		APIKey:              "$FIREWORKS_API_KEY",
		APIEndpoint:         "https://api.fireworks.ai/inference/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/models/kimi-k2p6",
		DefaultSmallModelID: "accounts/fireworks/models/deepseek-v4-flash",
	}

	for _, fwModel := range fireworksModels {
		costPer1MIn := math.Round(fwModel.Cost.Input*100) / 100
		costPer1MOut := math.Round(fwModel.Cost.Output*100) / 100
		costPer1MInCached := math.Round(fwModel.Cost.CacheRead*100) / 100

		var reasoningLevels []string
		var defaultReasoningEffort string
		if fwModel.Reasoning {
			switch {
			case strings.Contains(fwModel.ID, "deepseek-v4"):
				reasoningLevels = []string{"high", "xhigh"}
				defaultReasoningEffort = "high"
			default:
				reasoningLevels = []string{"low", "medium", "high"}
				defaultReasoningEffort = "medium"
			}
		}

		m := catwalk.Model{
			ID:                     fwModel.ID,
			Name:                   fwModel.Name,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			ContextWindow:          fwModel.Limit.Context,
			DefaultMaxTokens:       fwModel.Limit.Output,
			SupportsImages:         fwModel.Attachment,
			CanReason:              fwModel.Reasoning,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoningEffort,
		}

		provider.Models = append(provider.Models, m)
		fmt.Printf("Added model %s (%s)\n", fwModel.ID, fwModel.Name)
	}

	slices.SortFunc(provider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/fireworks.json", data, 0o600); err != nil {
		log.Fatal("Error writing provider config:", err)
	}

	fmt.Printf("Generated fireworks.json with %d models\n", len(provider.Models))
}
