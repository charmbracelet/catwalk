// Package main provides a command-line tool to fetch models from 0G Private
// Computer and generate a configuration file for the provider.
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

// Model represents a model from the 0G Private Computer catalog
// (GET https://router-api.0g.ai/v1/models, public, no key).
type Model struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Type                string       `json:"type"`
	ContextLength       int64        `json:"context_length"`
	MaxCompletionTokens int64        `json:"max_completion_tokens"`
	Architecture        Architecture `json:"architecture"`
	SupportedParameters []string     `json:"supported_parameters"`
	PricingUSD          Pricing      `json:"pricing_usd"`
}

// Architecture lists the modalities a model accepts and returns.
type Architecture struct {
	InputModalities []string `json:"input_modalities"`
}

// Pricing holds per-token USD prices as strings ("0.000004").
type Pricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	CacheWrite string `json:"cache_write"`
}

// ModelsResponse is the response structure for the 0G models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

const (
	catalogURL = "https://router-api.0g.ai/v1/models"
	// minChatModels guards against writing a truncated catalog.
	minChatModels = 20
	// fallbackMaxTokens is used when the catalog has no max_completion_tokens.
	fallbackMaxTokens = 16384
)

func fetchModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", catalogURL, nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

// perMillion converts a per-token USD string to USD per 1M tokens.
func perMillion(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return math.Round(v*1e6*1e6) / 1e6, true
}

func main() {
	resp, err := fetchModels()
	if err != nil {
		log.Fatal("Error fetching 0G models:", err)
	}

	provider := catwalk.Provider{
		Name:                "0G Private Computer",
		ID:                  catwalk.InferenceProvider0G,
		APIKey:              "$OG_API_KEY",
		APIEndpoint:         "https://router-api.0g.ai/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: "claude-sonnet-5.5",
		DefaultSmallModelID: "glm-5.3-flash",
		DefaultHeaders: map[string]string{
			"HTTP-Referer": "https://charm.land",
			"X-Title":      "Crush",
		},
		Models: []catwalk.Model{},
	}

	for _, model := range resp.Data {
		// Crush is an agent: only chat models that take tools are useful.
		if model.Type != "chatbot" || !slices.Contains(model.SupportedParameters, "tools") {
			continue
		}
		input, okIn := perMillion(model.PricingUSD.Prompt)
		output, okOut := perMillion(model.PricingUSD.Completion)
		if !okIn || !okOut {
			continue
		}
		cacheCreate, _ := perMillion(model.PricingUSD.CacheWrite)

		// Effort is offered only where the router forwards reasoning_effort;
		// the catalog does not list levels, so the common three are used.
		reasoning := catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
		if slices.Contains(model.SupportedParameters, "reasoning_effort") {
			reasoning = catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("low", "medium", "high"),
				DefaultEffortLevel: "medium",
			}
		}

		maxTokens := model.MaxCompletionTokens
		if maxTokens <= 0 {
			maxTokens = min(fallbackMaxTokens, model.ContextLength)
		}

		provider.Models = append(provider.Models, catwalk.Model{
			ID:   model.ID,
			Name: model.Name,
			Pricing: catwalk.Pricing{
				Input:       input,
				Output:      output,
				CacheCreate: cacheCreate,
			},
			ContextWindow:    model.ContextLength,
			DefaultMaxTokens: maxTokens,
			Reasoning:        reasoning,
			Capabilities: catwalk.Capabilities{
				Vision: slices.Contains(model.Architecture.InputModalities, "image"),
			},
		})
	}

	if len(provider.Models) < minChatModels {
		log.Fatalf("0G catalog returned %d usable models, below %d; refusing to write a truncated config", len(provider.Models), minChatModels)
	}
	for _, id := range []string{provider.DefaultLargeModelID, provider.DefaultSmallModelID} {
		if !slices.ContainsFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			log.Fatalf("default model %q is not in the 0G catalog", id)
		}
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling 0G provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/0g.json", data, 0o600); err != nil {
		log.Fatal("Error writing 0G provider config:", err)
	}

	fmt.Printf("Generated 0g.json with %d models\n", len(provider.Models))
}
