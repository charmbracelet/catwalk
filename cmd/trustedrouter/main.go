// Package main provides a command-line tool to fetch models from TrustedRouter
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

// Pricing is the per-token pricing of a model, as decimal strings in USD.
type Pricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// Architecture describes the modalities of a model.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// TopProvider describes the limits of the serving route.
type TopProvider struct {
	ContextLength       int64  `json:"context_length"`
	MaxCompletionTokens *int64 `json:"max_completion_tokens"`
}

// Extra holds the TrustedRouter-specific metadata we use for filtering.
type Extra struct {
	InternalOnly        bool `json:"internal_only"`
	ConfigurationHidden bool `json:"configuration_hidden"`
	SyntheticMonitor    bool `json:"synthetic_monitor"`
}

// Model represents a model from the TrustedRouter models API.
type Model struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	ContextLength   int64        `json:"context_length"`
	Architecture    Architecture `json:"architecture"`
	Pricing         Pricing      `json:"pricing"`
	TopProvider     TopProvider  `json:"top_provider"`
	SupportedParams []string     `json:"supported_parameters"`
	TrustedRouter   Extra        `json:"trustedrouter"`
}

// ModelsResponse is the response structure for the TrustedRouter models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func parsePrice(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return v
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func fetchTrustedRouterModels(apiEndpoint string) (*ModelsResponse, error) {
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

// This is used to generate the trustedrouter.json config file.
func main() {
	trustedRouterProvider := catwalk.Provider{
		Name:                "TrustedRouter",
		ID:                  catwalk.InferenceProviderTrustedRouter,
		APIKey:              "$TRUSTEDROUTER_API_KEY",
		APIEndpoint:         "https://api.trustedrouter.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "anthropic/claude-sonnet-4.6",
		DefaultSmallModelID: "z-ai/glm-5.3-flash",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchTrustedRouterModels(trustedRouterProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching TrustedRouter models:", err)
	}

	for _, model := range modelsResp.Data {
		if model.TrustedRouter.InternalOnly ||
			model.TrustedRouter.ConfigurationHidden ||
			model.TrustedRouter.SyntheticMonitor {
			continue
		}
		if model.ContextLength < 20000 {
			continue
		}
		// Skip non-text models or those without tools.
		if !slices.Contains(model.SupportedParams, "tools") ||
			!slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		canReason := slices.Contains(model.SupportedParams, "reasoning")
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		contextWindow := model.ContextLength
		if model.TopProvider.ContextLength > 0 {
			contextWindow = model.TopProvider.ContextLength
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            roundCost(parsePrice(model.Pricing.Prompt) * 1_000_000),
			CostPer1MOut:           roundCost(parsePrice(model.Pricing.Completion) * 1_000_000),
			CostPer1MInCached:      roundCost(parsePrice(model.Pricing.InputCacheWrite) * 1_000_000),
			CostPer1MOutCached:     roundCost(parsePrice(model.Pricing.InputCacheRead) * 1_000_000),
			ContextWindow:          contextWindow,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         slices.Contains(model.Architecture.InputModalities, "image"),
		}
		if model.TopProvider.MaxCompletionTokens != nil {
			m.DefaultMaxTokens = *model.TopProvider.MaxCompletionTokens / 2
		} else {
			m.DefaultMaxTokens = contextWindow / 10
		}

		trustedRouterProvider.Models = append(trustedRouterProvider.Models, m)
	}

	slices.SortFunc(trustedRouterProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	for _, id := range []string{trustedRouterProvider.DefaultLargeModelID, trustedRouterProvider.DefaultSmallModelID} {
		if !slices.ContainsFunc(trustedRouterProvider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			log.Fatalf("Default model %s not found in TrustedRouter catalog", id)
		}
	}

	data, err := json.MarshalIndent(trustedRouterProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling TrustedRouter provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/trustedrouter.json", data, 0o600); err != nil {
		log.Fatal("Error writing TrustedRouter provider config:", err)
	}

	fmt.Printf("Generated trustedrouter.json with %d models\n", len(trustedRouterProvider.Models))
}
