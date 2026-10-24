// Package main provides a command-line tool to fetch models from EmpirioLabs AI
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

// maxDefaultTokens caps the max_tokens Crush sends by default. Several of
// these models count reasoning toward max_tokens, so this leaves room for
// both the reasoning and the answer.
const maxDefaultTokens = 65536

// codingModels are the EmpirioLabs AI coding models with tool calling to
// list. Prices, limits and capabilities come from the public models endpoint.
var codingModels = []string{
	"deepseek-v4-1-flash",
	"deepseek-v4-pro",
	"glm-5-3",
	"kimi-k2-7-code",
	"minimax-m3",
	"qwen3-7-max",
	"qwen3-8-flash",
}

// PricingTier represents a single pricing tier, in USD per token. Pricing is
// returned as either a single object or an array of tiered objects; we always
// pick the first (base) tier.
type PricingTier struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

// Parameter represents a request parameter supported by a model.
type Parameter struct {
	Name    string `json:"name"`
	Default any    `json:"default"`
	Options []any  `json:"options"`
}

// Capabilities represents the capability flags of a model.
type Capabilities struct {
	Images    bool `json:"images"`
	Reasoning bool `json:"reasoning"`
}

// Model represents a model from the EmpirioLabs AI API.
type Model struct {
	ID                  string          `json:"id"`
	DisplayName         string          `json:"display_name"`
	Status              string          `json:"status"`
	ContextWindow       int64           `json:"context_window"`
	MaxOutputTokens     int64           `json:"max_output_tokens"`
	Features            []string        `json:"features"`
	Capabilities        Capabilities    `json:"capabilities"`
	InputModalities     []string        `json:"input_modalities"`
	OutputModalities    []string        `json:"output_modalities"`
	SupportedParameters []Parameter     `json:"supported_parameters"`
	Pricing             json.RawMessage `json:"pricing"`
}

// ModelsResponse is the response structure for the EmpirioLabs AI models API.
// Models are decoded one by one so an unrelated catalog entry can't break the
// generator.
type ModelsResponse struct {
	Data []json.RawMessage `json:"data"`
}

func parsePrice(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// extractBasePricing returns the base pricing tier regardless of whether the
// API returned a single object or a tiered array.
func extractBasePricing(raw json.RawMessage) (PricingTier, error) {
	var single PricingTier
	if err := json.Unmarshal(raw, &single); err == nil && single.Prompt != "" {
		return single, nil
	}

	var tiers []PricingTier
	if err := json.Unmarshal(raw, &tiers); err != nil {
		return PricingTier{}, fmt.Errorf("decoding pricing: %w", err)
	}
	if len(tiers) == 0 {
		return PricingTier{}, nil
	}
	return tiers[0], nil
}

// reasoningEffort returns the reasoning effort levels and the default level
// the model accepts, if any.
func reasoningEffort(model Model) ([]string, string) {
	for _, param := range model.SupportedParameters {
		if param.Name != "reasoning_effort" {
			continue
		}
		var levels []string
		for _, option := range param.Options {
			if level, ok := option.(string); ok && level != "" {
				levels = append(levels, level)
			}
		}
		if len(levels) == 0 {
			continue
		}
		defaultLevel, _ := param.Default.(string)
		if !slices.Contains(levels, defaultLevel) {
			defaultLevel = ""
		}
		return levels, defaultLevel
	}
	return nil, ""
}

func fetchEmpirioLabsModels(apiEndpoint string) (*ModelsResponse, error) {
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
	empirioLabsProvider := catwalk.Provider{
		Name:                "EmpirioLabs AI",
		ID:                  catwalk.InferenceProviderEmpirioLabs,
		APIKey:              "$EMPIRIOLABS_API_KEY",
		APIEndpoint:         "https://api.empiriolabs.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "qwen3-7-max",
		DefaultSmallModelID: "qwen3-8-flash",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchEmpirioLabsModels(empirioLabsProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching EmpirioLabs AI models:", err)
	}

	for _, raw := range modelsResp.Data {
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil || !slices.Contains(codingModels, head.ID) {
			continue
		}
		var model Model
		if err := json.Unmarshal(raw, &model); err != nil {
			fmt.Printf("Skipping model %s: %v\n", head.ID, err)
			continue
		}
		if model.Status != "available" {
			fmt.Printf("Skipping model %s: status %q\n", model.ID, model.Status)
			continue
		}
		if !slices.Contains(model.InputModalities, "text") ||
			!slices.Contains(model.OutputModalities, "text") ||
			!slices.Contains(model.Features, "function_calling") {
			fmt.Printf("Skipping model %s: no text chat with tool calling\n", model.ID)
			continue
		}

		pricing, err := extractBasePricing(model.Pricing)
		if err != nil {
			fmt.Printf("Skipping model %s: %v\n", model.ID, err)
			continue
		}
		costPer1MIn := roundCost(parsePrice(pricing.Prompt) * 1_000_000)
		costPer1MOut := roundCost(parsePrice(pricing.Completion) * 1_000_000)
		costPer1MCacheRead := roundCost(parsePrice(pricing.InputCacheRead) * 1_000_000)
		if costPer1MIn <= 0 || costPer1MOut <= 0 {
			fmt.Printf("Skipping model %s: missing token pricing\n", model.ID)
			continue
		}

		defaultMaxTokens := model.MaxOutputTokens
		if defaultMaxTokens <= 0 {
			defaultMaxTokens = model.ContextWindow / 10
		}
		defaultMaxTokens = min(defaultMaxTokens, maxDefaultTokens)

		canReason := model.Capabilities.Reasoning || slices.Contains(model.Features, "reasoning")
		var reasoningLevels []string
		var defaultReasoningEffort string
		if canReason {
			reasoningLevels, defaultReasoningEffort = reasoningEffort(model)
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.DisplayName,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MOutCached:     costPer1MCacheRead,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoningEffort,
			SupportsImages:         slices.Contains(model.InputModalities, "image"),
		}

		empirioLabsProvider.Models = append(empirioLabsProvider.Models, m)
	}

	for _, id := range []string{empirioLabsProvider.DefaultLargeModelID, empirioLabsProvider.DefaultSmallModelID} {
		if !slices.ContainsFunc(empirioLabsProvider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			log.Fatalf("Default model %s is missing from the EmpirioLabs AI models", id)
		}
	}

	slices.SortFunc(empirioLabsProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(empirioLabsProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling EmpirioLabs AI provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/empiriolabs.json", data, 0o600); err != nil {
		log.Fatal("Error writing EmpirioLabs AI provider config:", err)
	}

	fmt.Printf("Generated empiriolabs.json with %d models\n", len(empirioLabsProvider.Models))
}
