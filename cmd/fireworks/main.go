// Package main provides a command-line tool to fetch models from Fireworks.ai
// and generate configuration files for the provider and its Firepass variant.
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

// ModelsResponse is the OpenAI-compatible /inference/v1/models response.
type ModelsResponse struct {
	Data []FireworksModel `json:"data"`
}

// FireworksModel represents a single model entry in the /v1/models response.
type FireworksModel struct {
	ID string `json:"id"`
}

// modelSpec holds metadata for each known Fireworks model. The API only returns
// model IDs, so pricing and capabilities are sourced from here.
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

// knownModels maps short model IDs to their metadata. Only models present in
// both the API response and this map are included in the generated configs.
var knownModels = map[string]modelSpec{
	"accounts/fireworks/models/kimi-k2p7-code": {
		Name:              "Kimi K2.7 Code",
		CostPer1MIn:       0.95,
		CostPer1MOut:      4.00,
		CostPer1MInCached: 0.19,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"accounts/fireworks/models/kimi-k2p6": {
		Name:              "Kimi K2.6",
		CostPer1MIn:       0.95,
		CostPer1MOut:      4.00,
		CostPer1MInCached: 0.16,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"accounts/fireworks/models/deepseek-v4-pro": {
		Name:              "DeepSeek V4 Pro",
		CostPer1MIn:       1.74,
		CostPer1MOut:      3.48,
		CostPer1MInCached: 0.145,
		ContextWindow:     1_040_000,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/deepseek-v4-flash": {
		Name:              "DeepSeek V4 Flash",
		CostPer1MIn:       0.14,
		CostPer1MOut:      0.28,
		CostPer1MInCached: 0.028,
		ContextWindow:     1_040_000,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/glm-5p2": {
		Name:              "GLM 5.2",
		CostPer1MIn:       1.40,
		CostPer1MOut:      4.40,
		CostPer1MInCached: 0.26,
		ContextWindow:     1_040_000,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/glm-5p1": {
		Name:              "GLM 5.1",
		CostPer1MIn:       1.40,
		CostPer1MOut:      4.40,
		CostPer1MInCached: 0.26,
		ContextWindow:     202_000,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/qwen3p7-plus": {
		Name:              "Qwen 3.7 Plus",
		CostPer1MIn:       0.40,
		CostPer1MOut:      1.60,
		CostPer1MInCached: 0.08,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"accounts/fireworks/models/minimax-m3": {
		Name:              "MiniMax M3",
		CostPer1MIn:       0.30,
		CostPer1MOut:      1.20,
		CostPer1MInCached: 0.06,
		ContextWindow:     512_000,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    true,
	},
	"accounts/fireworks/models/minimax-m2p7": {
		Name:              "MiniMax M2.7",
		CostPer1MIn:       0.30,
		CostPer1MOut:      1.20,
		CostPer1MInCached: 0.06,
		ContextWindow:     196_000,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/gpt-oss-120b": {
		Name:              "GPT OSS 120B",
		CostPer1MIn:       0.15,
		CostPer1MOut:      0.60,
		CostPer1MInCached: 0.015,
		ContextWindow:     131_072,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/gpt-oss-20b": {
		Name:              "GPT OSS 20B",
		CostPer1MIn:       0.07,
		CostPer1MOut:      0.30,
		CostPer1MInCached: 0.035,
		ContextWindow:     131_072,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
	"accounts/fireworks/models/nemotron-3-ultra-nvfp4": {
		Name:              "NVIDIA Nemotron 3 Ultra",
		CostPer1MIn:       0.60,
		CostPer1MOut:      2.40,
		CostPer1MInCached: 0.12,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
}

// firepassModels lists models available via Firepass (zero-cost subscription).
var firepassModels = map[string]modelSpec{
	"accounts/fireworks/routers/kimi-k2p6-turbo": {
		Name:             "Kimi K2.6 Turbo",
		ContextWindow:    262_144,
		DefaultMaxTokens: 65_536,
		CanReason:        true,
		SupportsImages:   true,
	},
}

func fetchModels(endpoint, apiKey string) ([]string, error) {
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
	_ = os.WriteFile("tmp/fireworks-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}

	ids := make([]string, len(mr.Data))
	for i, m := range mr.Data {
		ids[i] = m.ID
	}
	return ids, nil
}

func buildModels(apiModelIDs []string, specs map[string]modelSpec) []catwalk.Model {
	var models []catwalk.Model
	for _, id := range apiModelIDs {
		spec, ok := specs[id]
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
			ID:                     id,
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

func writeProvider(path string, provider catwalk.Provider) {
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatalf("Error marshaling %s: %v", provider.Name, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatalf("Error writing %s: %v", path, err)
	}

	fmt.Printf("Generated %s with %d models\n", path, len(provider.Models))
}

func main() {
	apiKey := os.Getenv("FIREWORKS_API_KEY")
	if apiKey == "" {
		log.Fatal("FIREWORKS_API_KEY environment variable is not set")
	}

	fetchEndpoint := os.Getenv("FIREWORKS_API_ENDPOINT")
	if fetchEndpoint == "" {
		fetchEndpoint = "https://api.fireworks.ai/inference/v1"
	}

	apiModelIDs, err := fetchModels(fetchEndpoint, apiKey)
	if err != nil {
		log.Fatal("Error fetching Fireworks models:", err)
	}

	knownCount := 0
	for _, id := range apiModelIDs {
		if _, ok := knownModels[id]; ok {
			knownCount++
		} else {
			log.Printf("Warning: model %q found in API but not in known models map; skipping", id)
		}
	}
	if knownCount == 0 {
		log.Fatal("No known models found in API response")
	}

	// Fireworks pay-as-you-go provider
	fireworksModels := buildModels(apiModelIDs, knownModels)
	writeProvider("internal/providers/configs/fireworks.json", catwalk.Provider{
		Name:                "Fireworks",
		ID:                  catwalk.InferenceProviderFireworks,
		APIKey:              "$FIREWORKS_API_KEY",
		APIEndpoint:         "https://api.fireworks.ai/inference/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/models/deepseek-v4-pro",
		DefaultSmallModelID: "accounts/fireworks/models/deepseek-v4-flash",
		Models:              fireworksModels,
	})

	// Firepass provider — Kimi K2.6 Turbo with zero cost
	var firepassModelList []catwalk.Model
	for id, spec := range firepassModels {
		firepassModelList = append(firepassModelList, catwalk.Model{
			ID:                     id,
			Name:                   spec.Name,
			CostPer1MIn:            0,
			CostPer1MOut:           0,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			ContextWindow:          spec.ContextWindow,
			DefaultMaxTokens:       spec.DefaultMaxTokens,
			CanReason:              spec.CanReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: "medium",
			SupportsImages:         spec.SupportsImages,
		})
	}
	slices.SortFunc(firepassModelList, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})
	writeProvider("internal/providers/configs/firepass.json", catwalk.Provider{
		Name:                "Fireworks Firepass",
		ID:                  catwalk.InferenceProviderFirepass,
		APIKey:              "$FIREPASS_API_KEY",
		APIEndpoint:         "https://api.fireworks.ai/inference/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/routers/kimi-k2p6-turbo",
		DefaultSmallModelID: "accounts/fireworks/routers/kimi-k2p6-turbo",
		Models:              firepassModelList,
	})
}
