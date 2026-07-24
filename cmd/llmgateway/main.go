// Package main provides a command-line tool to fetch models from LLM Gateway
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

// LLMGatewayModel represents a model from the LLM Gateway models endpoint.
type LLMGatewayModel struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ContextLength int64           `json:"context_length"`
	Pricing       Pricing         `json:"pricing"`
	Architecture  Architecture    `json:"architecture"`
	Providers     []ModelProvider `json:"providers"`
	Stability     string          `json:"stability"`
	DeprecatedAt  *string         `json:"deprecated_at"`
	DeactivatedAt *string         `json:"deactivated_at"`
}

// Architecture describes a model's input and output modalities.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// ModelProvider is one upstream provider mapping for a model.
type ModelProvider struct {
	ProviderID       string   `json:"providerId"`
	Tools            bool     `json:"tools"`
	Reasoning        bool     `json:"reasoning"`
	ReasoningEfforts []string `json:"reasoning_efforts"`
	Vision           bool     `json:"vision"`
	Stability        string   `json:"stability"`
}

// Pricing contains the per-token pricing for a model.
type Pricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

// ModelsResponse is the response structure for the LLM Gateway models API.
type ModelsResponse struct {
	Data []LLMGatewayModel `json:"data"`
}

func fetchLLMGatewayModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.llmgateway.io/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/llmgateway-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func hasModality(modalities []string, modality string) bool {
	return slices.Contains(modalities, modality)
}

// pastDate reports whether the given RFC 3339 timestamp is in the past.
// Deprecation and deactivation dates may be in the future, in which case the
// model is still usable and should be included.
func pastDate(ts *string) bool {
	if ts == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, *ts)
	if err != nil {
		return false
	}
	return t.Before(time.Now())
}

// parsePrice converts a per-token price string to cost per 1M tokens.
func parsePrice(perToken string) float64 {
	var v float64
	if err := json.Unmarshal([]byte(perToken), &v); err != nil {
		return 0
	}
	return math.Round(v*1e6*1e5) / 1e5
}

// effortOrder is the canonical ordering of reasoning efforts.
var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// reasoningLevels merges the reasoning efforts of all stable provider
// mappings into a single canonically ordered list.
func reasoningLevels(providers []ModelProvider) []string {
	seen := map[string]bool{}
	for _, p := range providers {
		if !p.Reasoning || p.Stability == "unstable" {
			continue
		}
		for _, e := range p.ReasoningEfforts {
			seen[e] = true
		}
	}
	var levels []string
	for _, e := range effortOrder {
		if seen[e] {
			levels = append(levels, e)
		}
	}
	return levels
}

func defaultReasoningEffort(levels []string) string {
	if len(levels) == 0 {
		return ""
	}
	if slices.Contains(levels, "medium") {
		return "medium"
	}
	// Fall back to the middle of the supported range, skipping "none".
	usable := slices.DeleteFunc(slices.Clone(levels), func(e string) bool {
		return e == "none"
	})
	if len(usable) == 0 {
		return ""
	}
	return usable[len(usable)/2]
}

func main() {
	modelsResp, err := fetchLLMGatewayModels()
	if err != nil {
		log.Fatal("Error fetching LLM Gateway models:", err)
	}

	llmgatewayProvider := catwalk.Provider{
		Name:                "LLM Gateway",
		ID:                  catwalk.InferenceProviderLLMGateway,
		APIKey:              "$LLMGATEWAY_API_KEY",
		APIEndpoint:         "https://api.llmgateway.io/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-5",
		DefaultSmallModelID: "gpt-5-mini",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		if model.ID == "custom" {
			continue
		}
		if pastDate(model.DeprecatedAt) || pastDate(model.DeactivatedAt) {
			continue
		}
		if model.Stability == "unstable" {
			continue
		}
		if !hasModality(model.Architecture.InputModalities, "text") ||
			!hasModality(model.Architecture.OutputModalities, "text") {
			continue
		}
		if model.ContextLength < 20000 {
			continue
		}

		// Only consider models with at least one stable tool-capable
		// provider mapping.
		supportsTools := slices.ContainsFunc(model.Providers, func(p ModelProvider) bool {
			return p.Tools && p.Stability != "unstable"
		})
		if !supportsTools {
			continue
		}

		canReason := slices.ContainsFunc(model.Providers, func(p ModelProvider) bool {
			return p.Reasoning && p.Stability != "unstable"
		})
		levels := reasoningLevels(model.Providers)

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            parsePrice(model.Pricing.Prompt),
			CostPer1MOut:           parsePrice(model.Pricing.Completion),
			CostPer1MInCached:      parsePrice(model.Pricing.InputCacheRead),
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.ContextLength / 10,
			CanReason:              canReason,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: defaultReasoningEffort(levels),
			SupportsImages:         hasModality(model.Architecture.InputModalities, "image"),
		}

		llmgatewayProvider.Models = append(llmgatewayProvider.Models, m)
	}

	slices.SortFunc(llmgatewayProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(llmgatewayProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling LLM Gateway provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/llmgateway.json", data, 0o600); err != nil {
		log.Fatal("Error writing LLM Gateway provider config:", err)
	}

	fmt.Printf("Generated llmgateway.json with %d models\n", len(llmgatewayProvider.Models))
}
