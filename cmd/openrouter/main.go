// Package main provides a command-line tool to fetch models from OpenRouter
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
	"strconv"
	"time"

	"github.com/charmbracelet/fur/pkg/provider"
)

// Model represents the complete model configuration.
type Model struct {
	ID              string       `json:"id"`
	CanonicalSlug   string       `json:"canonical_slug"`
	HuggingFaceID   string       `json:"hugging_face_id"`
	Name            string       `json:"name"`
	Created         int64        `json:"created"`
	Description     string       `json:"description"`
	ContextLength   int64        `json:"context_length"`
	Architecture    Architecture `json:"architecture"`
	Pricing         Pricing      `json:"pricing"`
	TopProvider     TopProvider  `json:"top_provider"`
	SupportedParams []string     `json:"supported_parameters"`
}

// Architecture defines the model's architecture details.
type Architecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

// Pricing contains the pricing information for different operations.
type Pricing struct {
	Prompt            string `json:"prompt"`
	Completion        string `json:"completion"`
	Request           string `json:"request"`
	Image             string `json:"image"`
	WebSearch         string `json:"web_search"`
	InternalReasoning string `json:"internal_reasoning"`
	InputCacheRead    string `json:"input_cache_read"`
	InputCacheWrite   string `json:"input_cache_write"`
}

// TopProvider describes the top provider's capabilities.
type TopProvider struct {
	ContextLength       int64  `json:"context_length"`
	MaxCompletionTokens *int64 `json:"max_completion_tokens"`
	IsModerated         bool   `json:"is_moderated"`
}

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

// ModelPricing is the pricing structure for a model, detailing costs per
// million tokens for input and output, both cached and uncached.
type ModelPricing struct {
	CostPer1MIn        float64 `json:"cost_per_1m_in"`
	CostPer1MOut       float64 `json:"cost_per_1m_out"`
	CostPer1MInCached  float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached float64 `json:"cost_per_1m_out_cached"`
}

func getPricing(model Model) ModelPricing {
	pricing := ModelPricing{}
	costPrompt, err := strconv.ParseFloat(model.Pricing.Prompt, 64)
	if err != nil {
		costPrompt = 0.0
	}
	pricing.CostPer1MIn = costPrompt * 1_000_000
	costCompletion, err := strconv.ParseFloat(model.Pricing.Completion, 64)
	if err != nil {
		costCompletion = 0.0
	}
	pricing.CostPer1MOut = costCompletion * 1_000_000

	costPromptCached, err := strconv.ParseFloat(model.Pricing.InputCacheWrite, 64)
	if err != nil {
		costPromptCached = 0.0
	}
	pricing.CostPer1MInCached = costPromptCached * 1_000_000
	costCompletionCached, err := strconv.ParseFloat(model.Pricing.InputCacheRead, 64)
	if err != nil {
		costCompletionCached = 0.0
	}
	pricing.CostPer1MOutCached = costCompletionCached * 1_000_000
	return pricing
}

func fetchOpenRouterModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://openrouter.ai/api/v1/models",
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

// This is used to generate the openrouter.json config file.
func main() {
	modelsResp, err := fetchOpenRouterModels()
	if err != nil {
		log.Fatal("Error fetching OpenRouter models:", err)
	}

	openRouterProvider := provider.Provider{
		Name:                "OpenRouter",
		ID:                  "openrouter",
		APIKey:              "$OPENROUTER_API_KEY",
		APIEndpoint:         "https://openrouter.ai/api/v1",
		Type:                provider.TypeOpenAI,
		DefaultLargeModelID: "anthropic/claude-sonnet-4",
		DefaultSmallModelID: "anthropic/claude-haiku-3.5",
		Models:              []provider.Model{},
	}

	for _, model := range modelsResp.Data {
		// skip non‐text models or those without tools
		if !slices.Contains(model.SupportedParams, "tools") ||
			!slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		pricing := getPricing(model)
		canReason := slices.Contains(model.SupportedParams, "reasoning")
		supportsImages := slices.Contains(model.Architecture.InputModalities, "image")

		m := provider.Model{
			ID:                 model.ID,
			Name:               model.Name,
			CostPer1MIn:        pricing.CostPer1MIn,
			CostPer1MOut:       pricing.CostPer1MOut,
			CostPer1MInCached:  pricing.CostPer1MInCached,
			CostPer1MOutCached: pricing.CostPer1MOutCached,
			ContextWindow:      model.ContextLength,
			CanReason:          canReason,
			SupportsImages:     supportsImages,
		}
		if model.TopProvider.MaxCompletionTokens != nil {
			m.DefaultMaxTokens = *model.TopProvider.MaxCompletionTokens / 2
		} else {
			m.DefaultMaxTokens = model.ContextLength / 10
		}
		openRouterProvider.Models = append(openRouterProvider.Models, m)
	}

	// save the json in internal/providers/config/openrouter.json
	data, err := json.MarshalIndent(openRouterProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling OpenRouter provider:", err)
	}
	// write to file
	if err := os.WriteFile("internal/providers/configs/openrouter.json", data, 0o600); err != nil {
		log.Fatal("Error writing OpenRouter provider config:", err)
	}
}
