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

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

// APIModel represents a model from the AIHubMix API.
type APIModel struct {
	ModelID         string  `json:"model_id"`
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

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Data    []APIModel `json:"data"`
	Message string     `json:"message"`
	Success bool       `json:"success"`
}

func fetchAIHubMixModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://aihubmix.com/api/v1/models?type=llm",
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
	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func hasFeature(features, feature string) bool {
	if features == "" {
		return false
	}
	for f := range strings.SplitSeq(features, ",") {
		if strings.TrimSpace(f) == feature {
			return true
		}
	}
	return false
}

func hasModality(modalities, modality string) bool {
	if modalities == "" {
		return false
	}
	for m := range strings.SplitSeq(modalities, ",") {
		if strings.TrimSpace(m) == modality {
			return true
		}
	}
	return false
}

func parseFloat(p *float64) float64 {
	if p == nil {
		return 0.0
	}
	return *p
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
		DefaultLargeModelID: "gpt-5",
		DefaultSmallModelID: "gpt-5-nano",
		Models:              []catwalk.Model{},
		DefaultHeaders: map[string]string{
			"APP-Code": "IUFF7106",
		},
	}

	for _, model := range modelsResp.Data {
		// Skip models with context window < 20000
		if model.ContextLength < 20000 {
			continue
		}

		// Check for text I/O support
		if !hasModality(model.InputModalities, "text") {
			continue
		}

		// Check reasoning capability
		canReason := hasFeature(model.Features, "thinking")

		// Check image support
		supportsImages := hasModality(model.InputModalities, "image")

		// Parse reasoning levels
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// Calculate default max tokens
		defaultMaxTokens := model.MaxOutput
		if defaultMaxTokens == 0 || defaultMaxTokens > model.ContextLength/2 {
			defaultMaxTokens = model.ContextLength / 10
		}

		catwalkModel := catwalk.Model{
			ID:                     model.ModelID,
			Name:                   model.ModelID,
			CostPer1MIn:            parseFloat(model.Pricing.Input),
			CostPer1MOut:           parseFloat(model.Pricing.Output),
			CostPer1MInCached:      parseFloat(model.Pricing.CacheWrite),
			CostPer1MOutCached:     parseFloat(model.Pricing.CacheRead),
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         supportsImages,
		}

		aiHubMixProvider.Models = append(aiHubMixProvider.Models, catwalkModel)
		fmt.Printf("Added model %s with context window %d\n",
			model.ModelID, model.ContextLength)
	}

	if len(aiHubMixProvider.Models) == 0 {
		log.Fatal("No models found or no models met the criteria")
	}

	slices.SortFunc(aiHubMixProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
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
