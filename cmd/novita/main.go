// Package main provides a command-line tool to fetch models from NovitaAI and
// generate a configuration file for the provider.
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

type NovitaAIModel struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"display_name"`
	InputTokenPricePerM  float64  `json:"input_token_price_per_m"`
	OutputTokenPricePerM float64  `json:"output_token_price_per_m"`
	ContextSize          int64    `json:"context_size"`
	MaxOutputTokens      int64    `json:"max_output_tokens"`
	Status               int      `json:"status"`
	ModelType            string   `json:"model_type"`
	Features             []string `json:"features"`
	Endpoints            []string `json:"endpoints"`
	InputModalities      []string `json:"input_modalities"`
}

type ModelsResponse struct {
	Data []NovitaAIModel `json:"data"`
}

func main() {
	novitaAIProvider := catwalk.Provider{
		Name:                "NovitaAI",
		ID:                  catwalk.InferenceProviderNovitaAI,
		APIKey:              "$NOVITA_API_KEY",
		APIEndpoint:         "https://api.novita.ai/openai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "moonshotai/kimi-k2.5",
		DefaultSmallModelID: "zai-org/glm-4.7-flash",
	}

	modelsResp, err := fetchNovitaAIModels(novitaAIProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching NovitaAI models:", err)
	}

	for _, model := range modelsResp.Data {
		if shouldSkipModel(model) {
			fmt.Printf("Skipping model %s\n", model.ID)
			continue
		}

		var reasoningLevels []string
		var defaultReasoning string
		if supportsReasoningLevels(model.ID) {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		name := model.DisplayName
		if name == "" {
			name = fallbackDisplayName(model.ID)
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   name,
			CostPer1MIn:            roundCost(model.InputTokenPricePerM / 10000),
			CostPer1MOut:           roundCost(model.OutputTokenPricePerM / 10000),
			ContextWindow:          model.ContextSize,
			DefaultMaxTokens:       model.MaxOutputTokens,
			CanReason:              contains(model.Features, "reasoning"),
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         contains(model.InputModalities, "image"),
		}

		novitaAIProvider.Models = append(novitaAIProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, model.ContextSize)
	}

	slices.SortFunc(novitaAIProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(novitaAIProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling NovitaAI provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/novita.json", data, 0o600); err != nil {
		log.Fatal("Error writing NovitaAI provider config:", err)
	}

	fmt.Printf("Generated novita.json with %d models\n", len(novitaAIProvider.Models))
}

func fetchNovitaAIModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, apiEndpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Charm-Catwalk/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading models response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/novita-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

func shouldSkipModel(model NovitaAIModel) bool {
	if model.Status != 1 || model.ModelType != "chat" {
		return true
	}
	if model.ContextSize < 100000 {
		return true
	}
	if model.InputTokenPricePerM <= 0 || model.OutputTokenPricePerM <= 0 {
		return true
	}
	if !contains(model.Endpoints, "chat/completions") {
		return true
	}
	if !contains(model.Features, "function-calling") {
		return true
	}

	id := strings.ToLower(model.ID)
	return strings.HasPrefix(id, "ai_infer_test_") ||
		strings.HasPrefix(id, "dev/") ||
		id == "bunny" ||
		id == "elephant"
}

func fallbackDisplayName(id string) string {
	name := id
	if idx := strings.Index(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	return strings.ReplaceAll(name, "-", " ")
}

func contains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func supportsReasoningLevels(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "gpt-oss")
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}
