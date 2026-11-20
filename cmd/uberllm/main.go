// Package main provides a command-line tool to fetch models from UberLLM
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

	"charm.land/catwalk/v3/pkg/catwalk"
)

// UberLLMPricing is in US dollars per 1M tokens. UberLLM is a marketplace:
// the listed price follows the sellers and is refreshed every few minutes.
type UberLLMPricing struct {
	InputUSD     float64 `json:"input_usd"`
	OutputUSD    float64 `json:"output_usd"`
	CacheReadUSD float64 `json:"cache_read_usd"`
}

type UberLLMModel struct {
	ID              string         `json:"id"`
	DisplayName     string         `json:"display_name"`
	Type            string         `json:"type"`
	Available       bool           `json:"available"`
	ContextLength   int64          `json:"context_length"`
	MaxOutputTokens int64          `json:"max_output_tokens"`
	InputModalities []string       `json:"input_modalities"`
	SupportsTools   bool           `json:"supports_tools"`
	Pricing         UberLLMPricing `json:"pricing"`
}

type ModelsResponse struct {
	Data []UberLLMModel `json:"data"`
}

// curated lists the models offered in Crush. The catalog has hundreds of
// models from many sellers; these are coding models whose tool calls and
// reasoning_effort values were checked on the live API. Prices, context
// and output limits come from the catalog on every run.
var curated = map[string]struct {
	efforts       []string
	defaultEffort string
	maxTokens     int64
}{
	"anthropic/claude-haiku-4.5":   {maxTokens: 64000},
	"anthropic/claude-sonnet-5.5":  {maxTokens: 64000},
	"deepseek/deepseek-v4-pro":     {[]string{"low", "high", "max"}, "high", 128000},
	"deepseek/deepseek-v4.1-flash": {[]string{"low", "high", "max"}, "high", 131072},
	"google/gemini-3.5-flash":      {[]string{"minimal", "low", "medium", "high"}, "medium", 65536},
	"moonshotai/kimi-k3":           {[]string{"low", "high", "max"}, "high", 131072},
	"openai/gpt-5-mini":            {[]string{"low", "medium", "high"}, "medium", 128000},
	"openai/gpt-5.5":               {[]string{"low", "medium", "high", "xhigh"}, "medium", 128000},
	"qwen/qwen3-coder":             {maxTokens: 65536},
	"x-ai/grok-4.3":                {[]string{"low", "medium", "high"}, "medium", 32000},
	"z-ai/glm-5.3":                 {[]string{"low", "high", "max"}, "high", 131072},
}

func roundCost(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

func fetchModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading models response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/uberllm-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

func main() {
	uberllmProvider := catwalk.Provider{
		Name:                "UberLLM",
		ID:                  catwalk.InferenceProviderUberLLM,
		APIKey:              "$UBERLLM_API_KEY",
		APIEndpoint:         "https://api.uberllm.dev/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: "z-ai/glm-5.3",
		DefaultSmallModelID: "deepseek/deepseek-v4.1-flash",
	}

	modelsResp, err := fetchModels(uberllmProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching UberLLM models:", err)
	}

	for _, model := range modelsResp.Data {
		spec, ok := curated[model.ID]
		if !ok {
			continue
		}
		if !model.Available || model.Type != "chat" || !model.SupportsTools {
			fmt.Printf("Skipping unavailable model %s\n", model.ID)
			continue
		}

		reasoning := catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
		if len(spec.efforts) > 0 {
			reasoning = catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels(spec.efforts...),
				DefaultEffortLevel: spec.defaultEffort,
			}
		}

		maxTokens := spec.maxTokens
		if model.MaxOutputTokens > 0 {
			maxTokens = min(maxTokens, model.MaxOutputTokens)
		}

		uberllmProvider.Models = append(uberllmProvider.Models, catwalk.Model{
			ID:   model.ID,
			Name: model.DisplayName,
			Pricing: catwalk.Pricing{
				Input:    roundCost(model.Pricing.InputUSD),
				Output:   roundCost(model.Pricing.OutputUSD),
				CacheHit: roundCost(model.Pricing.CacheReadUSD),
			},
			ContextWindow:    model.ContextLength,
			DefaultMaxTokens: maxTokens,
			Reasoning:        reasoning,
			Capabilities: catwalk.Capabilities{
				Vision: slices.Contains(model.InputModalities, "image"),
				Audio:  slices.Contains(model.InputModalities, "audio"),
			},
		})
	}

	for _, id := range []string{uberllmProvider.DefaultLargeModelID, uberllmProvider.DefaultSmallModelID} {
		if !slices.ContainsFunc(uberllmProvider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			log.Fatalf("Default model %s is not in the catalog; keeping the existing config", id)
		}
	}

	slices.SortFunc(uberllmProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(uberllmProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling UberLLM provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/uberllm.json", data, 0o600); err != nil {
		log.Fatal("Error writing UberLLM provider config:", err)
	}

	fmt.Printf("Generated uberllm.json with %d models\n", len(uberllmProvider.Models))
}
