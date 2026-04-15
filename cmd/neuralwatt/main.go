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

type NeuralwattModel struct {
	ID          string `json:"id"`
	MaxModelLen int64  `json:"max_model_len"`
}

type ModelsResponse struct {
	Data []NeuralwattModel `json:"data"`
}

// ModelMeta contains the hardcoded metadata for a Neuralwatt model.
// The API only returns id and max_model_len, so pricing and capabilities
// are sourced from the pricing page at https://portal.neuralwatt.com/pricing.
type ModelMeta struct {
	Tools        bool
	Reasoning    bool
	Vision       bool
	CostPer1MIn  float64
	CostPer1MOut float64
}

var modelMetadata = map[string]ModelMeta{
	"mistralai/Devstral-Small-2-24B-Instruct-2512": {
		Tools:        true,
		Reasoning:    false,
		Vision:       true,
		CostPer1MIn:  0.1,
		CostPer1MOut: 0.3,
	},
	"zai-org/GLM-5.1-FP8": {
		Tools:        true,
		Reasoning:    true,
		Vision:       false,
		CostPer1MIn:  1.1,
		CostPer1MOut: 3.6,
	},
	"glm-5.1-fast": {
		Tools:        true,
		Reasoning:    false,
		Vision:       false,
		CostPer1MIn:  1.1,
		CostPer1MOut: 3.6,
	},
	"openai/gpt-oss-20b": {
		Tools:        true,
		Reasoning:    false,
		Vision:       false,
		CostPer1MIn:  0.0,
		CostPer1MOut: 0.2,
	},
	"moonshotai/Kimi-K2.5": {
		Tools:        true,
		Reasoning:    false,
		Vision:       true,
		CostPer1MIn:  0.5,
		CostPer1MOut: 2.6,
	},
	"kimi-k2.5-fast": {
		Tools:        true,
		Reasoning:    false,
		Vision:       true,
		CostPer1MIn:  0.5,
		CostPer1MOut: 2.6,
	},
	"MiniMaxAI/MiniMax-M2.5": {
		Tools:        true,
		Reasoning:    true,
		Vision:       false,
		CostPer1MIn:  0.3,
		CostPer1MOut: 1.4,
	},
	"Qwen/Qwen3.5-35B-A3B": {
		Tools:        true,
		Reasoning:    true,
		Vision:       false,
		CostPer1MIn:  0.3,
		CostPer1MOut: 1.1,
	},
	"Qwen/Qwen3.5-397B-A17B-FP8": {
		Tools:        true,
		Reasoning:    true,
		Vision:       false,
		CostPer1MIn:  0.7,
		CostPer1MOut: 4.1,
	},
	"qwen3.5-397b-fast": {
		Tools:        true,
		Reasoning:    false,
		Vision:       false,
		CostPer1MIn:  0.7,
		CostPer1MOut: 4.1,
	},
}

// modelNames provides display names for Neuralwatt-owned models that lack an
// org prefix and use lowercase IDs.
var modelNames = map[string]string{
	"glm-5.1-fast":      "GLM 5.1 Fast",
	"kimi-k2.5-fast":    "Kimi K2.5 Fast",
	"qwen3.5-397b-fast": "Qwen3.5 397B Fast",
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// modelDisplayName converts a model ID to a human-readable display name. For
// models with an org prefix (e.g. "zai-org/GLM-5-FP8"), the prefix is stripped.
// Neuralwatt-owned models without a prefix are looked up in modelNames for
// proper casing.
func modelDisplayName(id string) string {
	if name, ok := modelNames[id]; ok {
		return name
	}

	name := id
	if idx := strings.Index(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	name = strings.ReplaceAll(name, "-", " ")
	return name
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
		// Skip models with small context windows
		if model.MaxModelLen < 20000 {
			fmt.Printf("Skipping model %s: context %d < 20000\n",
				model.ID, model.MaxModelLen)
			continue
		}

		meta, ok := modelMetadata[model.ID]
		if !ok {
			fmt.Printf("Skipping unknown model %s (no metadata)\n", model.ID)
			continue
		}

		// Only include models that support tools
		if !meta.Tools {
			continue
		}

		var reasoningLevels []string
		var defaultReasoning string
		if meta.Reasoning {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   modelDisplayName(model.ID),
			CostPer1MIn:            roundCost(meta.CostPer1MIn),
			CostPer1MOut:           roundCost(meta.CostPer1MOut),
			CostPer1MInCached:      0, // Not available
			CostPer1MOutCached:     0, // Not available
			ContextWindow:          model.MaxModelLen,
			DefaultMaxTokens:       model.MaxModelLen / 10,
			CanReason:              meta.Reasoning,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
			SupportsImages:         meta.Vision,
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
