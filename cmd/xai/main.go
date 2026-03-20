// Package main provides a command-line tool to fetch models from xAI
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

type ModelsResponse struct {
	Models []XAIModel `json:"models"`
}

type XAIModel struct {
	ID                       string   `json:"id"`
	Aliases                  []string `json:"aliases"`
	InputModalities          []string `json:"input_modalities"`
	OutputModalities         []string `json:"output_modalities"`
	PromptTextTokenPrice     int64    `json:"prompt_text_token_price"`
	CompletionTextTokenPrice int64    `json:"completion_text_token_price"`
	CachedPromptTextTokenPrc int64    `json:"cached_prompt_text_token_price"`
}

func shortestAlias(model XAIModel) string {
	if len(model.Aliases) == 0 {
		return model.ID
	}
	shortest := model.Aliases[0]
	for _, a := range model.Aliases[1:] {
		if len(a) < len(shortest) {
			shortest = a
		}
	}
	if len(shortest) < len(model.ID) {
		return shortest
	}
	return model.ID
}

var prettyNames = map[string]string{
	"grok-3":                      "Grok 3",
	"grok-3-mini":                 "Grok 3 Mini",
	"grok-4":                      "Grok 4",
	"grok-4-fast":                 "Grok 4 Fast",
	"grok-4-fast-non-reasoning":   "Grok 4 Fast Non-Reasoning",
	"grok-4-1-fast":               "Grok 4.1 Fast",
	"grok-4-1-fast-non-reasoning": "Grok 4.1 Fast Non-Reasoning",
	"grok-4.20":                   "Grok 4.20",
	"grok-4.20-non-reasoning":     "Grok 4.20 Non-Reasoning",
	"grok-4.20-multi-agent":       "Grok 4.20 Multi-Agent",
	"grok-code-fast":              "Grok Code Fast",
}

func prettyName(id string) string {
	if name, ok := prettyNames[id]; ok {
		return name
	}
	return id
}

func contextWindow(modelID string) int64 {
	if strings.Contains(modelID, "grok-4") {
		return 200_000
	}
	return 131_072
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func priceToDollarsPerMillion(centsPerHundredMillion int64) float64 {
	return roundCost(float64(centsPerHundredMillion) / 10_000)
}

func fetchXAIModels() (*ModelsResponse, error) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("XAI_API_KEY environment variable is not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.x.ai/v1/language-models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/xai-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func main() {
	modelsResp, err := fetchXAIModels()
	if err != nil {
		log.Fatal("Error fetching xAI models:", err)
	}

	provider := catwalk.Provider{
		Name:                "xAI",
		ID:                  catwalk.InferenceProviderXAI,
		APIKey:              "$XAI_API_KEY",
		APIEndpoint:         "https://api.x.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "grok-4.20",
		DefaultSmallModelID: "grok-4-1-fast",
	}

	for _, model := range modelsResp.Models {
		if strings.Contains(model.ID, "multi-agent") {
			continue
		}

		id := shortestAlias(model)
		ctxWindow := contextWindow(model.ID)
		defaultMaxTokens := ctxWindow / 10

		canReason := !strings.Contains(model.ID, "non-reasoning") &&
			model.ID != "grok-3"
		supportsImages := slices.Contains(model.InputModalities, "image")

		m := catwalk.Model{
			ID:                 id,
			Name:               prettyName(id),
			CostPer1MIn:        priceToDollarsPerMillion(model.PromptTextTokenPrice),
			CostPer1MOut:       priceToDollarsPerMillion(model.CompletionTextTokenPrice),
			CostPer1MInCached:  0,
			CostPer1MOutCached: priceToDollarsPerMillion(model.CachedPromptTextTokenPrc),
			ContextWindow:      ctxWindow,
			DefaultMaxTokens:   defaultMaxTokens,
			CanReason:          canReason,
			SupportsImages:     supportsImages,
		}

		provider.Models = append(provider.Models, m)
		fmt.Printf("Added model %s (alias: %s)\n", model.ID, id)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling xAI provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/xai.json", data, 0o600); err != nil {
		log.Fatal("Error writing xAI provider config:", err)
	}

	fmt.Printf("Generated xai.json with %d models\n", len(provider.Models))
}
