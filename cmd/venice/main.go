// Package main provides a command-line tool to fetch models from Venice
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

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

type ModelsResponse struct {
	Data []VeniceModel `json:"data"`
}

type VeniceModel struct {
	Created   int64           `json:"created"`
	ID        string          `json:"id"`
	ModelSpec VeniceModelSpec `json:"model_spec"`
	Object    string          `json:"object"`
	OwnedBy   string          `json:"owned_by"`
	Type      string          `json:"type"`
}

type VeniceModelSpec struct {
	AvailableContextTokens int64                   `json:"availableContextTokens"`
	Capabilities           VeniceModelCapabilities `json:"capabilities"`
	Constraints            VeniceModelConstraints  `json:"constraints"`
	Name                   string                  `json:"name"`
	ModelSource            string                  `json:"modelSource"`
	Offline                bool                    `json:"offline"`
	Pricing                VeniceModelPricing      `json:"pricing"`
	Traits                 []string                `json:"traits"`
}

type VeniceModelCapabilities struct {
	OptimizedForCode        bool   `json:"optimizedForCode"`
	Quantization            string `json:"quantization"`
	SupportsFunctionCalling bool   `json:"supportsFunctionCalling"`
	SupportsReasoning       bool   `json:"supportsReasoning"`
	SupportsResponseSchema  bool   `json:"supportsResponseSchema"`
	SupportsVision          bool   `json:"supportsVision"`
	SupportsWebSearch       bool   `json:"supportsWebSearch"`
	SupportsLogProbs        bool   `json:"supportsLogProbs"`
}

type VeniceModelConstraints struct {
	Temperature *VeniceDefaultFloat `json:"temperature"`
	TopP        *VeniceDefaultFloat `json:"top_p"`
}

type VeniceDefaultFloat struct {
	Default float64 `json:"default"`
}

type VeniceModelPricing struct {
	Input  VeniceModelPricingValue `json:"input"`
	Output VeniceModelPricingValue `json:"output"`
}

type VeniceModelPricingValue struct {
	USD  float64 `json:"usd"`
	Diem float64 `json:"diem"`
}

func fetchVeniceModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := strings.TrimRight(apiEndpoint, "/") + "/models"
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	if apiKey := strings.TrimSpace(os.Getenv("VENICE_API_KEY")); apiKey != "" && !strings.HasPrefix(apiKey, "$") {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

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

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func bestLargeModelID(models []catwalk.Model) string {
	var best *catwalk.Model
	for i := range models {
		m := &models[i]

		if best == nil {
			best = m
			continue
		}
		if m.ContextWindow > best.ContextWindow {
			best = m
			continue
		}
		if m.ContextWindow == best.ContextWindow && m.CostPer1MOut > best.CostPer1MOut {
			best = m
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

func bestSmallModelID(models []catwalk.Model) string {
	var best *catwalk.Model
	for i := range models {
		m := &models[i]
		if best == nil {
			best = m
			continue
		}
		mCost := m.CostPer1MIn + m.CostPer1MOut
		bestCost := best.CostPer1MIn + best.CostPer1MOut
		if mCost < bestCost {
			best = m
			continue
		}
		if mCost == bestCost && m.ContextWindow < best.ContextWindow {
			best = m
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

func main() {
	veniceProvider := catwalk.Provider{
		Name:        "Venice AI",
		ID:          catwalk.InferenceProviderVenice,
		APIKey:      "$VENICE_API_KEY",
		APIEndpoint: "https://api.venice.ai/api/v1",
		Type:        catwalk.TypeOpenAICompat,
		Models:      []catwalk.Model{},
	}

	modelsResp, err := fetchVeniceModels(veniceProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Venice models:", err)
	}

	for _, model := range modelsResp.Data {
		if strings.ToLower(model.Type) != "text" {
			continue
		}
		if model.ModelSpec.Offline {
			continue
		}
		if !model.ModelSpec.Capabilities.SupportsFunctionCalling {
			continue
		}

		contextWindow := model.ModelSpec.AvailableContextTokens
		if contextWindow <= 0 {
			continue
		}

		defaultMaxTokens := minInt64(contextWindow/4, 32768)
		defaultMaxTokens = maxInt64(defaultMaxTokens, 2048)

		canReason := model.ModelSpec.Capabilities.SupportsReasoning
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		options := catwalk.ModelOptions{}
		if model.ModelSpec.Constraints.Temperature != nil {
			v := model.ModelSpec.Constraints.Temperature.Default
			if !math.IsNaN(v) {
				options.Temperature = &v
			}
		}
		if model.ModelSpec.Constraints.TopP != nil {
			v := model.ModelSpec.Constraints.TopP.Default
			if !math.IsNaN(v) {
				options.TopP = &v
			}
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.ModelSpec.Name,
			CostPer1MIn:            model.ModelSpec.Pricing.Input.USD,
			CostPer1MOut:           model.ModelSpec.Pricing.Output.USD,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			ContextWindow:          contextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         model.ModelSpec.Capabilities.SupportsVision,
			Options:                options,
		}

		veniceProvider.Models = append(veniceProvider.Models, m)
	}

	slices.SortFunc(veniceProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	veniceProvider.DefaultLargeModelID = bestLargeModelID(veniceProvider.Models)
	veniceProvider.DefaultSmallModelID = bestSmallModelID(veniceProvider.Models)

	data, err := json.MarshalIndent(veniceProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Venice provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/venice.json", data, 0o600); err != nil {
		log.Fatal("Error writing Venice provider config:", err)
	}

	fmt.Printf("Generated venice.json with %d models\n", len(veniceProvider.Models))
}
