// Package main provides a command-line tool to fetch models from AIHubMix
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

// APIModel represents a model from the AIHubMix API.
type APIModel struct {
	ModelID         string  `json:"model_id"`
	ModelName       string  `json:"model_name"`
	Desc            string  `json:"desc"`
	Pricing         Pricing `json:"pricing"`
	Types           string  `json:"types"`
	Features        string  `json:"features"`
	InputModalities string  `json:"input_modalities"`
	MaxOutput       int64   `json:"max_output"`
	ContextLength   int64   `json:"context_length"`
}

// Pricing contains the pricing information from the API.
type Pricing struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

const (
	minContextWindow  = 20000
	defaultLargeModel = "gpt-5"
	defaultSmallModel = "gpt-5-nano"
	maxTokensFactor   = 10
)

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Data    []APIModel `json:"data"`
	Message string     `json:"message"`
	Success bool       `json:"success"`
}

func fetchAIHubMixModels() (*ModelsResponse, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://aihubmix.com/api/v1/models?type=llm",
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

func hasField(s, field string) bool {
	if s == "" {
		return false
	}
	for item := range strings.SplitSeq(s, ",") {
		if strings.TrimSpace(item) == field {
			return true
		}
	}
	return false
}

func parseFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func calculateMaxTokens(contextLength, maxOutput, factor int64) int64 {
	if maxOutput == 0 || maxOutput > contextLength/2 {
		return contextLength / factor
	}
	return maxOutput
}

func buildReasoningConfig(canReason bool) ([]string, string) {
	if !canReason {
		return nil, ""
	}
	return []string{"low", "medium", "high"}, "medium"
}

func main() {
	modelsResp, err := fetchAIHubMixModels()
	if err != nil {
		log.Fatal("Error fetching AIHubMix models:", err)
	}

	aiHubMixProvider := catwalk.Provider{
		Name:                "AIHubMix",
		ID:                  catwalk.InferenceAIHubMix,
		APIKey:              "$AIHUBMIX_API_KEY",
		APIEndpoint:         "https://aihubmix.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: defaultLargeModel,
		DefaultSmallModelID: defaultSmallModel,
		DefaultHeaders: map[string]string{
			"APP-Code": "IUFF7106",
		},
	}

	for _, model := range modelsResp.Data {
		if model.ContextLength < minContextWindow {
			continue
		}
		if !hasField(model.InputModalities, "text") {
			continue
		}

		canReason := hasField(model.Features, "thinking")
		supportsImages := hasField(model.InputModalities, "image")

		reasoningLevels, defaultReasoning := buildReasoningConfig(canReason)
		maxTokens := calculateMaxTokens(model.ContextLength, model.MaxOutput, maxTokensFactor)

		aiHubMixProvider.Models = append(aiHubMixProvider.Models, catwalk.Model{
			ID:                     model.ModelID,
			Name:                   model.ModelName,
			CostPer1MIn:            parseFloat(model.Pricing.Input),
			CostPer1MOut:           parseFloat(model.Pricing.Output),
			CostPer1MInCached:      parseFloat(model.Pricing.CacheWrite),
			CostPer1MOutCached:     parseFloat(model.Pricing.CacheRead),
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       maxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		})

		fmt.Printf("Added model %s with context window %d\n",
			model.ModelID, model.ContextLength)
	}

	if len(aiHubMixProvider.Models) == 0 {
		log.Fatal("No models found or no models met the criteria")
	}

	slices.SortFunc(aiHubMixProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(aiHubMixProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling AIHubMix provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/aihubmix.json", data, 0o600); err != nil {
		log.Fatal("Error writing AIHubMix provider config:", err)
	}

	fmt.Printf("\nSuccessfully wrote %d models to internal/providers/configs/aihubmix.json\n", len(aiHubMixProvider.Models))
}
