// Package main provides a command-line tool to fetch models from Neuralwatt
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

	"charm.land/catwalk/pkg/catwalk"
)

type Pricing struct {
	InputPerMillion        *float64 `json:"input_per_million"`
	OutputPerMillion       *float64 `json:"output_per_million"`
	CachedInputPerMillion  *float64 `json:"cached_input_per_million"`
	CachedOutputPerMillion *float64 `json:"cached_output_per_million"`
	PricingTBD             bool     `json:"pricing_tbd"`
}

type Capabilities struct {
	Tools           bool `json:"tools"`
	Vision          bool `json:"vision"`
	Reasoning       bool `json:"reasoning"`
	ReasoningEffort bool `json:"reasoning_effort"`
}

type Limits struct {
	MaxOutputTokens *int64 `json:"max_output_tokens"`
}

type Metadata struct {
	DisplayName  string       `json:"display_name"`
	Pricing      Pricing      `json:"pricing"`
	Capabilities Capabilities `json:"capabilities"`
	Limits       Limits       `json:"limits"`
	Deprecated   bool         `json:"deprecated"`
}

type NeuralwattModel struct {
	ID          string   `json:"id"`
	MaxModelLen int64    `json:"max_model_len"`
	Metadata    Metadata `json:"metadata"`
}

type ModelsResponse struct {
	Data []NeuralwattModel `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func ptrDeref[T any](v *T, fallback T) T {
	if v == nil {
		return fallback
	}
	return *v
}

func fetchNeuralwattModels(apiEndpoint string) (*ModelsResponse, error) {
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

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/neuralwatt-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

func fallbackDisplayName(id string) string {
	name := id
	if idx := strings.Index(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	return strings.ReplaceAll(name, "-", " ")
}

func main() {
	neuralwattProvider := catwalk.Provider{
		Name:                "Neuralwatt",
		ID:                  "neuralwatt",
		APIKey:              "$NEURALWATT_API_KEY",
		APIEndpoint:         "https://api.neuralwatt.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "zai-org/GLM-5.1-FP8",
		DefaultSmallModelID: "mistralai/Devstral-Small-2-24B-Instruct-2512",
	}

	modelsResp, err := fetchNeuralwattModels(neuralwattProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Neuralwatt models:", err)
	}

	for _, model := range modelsResp.Data {
		meta := model.Metadata

		if meta.Deprecated {
			fmt.Printf("Skipping deprecated model %s\n", model.ID)
			continue
		}

		// Skip models with small context windows
		if model.MaxModelLen < 20000 {
			fmt.Printf("Skipping model %s: context %d < 20000\n",
				model.ID, model.MaxModelLen)
			continue
		}

		if !meta.Capabilities.Tools {
			fmt.Printf("Skipping model %s (no tool support)\n", model.ID)
			continue
		}

		costIn := ptrDeref(meta.Pricing.InputPerMillion, 0)
		costOut := ptrDeref(meta.Pricing.OutputPerMillion, 0)
		// Null cached pricing means same as non-cached
		costInCached := ptrDeref(meta.Pricing.CachedInputPerMillion, costIn)
		costOutCached := ptrDeref(meta.Pricing.CachedOutputPerMillion, costOut)

		var defaultMaxTokens int64
		if meta.Limits.MaxOutputTokens != nil {
			defaultMaxTokens = *meta.Limits.MaxOutputTokens
		} else {
			defaultMaxTokens = model.MaxModelLen / 10
		}

		var reasoningLevels []string
		var defaultReasoning string
		if meta.Capabilities.ReasoningEffort {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		name := meta.DisplayName
		if name == "" {
			name = fallbackDisplayName(model.ID)
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   name,
			CostPer1MIn:            roundCost(costIn),
			CostPer1MOut:           roundCost(costOut),
			CostPer1MInCached:      roundCost(costInCached),
			CostPer1MOutCached:     roundCost(costOutCached),
			ContextWindow:          model.MaxModelLen,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              meta.Capabilities.Reasoning,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
			SupportsImages:         meta.Capabilities.Vision,
		}

		neuralwattProvider.Models = append(neuralwattProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, model.MaxModelLen)
	}

	slices.SortFunc(neuralwattProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(neuralwattProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Neuralwatt provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/neuralwatt.json", data, 0o600); err != nil {
		log.Fatal("Error writing Neuralwatt provider config:", err)
	}

	fmt.Printf("Generated neuralwatt.json with %d models\n", len(neuralwattProvider.Models))
}
