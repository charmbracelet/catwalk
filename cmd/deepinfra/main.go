// Package main provides a command-line tool to fetch models from DeepInfra
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

// ModelsResponse is the OpenAI-compatible /v1/openai/models response.
type ModelsResponse struct {
	Data []DeepInfraModel `json:"data"`
}

// DeepInfraModel represents a single model entry in the /models response.
type DeepInfraModel struct {
	ID string `json:"id"`
}

// modelSpec holds pricing and metadata for each known DeepInfra model. The API
// does not return pricing, so it is sourced from here.
type modelSpec struct {
	Name              string
	CostPer1MIn       float64
	CostPer1MOut      float64
	CostPer1MInCached float64
	ContextWindow     int64
	DefaultMaxTokens  int64
	CanReason         bool
	SupportsImages    bool
}

var reasoningLevels = []string{"low", "medium", "high"}

// knownModels maps model IDs to their metadata. Only models present in both
// the API response and this map are included in the generated config.
var knownModels = map[string]modelSpec{
	"deepseek-ai/DeepSeek-V4-Pro": {
		Name:              "DeepSeek V4 Pro",
		CostPer1MIn:       1.30,
		CostPer1MOut:      2.60,
		CostPer1MInCached: 0.10,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  262_144,
		CanReason:         true,
		SupportsImages:    false,
	},
	"deepseek-ai/DeepSeek-V4-Flash": {
		Name:              "DeepSeek V4 Flash",
		CostPer1MIn:       0.10,
		CostPer1MOut:      0.20,
		CostPer1MInCached: 0.02,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  262_144,
		CanReason:         true,
		SupportsImages:    false,
	},
	"deepseek-ai/DeepSeek-V3.2": {
		Name:              "DeepSeek V3.2",
		CostPer1MIn:       0.26,
		CostPer1MOut:      0.38,
		CostPer1MInCached: 0.13,
		ContextWindow:     163_840,
		DefaultMaxTokens:  40_960,
		CanReason:         true,
		SupportsImages:    false,
	},
	"deepseek-ai/DeepSeek-R1-0528": {
		Name:              "DeepSeek R1 0528",
		CostPer1MIn:       0.50,
		CostPer1MOut:      2.15,
		CostPer1MInCached: 0.35,
		ContextWindow:     163_840,
		DefaultMaxTokens:  40_960,
		CanReason:         true,
		SupportsImages:    false,
	},
	"zai-org/GLM-5.2": {
		Name:              "GLM 5.2",
		CostPer1MIn:       1.20,
		CostPer1MOut:      4.20,
		CostPer1MInCached: 0.20,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  262_144,
		CanReason:         true,
		SupportsImages:    false,
	},
	"zai-org/GLM-5.1": {
		Name:              "GLM 5.1",
		CostPer1MIn:       1.05,
		CostPer1MOut:      3.50,
		CostPer1MInCached: 0.205,
		ContextWindow:     198_000,
		DefaultMaxTokens:  49_152,
		CanReason:         true,
		SupportsImages:    false,
	},
	"zai-org/GLM-4.7-Flash": {
		Name:              "GLM 4.7 Flash",
		CostPer1MIn:       0.06,
		CostPer1MOut:      0.40,
		CostPer1MInCached: 0.01,
		ContextWindow:     198_000,
		DefaultMaxTokens:  49_152,
		CanReason:         true,
		SupportsImages:    false,
	},
	"moonshotai/Kimi-K2.7-Code": {
		Name:              "Kimi K2.7 Code",
		CostPer1MIn:       0.74,
		CostPer1MOut:      3.50,
		CostPer1MInCached: 0.15,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"moonshotai/Kimi-K2.6": {
		Name:              "Kimi K2.6",
		CostPer1MIn:       0.75,
		CostPer1MOut:      3.50,
		CostPer1MInCached: 0.15,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"moonshotai/Kimi-K2.5": {
		Name:              "Kimi K2.5",
		CostPer1MIn:       0.45,
		CostPer1MOut:      2.25,
		CostPer1MInCached: 0.07,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"Qwen/Qwen3.7-Max": {
		Name:              "Qwen 3.7 Max",
		CostPer1MIn:       2.50,
		CostPer1MOut:      7.50,
		CostPer1MInCached: 0.50,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"Qwen/Qwen3.5-397B-A17B": {
		Name:              "Qwen 3.5 397B A17B",
		CostPer1MIn:       0.45,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0.22,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo": {
		Name:              "Qwen3 Coder 480B Turbo",
		CostPer1MIn:       0.30,
		CostPer1MOut:      1.00,
		CostPer1MInCached: 0.10,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         false,
		SupportsImages:    false,
	},
	"Qwen/Qwen3-235B-A22B-Thinking-2507": {
		Name:              "Qwen3 235B Thinking",
		CostPer1MIn:       0.23,
		CostPer1MOut:      2.30,
		CostPer1MInCached: 0.20,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"Qwen/Qwen3-235B-A22B-Instruct-2507": {
		Name:              "Qwen3 235B Instruct",
		CostPer1MIn:       0.09,
		CostPer1MOut:      0.10,
		CostPer1MInCached: 0,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         false,
		SupportsImages:    false,
	},
	"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8": {
		Name:              "Llama 4 Maverick",
		CostPer1MIn:       0.15,
		CostPer1MOut:      0.60,
		CostPer1MInCached: 0,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  262_144,
		CanReason:         false,
		SupportsImages:    true,
	},
	"meta-llama/Llama-4-Scout-17B-16E-Instruct": {
		Name:              "Llama 4 Scout",
		CostPer1MIn:       0.10,
		CostPer1MOut:      0.30,
		CostPer1MInCached: 0,
		ContextWindow:     327_680,
		DefaultMaxTokens:  81_920,
		CanReason:         false,
		SupportsImages:    true,
	},
	"meta-llama/Llama-3.3-70B-Instruct-Turbo": {
		Name:              "Llama 3.3 70B Turbo",
		CostPer1MIn:       0.10,
		CostPer1MOut:      0.32,
		CostPer1MInCached: 0,
		ContextWindow:     131_072,
		DefaultMaxTokens:  32_768,
		CanReason:         false,
		SupportsImages:    false,
	},
	"nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B": {
		Name:              "NVIDIA Nemotron 3 Ultra",
		CostPer1MIn:       0.50,
		CostPer1MOut:      2.20,
		CostPer1MInCached: 0.10,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"XiaomiMiMo/MiMo-V2.5-Pro": {
		Name:              "MiMo V2.5 Pro",
		CostPer1MIn:       1.00,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0.20,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  262_144,
		CanReason:         true,
		SupportsImages:    false,
	},
	"XiaomiMiMo/MiMo-V2.5": {
		Name:              "MiMo V2.5",
		CostPer1MIn:       0.40,
		CostPer1MOut:      2.00,
		CostPer1MInCached: 0.08,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
}

func fetchModels(endpoint, apiKey string) ([]DeepInfraModel, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		endpoint+"/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/deepinfra-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}

	return mr.Data, nil
}

func buildModels(apiModels []DeepInfraModel) []catwalk.Model {
	var models []catwalk.Model
	for _, am := range apiModels {
		spec, ok := knownModels[am.ID]
		if !ok {
			continue
		}

		var reasoningEffort string
		var levels []string
		if spec.CanReason {
			levels = reasoningLevels
			reasoningEffort = "medium"
		}

		m := catwalk.Model{
			ID:                     am.ID,
			Name:                   spec.Name,
			CostPer1MIn:            spec.CostPer1MIn,
			CostPer1MOut:           spec.CostPer1MOut,
			CostPer1MInCached:      spec.CostPer1MInCached,
			CostPer1MOutCached:     0,
			ContextWindow:          spec.ContextWindow,
			DefaultMaxTokens:       spec.DefaultMaxTokens,
			CanReason:              spec.CanReason,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: reasoningEffort,
			SupportsImages:         spec.SupportsImages,
		}

		models = append(models, m)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	return models
}

func main() {
	apiKey := os.Getenv("DEEPINFRA_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPINFRA_API_KEY environment variable is not set")
	}

	endpoint := os.Getenv("DEEPINFRA_API_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.deepinfra.com/v1/openai"
	}

	apiModels, err := fetchModels(endpoint, apiKey)
	if err != nil {
		log.Fatal("Error fetching DeepInfra models:", err)
	}

	knownCount := 0
	for _, m := range apiModels {
		if _, ok := knownModels[m.ID]; ok {
			knownCount++
		} else {
			log.Printf("Warning: model %q found in API but not in known models map; skipping", m.ID)
		}
	}
	if knownCount == 0 {
		log.Fatal("No known models found in API response")
	}

	models := buildModels(apiModels)

	data, err := json.MarshalIndent(catwalk.Provider{
		Name:                "DeepInfra",
		ID:                  catwalk.InferenceProviderDeepInfra,
		APIKey:              "$DEEPINFRA_API_KEY",
		APIEndpoint:         endpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek-ai/DeepSeek-V4-Pro",
		DefaultSmallModelID: "deepseek-ai/DeepSeek-V4-Flash",
		Models:              models,
	}, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling DeepInfra provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/deepinfra.json", data, 0o600); err != nil {
		log.Fatal("Error writing DeepInfra provider config:", err)
	}

	fmt.Printf("Generated deepinfra.json with %d models\n", len(models))
}
