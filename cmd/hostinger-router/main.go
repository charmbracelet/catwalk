// Package main provides a command-line tool to fetch models from Hostinger
// Router and generate a configuration file for the provider.
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

// usdPerCredit is the value of one Hostinger Router credit in US dollars.
// The models API returns prices in credits per token.
const usdPerCredit = 0.01

// Pricing contains the per-token prices of a model, in credits.
type Pricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// Architecture describes the input and output modalities of a model.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// Model represents a model from the Hostinger Router API.
type Model struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	ContextLength       int64        `json:"context_length"`
	MaxOutputTokens     int64        `json:"max_output_tokens"`
	Pricing             Pricing      `json:"pricing"`
	SupportedParameters []string     `json:"supported_parameters"`
	SupportedEndpoints  []string     `json:"supported_endpoints"`
	Architecture        Architecture `json:"architecture"`
}

// ModelsResponse is the response structure for the Hostinger Router models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// costPer1M converts a per-token price in credits to US dollars per 1M tokens.
func costPer1M(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return roundCost(v * usdPerCredit * 1_000_000)
}

// ignoresReasoningEffort reports whether reasoning_effort must not be sent to
// the model on chat completions, even though the catalog lists it. Claude
// models then reply with Anthropic finish reasons (e.g. "tool_use" instead of
// "tool_calls"), and the Phi reasoning models reject the parameter.
func ignoresReasoningEffort(id string) bool {
	return strings.HasPrefix(id, "claude-") || strings.HasPrefix(id, "phi-4")
}

func fetchHostingerRouterModels(apiEndpoint string) (*ModelsResponse, error) {
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
	hostingerRouterProvider := catwalk.Provider{
		Name:                "Hostinger Router",
		ID:                  catwalk.InferenceProviderHostingerRouter,
		APIKey:              "$HOSTINGER_ROUTER_API_KEY",
		APIEndpoint:         "https://router.hostinger.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-5",
		DefaultSmallModelID: "gpt-5-mini",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchHostingerRouterModels(hostingerRouterProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Hostinger Router models:", err)
	}

	for _, model := range modelsResp.Data {
		// The catalog also lists embedding, image, audio and Responses-only
		// models. Keep the ones served on the chat completions endpoint.
		if !slices.Contains(model.SupportedEndpoints, "/v1/chat/completions") {
			continue
		}

		// Skip models with small context windows.
		if model.ContextLength < 20000 {
			continue
		}

		// Skip non-text models.
		if !slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		// Require tool support.
		if !slices.Contains(model.SupportedParameters, "tools") {
			continue
		}

		// Only expose reasoning controls where the model accepts the
		// reasoning_effort parameter on chat completions.
		canReason := slices.Contains(model.SupportedParameters, "reasoning_effort") &&
			!ignoresReasoningEffort(model.ID)
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// DefaultMaxTokens: use half of max_output_tokens when available,
		// capped at 15% of context_length; otherwise 10% of context_length.
		var defaultMaxTokens int64
		if model.MaxOutputTokens > 0 {
			maxFromOutput := model.MaxOutputTokens / 2
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
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            costPer1M(model.Pricing.Prompt),
			CostPer1MOut:           costPer1M(model.Pricing.Completion),
			CostPer1MInCached:      costPer1M(model.Pricing.InputCacheWrite),
			CostPer1MOutCached:     costPer1M(model.Pricing.InputCacheRead),
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         slices.Contains(model.Architecture.InputModalities, "image"),
		}

		hostingerRouterProvider.Models = append(hostingerRouterProvider.Models, m)
	}

	if len(hostingerRouterProvider.Models) == 0 {
		log.Fatal("No Hostinger Router models matched the filters")
	}

	slices.SortFunc(hostingerRouterProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(hostingerRouterProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Hostinger Router provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/hostinger-router.json", data, 0o600); err != nil {
		log.Fatal("Error writing Hostinger Router provider config:", err)
	}

	fmt.Printf("Generated hostinger-router.json with %d models\n", len(hostingerRouterProvider.Models))
}
