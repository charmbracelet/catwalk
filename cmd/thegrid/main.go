// Package main provides a command-line tool to fetch instruments from The Grid
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

	"charm.land/catwalk/pkg/catwalk"
)

// Pricing is The Grid's price for an instrument. Instruments are market
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

// Model represents an instrument from The Grid's models API.
type Model struct {
	ID                  string       `json:"id"`
	DisplayName         string       `json:"display_name"`
	Architecture        Architecture `json:"architecture"`
	Attachments         bool         `json:"attachments"`
	ContextLength       int64        `json:"context_length"`
	MaxCompletionTokens int64        `json:"max_completion_tokens"`
	Pricing             Pricing      `json:"pricing"`
	Reasoning           *Reasoning   `json:"reasoning"`
	ToolCall            bool         `json:"tool_call"`
}

// ModelsResponse is the response structure for The Grid's models API.
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

func main() {
	theGridProvider := catwalk.Provider{
		Name:                "The Grid",
		ID:                  catwalk.InferenceProviderTheGrid,
		APIKey:              "$THEGRID_API_KEY",
		APIEndpoint:         "https://api.thegrid.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "agent-max",
		DefaultSmallModelID: "text-standard",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchTheGridModels(theGridProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching The Grid models:", err)
	}

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

		if model.Pricing.USDPer1MTokens <= 0 {
			fmt.Printf("Skipping instrument %s: no price\n", model.ID)
			continue
		}

		// A single market rate covers both directions, and The Grid does not
		// quote a separate cached rate, so all four costs carry it.
		cost := roundCost(model.Pricing.USDPer1MTokens)

		// DefaultMaxTokens: use half of max_completion_tokens when available,
		// capped at 15% of context_length; otherwise 10% of context_length.
		// This matches the heuristic used by the other generators, and avoids
		// instruments whose ceiling equals their whole context window
		// defaulting to an output budget the size of the context.
		var defaultMaxTokens int64
		if model.MaxCompletionTokens > 0 {
			maxFromOutput := model.MaxCompletionTokens / 2
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
			Name:               strings.TrimPrefix(model.DisplayName, "The Grid: "),
			CostPer1MIn:        cost,
			CostPer1MOut:       cost,
			CostPer1MInCached:  cost,
			CostPer1MOutCached: cost,
			ContextWindow:      model.ContextLength,
			DefaultMaxTokens:   defaultMaxTokens,
			SupportsImages:     model.Attachments || slices.Contains(model.Architecture.InputModalities, "image"),
		}

		if model.Reasoning != nil {
			m.CanReason = model.Reasoning.DefaultEnabled || len(model.Reasoning.SupportedEfforts) > 0
			m.ReasoningLevels = model.Reasoning.SupportedEfforts
			m.DefaultReasoningEffort = model.Reasoning.DefaultEffort
		}

		theGridProvider.Models = append(theGridProvider.Models, m)
	}

	slices.SortFunc(theGridProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(theGridProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling The Grid provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/thegrid.json", data, 0o600); err != nil {
		log.Fatal("Error writing The Grid provider config:", err)
	}

	fmt.Printf("Generated thegrid.json with %d instruments\n", len(theGridProvider.Models))
}
