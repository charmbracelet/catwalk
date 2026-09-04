// Package main provides a command-line tool to fetch models from Synthorai
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

// APIModel represents a model from the Synthorai catalog API.
type APIModel struct {
	Alias                    string    `json:"alias"`
	DisplayName              string    `json:"display_name"`
	Category                 string    `json:"category"`
	Capabilities             []string  `json:"capabilities"`
	InputModalities          []string  `json:"input_modalities"`
	MaxInputTokens           int64     `json:"max_input_tokens"`
	MaxOutputTokens          int64     `json:"max_output_tokens"`
	SupportsStructuredOutput bool      `json:"supports_structured_output"`
	Channels                 []Channel `json:"channels"`
}

// Channel carries one upstream route's prices for a model. A model may be
// served by several channels at identical prices; the first is representative.
type Channel struct {
	InputPerM        float64 `json:"input_per_million_tokens"`
	OutputPerM       float64 `json:"output_per_million_tokens"`
	CacheReadPerM    float64 `json:"cache_read_input_per_million_tokens"`
	CacheWrite5mPerM float64 `json:"cache_write_5m_input_per_million_tokens"`
}

// ModelsResponse is the response structure for the catalog API.
type ModelsResponse struct {
	Data    []APIModel `json:"data"`
	Success bool       `json:"success"`
}

const (
	minContextWindow  = 20000
	defaultLargeModel = "claude-opus-5"
	defaultSmallModel = "deepseek-v4-flash"
	maxTokensFactor   = 10
)

func fetchSynthoraiModels() (*ModelsResponse, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://synthorai.io/api/models",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &mr, nil
}

func has(list []string, want string) bool {
	return slices.Contains(list, want)
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func calculateMaxTokens(contextWindow, maxOutput, factor int64) int64 {
	if maxOutput == 0 || maxOutput > contextWindow/2 {
		return contextWindow / factor
	}
	return maxOutput
}

// buildReasoningConfig reports whether the catalog marks the model as
// reasoning-capable, and deliberately returns no effort levels.
//
// Probing the gateway on 2026-08-26 showed glm-5.2 and deepseek-v4-pro accept
// reasoning_effort but return an identical reasoning token count at every
// level, while kimi-k3 is genuinely graded. Since acceptance is not grading and
// the catalog does not distinguish the two, enumerating levels here would
// advertise a knob that does not move for most of them.
func buildReasoningConfig(canReason bool) ([]string, string) {
	if !canReason {
		return nil, ""
	}
	return nil, ""
}

func main() {
	modelsResp, err := fetchSynthoraiModels()
	if err != nil {
		log.Fatal("Error fetching Synthorai models:", err)
	}

	provider := catwalk.Provider{
		Name:                "Synthorai",
		ID:                  catwalk.InferenceProviderSynthorai,
		APIKey:              "$SYNTHORAI_API_KEY",
		APIEndpoint:         "https://synthorai.io/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: defaultLargeModel,
		DefaultSmallModelID: defaultSmallModel,
	}

	for _, m := range modelsResp.Data {
		if m.Category != "chat" {
			continue
		}
		if m.MaxInputTokens < minContextWindow {
			continue
		}
		if !has(m.InputModalities, "text") {
			continue
		}
		if len(m.Channels) == 0 {
			continue
		}

		// Channels for one alias carry identical prices; take the first.
		c := m.Channels[0]
		canReason := has(m.Capabilities, "reasoning") || has(m.Capabilities, "thinking")
		reasoningLevels, defaultReasoning := buildReasoningConfig(canReason)

		provider.Models = append(provider.Models, catwalk.Model{
			ID:                     m.Alias,
			Name:                   m.DisplayName,
			CostPer1MIn:            roundCost(c.InputPerM),
			CostPer1MOut:           roundCost(c.OutputPerM),
			CostPer1MInCached:      roundCost(c.CacheWrite5mPerM),
			CostPer1MOutCached:     roundCost(c.CacheReadPerM),
			ContextWindow:          m.MaxInputTokens,
			DefaultMaxTokens:       calculateMaxTokens(m.MaxInputTokens, m.MaxOutputTokens, maxTokensFactor),
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         has(m.InputModalities, "image"),
		})
	}

	if len(provider.Models) == 0 {
		log.Fatal("No models found or no models met the criteria")
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Synthorai provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/synthorai.json", data, 0o600); err != nil {
		log.Fatal("Error writing Synthorai provider config:", err)
	}

	fmt.Printf("\nSuccessfully wrote %d models to internal/providers/configs/synthorai.json\n", len(provider.Models))
}
