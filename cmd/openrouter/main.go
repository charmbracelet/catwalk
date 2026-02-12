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
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
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

// Endpoint represents a single endpoint configuration for a model.
type Endpoint struct {
	Name                string   `json:"name"`
	ContextLength       int64    `json:"context_length"`
	Pricing             Pricing  `json:"pricing"`
	ProviderName        string   `json:"provider_name"`
	Tag                 string   `json:"tag"`
	Quantization        *string  `json:"quantization"`
	MaxCompletionTokens *int64   `json:"max_completion_tokens"`
	MaxPromptTokens     *int64   `json:"max_prompt_tokens"`
	SupportedParams     []string `json:"supported_parameters"`
	Status              int      `json:"status"`
	UptimeLast30m       float64  `json:"uptime_last_30m"`
}

// EndpointsResponse is the response structure for the endpoints API.
type EndpointsResponse struct {
	Data struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		Created     int64      `json:"created"`
		Description string     `json:"description"`
		Endpoints   []Endpoint `json:"endpoints"`
	} `json:"data"`
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read models response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	// for debugging
	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/openrouter-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func fetchModelEndpoints(modelID string) (*EndpointsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://openrouter.ai/api/v1/models/%s/endpoints", modelID)
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		url,
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
	var er EndpointsResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &er, nil
}

func selectBestEndpoint(endpoints []Endpoint) *Endpoint {
	if len(endpoints) == 0 {
		return nil
	}

	var best *Endpoint
	for i := range endpoints {
		endpoint := &endpoints[i]
		// Skip endpoints with poor status or uptime
		if endpoint.Status < 0 || endpoint.UptimeLast30m < 90.0 {
			continue
		}

		if best == nil {
			best = endpoint
			continue
		}

		if isBetterEndpoint(endpoint, best) {
			best = endpoint
		}
	}

	// If no good endpoint found, return the first one as fallback
	if best == nil {
		best = &endpoints[0]
	}

	return best
}

func isBetterEndpoint(candidate, current *Endpoint) bool {
	candidateHasTools := slices.Contains(candidate.SupportedParams, "tools")
	currentHasTools := slices.Contains(current.SupportedParams, "tools")

	// Prefer endpoints with tool support over those without
	if candidateHasTools && !currentHasTools {
		return true
	}
	if !candidateHasTools && currentHasTools {
		return false
	}

	// Both have same tool support status, compare other factors
	if candidate.ContextLength > current.ContextLength {
		return true
	}
	if candidate.ContextLength == current.ContextLength {
		return candidate.UptimeLast30m > current.UptimeLast30m
	}

	return false
}

