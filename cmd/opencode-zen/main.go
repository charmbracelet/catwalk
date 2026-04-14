// Package main generates the OpenCode Zen provider configuration.
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

type ZenModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ZenModelsResponse struct {
	Object string     `json:"object"`
	Data   []ZenModel `json:"data"`
}

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

type ModelEnrichment struct {
	Name       string      `json:"name"`
	Attachment bool        `json:"attachment"`
	Reasoning  bool        `json:"reasoning"`
	Cost       PricingData `json:"cost"`
	Limit      ModelLimit  `json:"limit"`
}

func fetchZenModels() ([]ZenModel, error) {
	apiKey := os.Getenv("OPENCODE_ZEN_API_KEY")
	if apiKey == "" {
		apiKey = "public"
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://opencode.ai/zen/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Catwalk/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch zen models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ZenModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("failed to decode zen models: %w", err)
	}

	return mr.Data, nil
}

func fetchEnrichmentData() (map[string]ModelEnrichment, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://models.dev/api.json",
		nil,
	)
	req.Header.Set("User-Agent", "Catwalk/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed fetching enrichment data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var fullData map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&fullData); err != nil {
		return nil, fmt.Errorf("failed to decode when fetching enrichment data: %w", err)
	}

	rawOpenCode, ok := fullData["opencode"]
	if !ok {
		return nil, fmt.Errorf("opencode provider not found in models.dev/api.json")
	}

	var openCodeData struct {
		Models map[string]ModelEnrichment `json:"models"`
	}
	if err := json.Unmarshal(rawOpenCode, &openCodeData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal when fetching enrichment data: %w", err)
	}

	return openCodeData.Models, nil
}

func main() {
	zenModels, err := fetchZenModels()
	if err != nil {
		log.Fatal("Error fetching OpenCode Zen models:", err)
	}

	enrichmentData, err := fetchEnrichmentData()
	if err != nil {
		log.Fatal("Error fetching enrichment data:", err)
	}

	zenProvider := catwalk.Provider{
		Name:                "OpenCode Zen",
		ID:                  catwalk.InferenceProviderOpenCodeZen,
		APIKey:              "$OPENCODE_ZEN_API_KEY",
		APIEndpoint:         "https://opencode.ai/zen/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "minimax-m2.5-free",
		DefaultSmallModelID: "minimax-m2.5-free",
	}

	for _, zenModel := range zenModels {
		enrichment, hasEnrichment := enrichmentData[zenModel.ID]

		var costPer1MIn, costPer1MOut, costPer1MInCached, costPer1MOutCached float64
		var contextWindow, defaultMaxTokens int64 = 200000, 20000
		var supportsImages bool
		var canReason bool
		var reasoningLevels []string
		var defaultReasoningEffort string
		modelName := zenModel.ID

		if hasEnrichment {
			costPer1MIn = math.Round(enrichment.Cost.Input*100) / 100
			costPer1MOut = math.Round(enrichment.Cost.Output*100) / 100
			costPer1MInCached = math.Round(enrichment.Cost.CacheRead*100) / 100
			costPer1MOutCached = math.Round(enrichment.Cost.CacheWrite*100) / 100
			contextWindow = enrichment.Limit.Context
			defaultMaxTokens = enrichment.Limit.Output
			supportsImages = enrichment.Attachment
			modelName = enrichment.Name

			if enrichment.Reasoning {
				reasoningLevels = []string{"low", "medium", "high"}
				defaultReasoningEffort = "medium"
				canReason = true
			}
		} else {
			log.Printf("WARNING: No enrichment found for model %s, using defaults\n", zenModel.ID)
		}

		if costPer1MIn == 0 && costPer1MOut == 0 {
			modelName = strings.TrimSuffix(modelName, "Free ")
			modelName = strings.TrimSuffix(modelName, "Free")
			modelName = strings.TrimRight(modelName, " ")
			modelName += " FREE"
		}

		m := catwalk.Model{
			ID:                     zenModel.ID,
			Name:                   modelName,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			CostPer1MOutCached:     costPer1MOutCached,
			ContextWindow:          contextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			SupportsImages:         supportsImages,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoningEffort,
		}

		zenProvider.Models = append(zenProvider.Models, m)
		fmt.Printf("Added model %s (%s)\n", zenModel.ID, modelName)
	}

	slices.SortFunc(zenProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(zenProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/opencode-zen.json", data, 0o600); err != nil {
		log.Fatal("Error writing provider config:", err)
	}

	fmt.Printf("Generated opencode-zen.json with %d models\n", len(zenProvider.Models))
}
