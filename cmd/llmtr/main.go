// Package main provides a command-line tool to fetch models from LLMTR
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
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Model represents a model from the LLMTR models API.
type Model struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	ContextLength      int64        `json:"context_length"`
	Architecture       Architecture `json:"architecture"`
	Pricing            Pricing      `json:"pricing"`
	TopProvider        TopProvider  `json:"top_provider"`
	SupportedParams    []string     `json:"supported_parameters"`
	SupportedEndpoints []string     `json:"supported_endpoints"`
	Reasoning          *Reasoning   `json:"reasoning"`
}

// Architecture describes the input and output modalities of a model.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// Pricing contains the price per token in US dollars, as strings.
type Pricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// TopProvider describes the limits of the upstream serving the model.
type TopProvider struct {
	ContextLength       int64  `json:"context_length"`
	MaxCompletionTokens *int64 `json:"max_completion_tokens"`
}

// Reasoning describes how a model reasons.
type Reasoning struct {
	Mandatory        bool     `json:"mandatory"`
	SupportedEfforts []string `json:"supported_efforts"`
}

// ModelsResponse is the response structure for the LLMTR models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func perMillion(price string) float64 {
	v, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return 0
	}
	return roundCost(v * 1_000_000)
}

func fetchLLMTRModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://llmtr.com/v1/models",
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
	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func reasoningFor(model Model) catwalk.Reasoning {
	if model.Reasoning != nil && model.Reasoning.Mandatory {
		return catwalk.Reasoning{Thinking: catwalk.ThinkingAlways}
	}
	if !slices.Contains(model.SupportedParams, "reasoning") {
		return catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
	}
	reasoning := catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable}
	if model.Reasoning != nil &&
		len(model.Reasoning.SupportedEfforts) > 0 &&
		slices.Contains(model.SupportedParams, "reasoning_effort") {
		efforts := model.Reasoning.SupportedEfforts
		reasoning.EffortLevels = catwalk.NewEffortLevels(efforts...)
		reasoning.DefaultEffortLevel = efforts[0]
		if slices.Contains(efforts, "medium") {
			reasoning.DefaultEffortLevel = "medium"
		}
	}
	return reasoning
}

func main() {
	modelsResp, err := fetchLLMTRModels()
	if err != nil {
		log.Fatal("Error fetching LLMTR models:", err)
	}

	llmtrProvider := catwalk.Provider{
		Name:                "LLMTR",
		ID:                  catwalk.InferenceProviderLLMTR,
		APIKey:              "$LLMTR_API_KEY",
		APIEndpoint:         "https://llmtr.com/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: "anthropic/claude-sonnet-4.6",
		DefaultSmallModelID: "anthropic/claude-haiku-4.5",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		// Only chat completions models with tool calling and text in and out.
		if !slices.Contains(model.SupportedEndpoints, "/v1/chat/completions") ||
			!slices.Contains(model.SupportedParams, "tools") ||
			!slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}
		if model.ContextLength < 20000 {
			continue
		}

		m := catwalk.Model{
			ID:   model.ID,
			Name: model.Name,
			Pricing: catwalk.Pricing{
				Input:       perMillion(model.Pricing.Prompt),
				Output:      perMillion(model.Pricing.Completion),
				CacheCreate: perMillion(model.Pricing.InputCacheWrite),
				CacheHit:    perMillion(model.Pricing.InputCacheRead),
			},
			ContextWindow: model.ContextLength,
			Reasoning:     reasoningFor(model),
			Capabilities: catwalk.Capabilities{
				Vision: slices.Contains(model.Architecture.InputModalities, "image"),
				Audio:  slices.Contains(model.Architecture.InputModalities, "audio"),
			},
		}
		if model.TopProvider.MaxCompletionTokens != nil {
			m.DefaultMaxTokens = *model.TopProvider.MaxCompletionTokens / 2
		} else {
			m.DefaultMaxTokens = model.ContextLength / 10
		}

		llmtrProvider.Models = append(llmtrProvider.Models, m)
	}

	slices.SortFunc(llmtrProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(llmtrProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling LLMTR provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/llmtr.json", data, 0o600); err != nil {
		log.Fatal("Error writing LLMTR provider config:", err)
	}

	fmt.Printf("Generated llmtr.json with %d models\n", len(llmtrProvider.Models))
}