// This is used to generate the openrouter.json config file.
func main() {
	modelsResp, err := fetchOpenRouterModels()
	if err != nil {
		log.Fatal("Error fetching OpenRouter models:", err)
	}

	openRouterProvider := catwalk.Provider{
		Name:                "OpenRouter",
		ID:                  "openrouter",
		APIKey:              "$OPENROUTER_API_KEY",
		APIEndpoint:         "https://openrouter.ai/api/v1",
		Type:                catwalk.TypeOpenRouter,
		DefaultLargeModelID: "anthropic/claude-sonnet-4",
		DefaultSmallModelID: "anthropic/claude-3.5-haiku",
		Models:              []catwalk.Model{},
		DefaultHeaders: map[string]string{
			"HTTP-Referer": "https://charm.land",
			"X-Title":      "Crush",
		},
	}

	for _, model := range modelsResp.Data {
		if model.ContextLength < 20000 {
			continue
		}
		// skip non‐text models or those without tools
		if !slices.Contains(model.SupportedParams, "tools") ||
			!slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		// Fetch endpoints for this model to get the best configuration
		endpointsResp, err := fetchModelEndpoints(model.ID)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch endpoints for %s: %v\n", model.ID, err)
			// Fall back to using the original model data
			pricing := getPricing(model)
			canReason := slices.Contains(model.SupportedParams, "reasoning")
			supportsImages := slices.Contains(model.Architecture.InputModalities, "image")

			var reasoningLevels []string
			var defaultReasoning string
			if canReason {
				reasoningLevels = []string{"low", "medium", "high"}
				defaultReasoning = "medium"
			}
			m := catwalk.Model{
				ID:                     model.ID,
				Name:                   model.Name,
				CostPer1MIn:            pricing.CostPer1MIn,
				CostPer1MOut:           pricing.CostPer1MOut,
				CostPer1MInCached:      pricing.CostPer1MInCached,
				CostPer1MOutCached:     pricing.CostPer1MOutCached,
				ContextWindow:          model.ContextLength,
				CanReason:              canReason,
				DefaultReasoningEffort: defaultReasoning,
				ReasoningLevels:        reasoningLevels,
				SupportsImages:         supportsImages,
			}
			if model.TopProvider.MaxCompletionTokens != nil {
				m.DefaultMaxTokens = *model.TopProvider.MaxCompletionTokens / 2
			} else {
				m.DefaultMaxTokens = model.ContextLength / 10
			}
			if model.TopProvider.ContextLength > 0 {
				m.ContextWindow = model.TopProvider.ContextLength
			}
			openRouterProvider.Models = append(openRouterProvider.Models, m)
			continue
		}

		// Select the best endpoint
		bestEndpoint := selectBestEndpoint(endpointsResp.Data.Endpoints)
		if bestEndpoint == nil {
			fmt.Printf("Warning: No suitable endpoint found for %s\n", model.ID)
			continue
		}

		// Check if the best endpoint supports tools
		if !slices.Contains(bestEndpoint.SupportedParams, "tools") {
			continue
		}

		// Use the best endpoint's configuration
		pricing := ModelPricing{}
		costPrompt, err := strconv.ParseFloat(bestEndpoint.Pricing.Prompt, 64)
		if err != nil {
			costPrompt = 0.0
		}
		pricing.CostPer1MIn = costPrompt * 1_000_000
		costCompletion, err := strconv.ParseFloat(bestEndpoint.Pricing.Completion, 64)
		if err != nil {
			costCompletion = 0.0
		}
		pricing.CostPer1MOut = costCompletion * 1_000_000

		costPromptCached, err := strconv.ParseFloat(bestEndpoint.Pricing.InputCacheWrite, 64)
		if err != nil {
			costPromptCached = 0.0
		}
		pricing.CostPer1MInCached = costPromptCached * 1_000_000
		costCompletionCached, err := strconv.ParseFloat(bestEndpoint.Pricing.InputCacheRead, 64)
		if err != nil {
			costCompletionCached = 0.0
		}
		pricing.CostPer1MOutCached = costCompletionCached * 1_000_000

		canReason := slices.Contains(bestEndpoint.SupportedParams, "reasoning")
		supportsImages := slices.Contains(model.Architecture.InputModalities, "image")

		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}
		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            pricing.CostPer1MIn,
			CostPer1MOut:           pricing.CostPer1MOut,
			CostPer1MInCached:      pricing.CostPer1MInCached,
			CostPer1MOutCached:     pricing.CostPer1MOutCached,
			ContextWindow:          bestEndpoint.ContextLength,
			CanReason:              canReason,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
			SupportsImages:         supportsImages,
		}

		// Set max tokens based on the best endpoint
		if bestEndpoint.MaxCompletionTokens != nil {
			m.DefaultMaxTokens = *bestEndpoint.MaxCompletionTokens / 2
		} else {
			m.DefaultMaxTokens = bestEndpoint.ContextLength / 10
		}

		openRouterProvider.Models = append(openRouterProvider.Models, m)
		fmt.Printf("Added model %s with context window %d from provider %s\n",
			model.ID, bestEndpoint.ContextLength, bestEndpoint.ProviderName)
	}

	slices.SortFunc(openRouterProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

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
