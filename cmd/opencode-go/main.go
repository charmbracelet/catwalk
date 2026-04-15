// Package main generates the OpenCode Go provider configuration.
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
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type ModelLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

type GoModel struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Attachment bool        `json:"attachment"`
	Reasoning  bool        `json:"reasoning"`
	Cost       PricingData `json:"cost"`
	Limit      ModelLimit  `json:"limit"`
}

type GoProviderData struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	API    string             `json:"api"`
	Env    []string           `json:"env"`
	Models map[string]GoModel `json:"models"`
}

func fetchGoModels() (map[string]GoModel, error) {
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

	rawGoData, ok := fullData["opencode-go"]
	if !ok {
		return nil, fmt.Errorf("opencode-go provider not found in models.dev/api.json")
	}

	var goData GoProviderData
	if err := json.Unmarshal(rawGoData, &goData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal opencode-go data: %w", err)
	}

	return goData.Models, nil
}

func main() {
	goModels, err := fetchGoModels()
	if err != nil {
		log.Fatal("Error fetching OpenCode Go models:", err)
	}

	goProvider := catwalk.Provider{
		Name:                "OpenCode Go",
		ID:                  catwalk.InferenceProviderOpenCodeGo,
		APIKey:              "$OPENCODE_API_KEY",
		APIEndpoint:         "https://opencode.ai/zen/go/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "minimax-m2.7",
		DefaultSmallModelID: "minimax-m2.7",
	}

	for _, goModel := range goModels {
		costPer1MIn := math.Round(goModel.Cost.Input*100) / 100
		costPer1MOut := math.Round(goModel.Cost.Output*100) / 100
		costPer1MInCached := math.Round(goModel.Cost.CacheRead*100) / 100

		var reasoningLevels []string
		var defaultReasoningEffort string
		if goModel.Reasoning {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoningEffort = "medium"
		}

		m := catwalk.Model{
			ID:                     goModel.ID,
			Name:                   goModel.Name,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			ContextWindow:          goModel.Limit.Context,
			DefaultMaxTokens:       goModel.Limit.Output,
			SupportsImages:         goModel.Attachment,
			CanReason:              goModel.Reasoning,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoningEffort,
		}

		goProvider.Models = append(goProvider.Models, m)
		fmt.Printf("Added model %s (%s)\n", goModel.ID, goModel.Name)
	}

	slices.SortFunc(goProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(goProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/opencode-go.json", data, 0o600); err != nil {
		log.Fatal("Error writing provider config:", err)
	}

	fmt.Printf("Generated opencode-go.json with %d models\n", len(goProvider.Models))
}
