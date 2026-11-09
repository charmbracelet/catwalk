// Package main provides a command-line tool to fetch instruments from The Grid AI
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

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Pricing is The Grid AI's price for an instrument. Instruments are market
// priced and quoted at a single rate that applies to both input and output
// tokens, so prompt and completion always carry the same value.
type Pricing struct {
	USDPer1MTokens float64 `json:"usd_per_1m_tokens"`
}

// Architecture describes the modalities an instrument accepts and returns.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// Reasoning describes an instrument's reasoning support.
type Reasoning struct {
	DefaultEffort    string   `json:"default_effort"`
	DefaultEnabled   bool     `json:"default_enabled"`
	SupportedEfforts []string `json:"supported_efforts"`
}

// Model represents an instrument from The Grid AI's models API.
type Model struct {
	ID                  string       `json:"id"`
	DisplayName         string       `json:"display_name"`
	Architecture        Architecture `json:"architecture"`
	ContextLength       int64        `json:"context_length"`
	MaxCompletionTokens int64        `json:"max_completion_tokens"`
	Pricing             *Pricing     `json:"pricing"`
	Reasoning           *Reasoning   `json:"reasoning"`
	ToolCall            bool         `json:"tool_call"`
}

// ModelsResponse is the response structure for The Grid AI's models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func fetchTheGridModels(apiEndpoint string) (*ModelsResponse, error) {
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

// getPricing converts The Grid AI's single market rate into catwalk pricing.
// The Grid AI does not discount cached input, so a cache hit is billed at the
// same rate. It does not price cache writes, so cache_create stays empty.
func getPricing(model Model) (catwalk.Pricing, bool) {
	// The catalog serves `pricing: null` when the rate cache is cold, which is
	// a normal transient state rather than a reason to drop the instrument.
	if model.Pricing == nil || model.Pricing.USDPer1MTokens <= 0 {
		return catwalk.Pricing{}, false
	}
	rate := roundCost(model.Pricing.USDPer1MTokens)
	return catwalk.Pricing{Input: rate, Output: rate, CacheHit: rate}, true
}

func getReasoning(model Model) catwalk.Reasoning {
	r := model.Reasoning
	if r == nil || (!r.DefaultEnabled && len(r.SupportedEfforts) == 0) {
		return catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
	}

	// Instruments that reason are toggleable with effort levels, like every
	// other generated provider. The Grid AI marks some instruments as
	// mandatory reasoners, but they still accept an effort level per request.
	reasoning := catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable}
	if len(r.SupportedEfforts) == 0 {
		return reasoning
	}

	reasoning.EffortLevels = catwalk.NewEffortLevels(r.SupportedEfforts...)
	switch {
	case r.DefaultEffort != "" && slices.Contains(r.SupportedEfforts, r.DefaultEffort):
		reasoning.DefaultEffortLevel = r.DefaultEffort
	case slices.Contains(r.SupportedEfforts, "medium"):
		reasoning.DefaultEffortLevel = "medium"
	case slices.Contains(r.SupportedEfforts, "high"):
		reasoning.DefaultEffortLevel = "high"
	default:
		reasoning.DefaultEffortLevel = r.SupportedEfforts[0]
	}
	return reasoning
}

// getDefaultMaxTokens uses half of max_completion_tokens when available,
// capped at 15% of context_length; otherwise 10% of context_length. This
// matches the heuristic used by the other generators, and avoids instruments
// whose ceiling equals their whole context window defaulting to an output
// budget the size of the context.
func getDefaultMaxTokens(model Model) int64 {
	if model.MaxCompletionTokens > 0 {
		maxFromOutput := model.MaxCompletionTokens / 2
		maxAt15Pct := (model.ContextLength * 15) / 100
		if maxFromOutput <= maxAt15Pct {
			return maxFromOutput
		}
	}
	return model.ContextLength / 10
}

// This is used to generate the thegrid.json config file.
func main() {
	theGridProvider := catwalk.Provider{
		Name:                "The Grid AI",
		ID:                  catwalk.InferenceProviderTheGrid,
		APIKey:              "$THEGRID_API_KEY",
		APIEndpoint:         "https://api.thegrid.ai/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: "agent-max",
		DefaultSmallModelID: "text-standard",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchTheGridModels(theGridProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching The Grid AI models:", err)
	}

	var unpriced int

	for _, model := range modelsResp.Data {
		// Skip non-text instruments.
		if !slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		// Require tool support, as Crush drives tool calls.
		if !model.ToolCall {
			continue
		}

		pricing, priced := getPricing(model)
		if !priced {
			unpriced++
		}

		m := catwalk.Model{
			ID:               model.ID,
			Name:             strings.TrimPrefix(model.DisplayName, "The Grid: "),
			Pricing:          pricing,
			ContextWindow:    model.ContextLength,
			DefaultMaxTokens: getDefaultMaxTokens(model),
			Reasoning:        getReasoning(model),
			Capabilities: catwalk.Capabilities{
				Vision: slices.Contains(model.Architecture.InputModalities, "image"),
			},
		}

		theGridProvider.Models = append(theGridProvider.Models, m)
	}

	slices.SortFunc(theGridProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/thegrid.json
	data, err := json.MarshalIndent(theGridProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling The Grid AI provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/thegrid.json", data, 0o600); err != nil {
		log.Fatal("Error writing The Grid AI provider config:", err)
	}

	fmt.Printf("Generated thegrid.json with %d instruments\n", len(theGridProvider.Models))
	if unpriced > 0 {
		fmt.Printf("%d of them carried no published price and were written with a zero cost\n", unpriced)
	}
}
