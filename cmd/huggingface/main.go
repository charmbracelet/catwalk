// Package main provides a command-line tool to fetch models from Hugging Face Router
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

// SupportedProviders defines which providers we want to support.
// Add or remove providers from this slice to control which ones are included.
var SupportedProviders = []string{
	// "together", // Multiple issues
	"fireworks-ai",
	//"nebius",
	// "novita", // Usage report is wrong
	"groq",
	"cerebras",
	// "hyperbolic",
	// "nscale",
	// "sambanova",
	// "cohere",
	"hf-inference",
}

// Model represents a model from the Hugging Face Router API.
type Model struct {
	ID        string     `json:"id"`
	Object    string     `json:"object"`
	Created   int64      `json:"created"`
	OwnedBy   string     `json:"owned_by"`
	Providers []Provider `json:"providers"`
}

// Provider represents a provider configuration for a model.
type Provider struct {
	Provider                 string   `json:"provider"`
	Status                   string   `json:"status"`
	ContextLength            int64    `json:"context_length,omitempty"`
	Pricing                  *Pricing `json:"pricing,omitempty"`
	SupportsTools            bool     `json:"supports_tools"`
	SupportsStructuredOutput bool     `json:"supports_structured_output"`
}

// Pricing contains the pricing information for a provider.
type Pricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// ModelsResponse is the response structure for the Hugging Face Router models API.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func fetchHuggingFaceModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://router.huggingface.co/v1/models",
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

// findContextWindow looks for a context window from any provider for the given model.
func findContextWindow(model Model) int64 {
	for _, provider := range model.Providers {
		if provider.ContextLength > 0 {
			return provider.ContextLength
		}
	}
	return 0
}

// WARN: DO NOT USE
// for now we have a subset list of models we use.
func main() {
	modelsResp, err := fetchHuggingFaceModels()
	if err != nil {
		log.Fatal("Error fetching Hugging Face models:", err)
	}

	hfProvider := catwalk.Provider{
		Name:                "Hugging Face",
		ID:                  catwalk.InferenceProviderHuggingFace,
		APIKey:              "$HF_TOKEN",
		APIEndpoint:         "https://router.huggingface.co/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "moonshotai/Kimi-K2-Instruct-0905:groq",
		DefaultSmallModelID: "openai/gpt-oss-20b",
		Models:              []catwalk.Model{},
		DefaultHeaders: map[string]string{
			"HTTP-Referer": "https://charm.land",
			"X-Title":      "Crush",
		},
	}

	for _, model := range modelsResp.Data {
		// Find context window from any provider for this model
		fallbackContextLength := findContextWindow(model)
		if fallbackContextLength == 0 {
			fmt.Printf("Skipping model %s - no context window found in any provider\n", model.ID)
			continue
		}

		for _, provider := range model.Providers {
			// Skip unsupported providers
			if !slices.Contains(SupportedProviders, provider.Provider) {
				continue
			}

			// Skip providers that don't support tools
			if !provider.SupportsTools {
				continue
			}

			// Skip non-live providers
			if provider.Status != "live" {
				continue
			}

			// Create model with provider-specific ID and name
			modelID := fmt.Sprintf("%s:%s", model.ID, provider.Provider)
			modelName := fmt.Sprintf("%s (%s)", model.ID, provider.Provider)

			// Use provider's context length, or fallback if not available
			contextLength := provider.ContextLength
			if contextLength == 0 {
				contextLength = fallbackContextLength
			}

			// Calculate pricing (convert from per-token to per-1M tokens)
			var costPer1MIn, costPer1MOut float64
			if provider.Pricing != nil {
				costPer1MIn = provider.Pricing.Input
				costPer1MOut = provider.Pricing.Output
			}

			// Set default max tokens (conservative estimate)
			defaultMaxTokens := min(contextLength/4, 8192)

			m := catwalk.Model{
				ID:                 modelID,
				Name:               modelName,
				CostPer1MIn:        costPer1MIn,
				CostPer1MOut:       costPer1MOut,
				CostPer1MInCached:  0, // Not provided by HF Router
				CostPer1MOutCached: 0, // Not provided by HF Router
				ContextWindow:      contextLength,
				DefaultMaxTokens:   defaultMaxTokens,
				CanReason:          false, // Not provided by HF Router
				SupportsImages:     false, // Not provided by HF Router
			}

			hfProvider.Models = append(hfProvider.Models, m)
			fmt.Printf("Added model %s with context window %d from provider %s\n",
				modelID, contextLength, provider.Provider)
		}
	}

	slices.SortFunc(hfProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/huggingface.json
	data, err := json.MarshalIndent(hfProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Hugging Face provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/huggingface.json", data, 0o600); err != nil {
		log.Fatal("Error writing Hugging Face provider config:", err)
	}

	fmt.Printf("Generated huggingface.json with %d models\n", len(hfProvider.Models))
}
