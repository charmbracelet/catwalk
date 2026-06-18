// Package main provides a command-line tool to fetch models from Mistral AI
// and generate a configuration file for the provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

type ModelsResponse struct {
	Data []MistralModel `json:"data"`
}

type MistralModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	MaxContextLength int64             `json:"max_context_length"`
	Capabilities     MistralCapability `json:"capabilities"`
	Aliases          []string          `json:"aliases"`
	Deprecation      *string           `json:"deprecation"`
}

type MistralCapability struct {
	CompletionChat  bool `json:"completion_chat"`
	CompletionFIM   bool `json:"completion_fim"`
	FunctionCalling bool `json:"function_calling"`
	Vision          bool `json:"vision"`
}

type pricing struct {
	in  float64
	out float64
}

// Pricing per 1M tokens (USD), from https://mistral.ai/pricing
// Keyed by the canonical model ID we want to emit.
var modelPricing = map[string]pricing{
	"mistral-medium-latest":   {1.50, 7.50},
	"mistral-small-latest":    {0.10, 0.30},
	"mistral-large-latest":    {0.50, 1.50},
	"ministral-3b-latest":     {0.10, 0.10},
	"ministral-8b-latest":     {0.15, 0.15},
	"ministral-14b-latest":    {0.20, 0.20},
	"devstral-medium-latest":  {0.40, 2.00},
	"codestral-latest":        {0.30, 0.90},
	"mistral-tiny-latest":     {0.15, 0.15},
	"magistral-medium-latest": {2.00, 5.00},
	"magistral-small-latest":  {0.50, 1.50},
}

// The set of model IDs we want to include in the generated config.
// These are the canonical, user-facing identifiers.
var wantedModels = map[string]bool{
	"mistral-large-latest":    true,
	"mistral-medium-latest":   true,
	"mistral-small-latest":    true,
	"mistral-tiny-latest":     true,
	"devstral-medium-latest":  true,
	"codestral-latest":        true,
	"magistral-medium-latest": true,
	"magistral-small-latest":  true,
	"ministral-3b-latest":     true,
	"ministral-8b-latest":     true,
	"ministral-14b-latest":    true,
}

var prettyNames = map[string]string{
	"mistral-large-latest":    "Mistral Large 3",
	"mistral-medium-latest":   "Mistral Medium 3.5",
	"mistral-small-latest":    "Mistral Small 4",
	"mistral-tiny-latest":     "Mistral NeMo",
	"devstral-medium-latest":  "Devstral 2",
	"codestral-latest":        "Codestral",
	"magistral-medium-latest": "Magistral Medium",
	"magistral-small-latest":  "Magistral Small",
	"ministral-3b-latest":     "Ministral 3B",
	"ministral-8b-latest":     "Ministral 8B",
	"ministral-14b-latest":    "Ministral 14B",
}

func prettyName(id string) string {
	if name, ok := prettyNames[id]; ok {
		return name
	}
	return id
}

func fetchMistralModels() (*ModelsResponse, error) {
	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("MISTRAL_API_KEY environment variable is not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.mistral.ai/v1/models",
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
	_ = os.WriteFile("tmp/mistral-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func main() {
	modelsResp, err := fetchMistralModels()
	if err != nil {
		log.Fatal("Error fetching Mistral models:", err)
	}

	provider := catwalk.Provider{
		Name:                "Mistral AI",
		ID:                  catwalk.InferenceProviderMistral,
		APIKey:              "$MISTRAL_API_KEY",
		APIEndpoint:         "https://api.mistral.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "mistral-medium-latest",
		DefaultSmallModelID: "mistral-small-latest",
	}

	// Index models by ID for quick lookup
	modelByID := make(map[string]MistralModel)
	for _, m := range modelsResp.Data {
		modelByID[m.ID] = m
	}

	// Only emit the canonical IDs we want, deduplicated
	seen := make(map[string]bool)
	for _, model := range modelsResp.Data {
		if !wantedModels[model.ID] {
			continue
		}
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true

		if model.Deprecation != nil {
			continue
		}
		if !model.Capabilities.CompletionChat {
			continue
		}

		p := modelPricing[model.ID]
		ctxWindow := model.MaxContextLength
		defaultMaxTokens := ctxWindow / 10

		canReason := strings.Contains(model.ID, "magistral")

		var (
			reasoningLevels  []string
			defaultReasoning string
		)
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   prettyName(model.ID),
			CostPer1MIn:            p.in,
			CostPer1MOut:           p.out,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			ContextWindow:          ctxWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         model.Capabilities.Vision,
		}

		provider.Models = append(provider.Models, m)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Mistral provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/mistral.json", data, 0o600); err != nil {
		log.Fatal("Error writing Mistral provider config:", err)
	}

	fmt.Printf("Generated mistral.json with %d models\n", len(provider.Models))
}
