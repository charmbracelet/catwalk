// Package main provides a command-line tool to fetch models from Xiaomi MiMo
// and generate configuration files for the provider.
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

// ModelsResponse is the OpenAI-compatible /v1/models response.
type ModelsResponse struct {
	Data []MiMoModel `json:"data"`
}

// MiMoModel represents a single model entry in the /v1/models response.
type MiMoModel struct {
	ID string `json:"id"`
}

// modelSpec holds metadata for each known MiMo model. The /v1/models endpoint
// only returns model IDs, so pricing and capabilities are sourced from here.
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
// the API response and this map are included in the generated configs.
var knownModels = map[string]modelSpec{
	"mimo-v2.5-pro": {
		Name:              "MiMo V2.5 Pro",
		CostPer1MIn:       0.435,
		CostPer1MOut:      0.87,
		CostPer1MInCached: 0.0036,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    false,
	},
	"mimo-v2.5": {
		Name:              "MiMo V2.5",
		CostPer1MIn:       0.14,
		CostPer1MOut:      0.28,
		CostPer1MInCached: 0.0028,
		ContextWindow:     1_048_576,
		DefaultMaxTokens:  131_072,
		CanReason:         true,
		SupportsImages:    true,
	},
	"mimo-v2-flash": {
		Name:              "MiMo V2 Flash",
		CostPer1MIn:       0.1,
		CostPer1MOut:      0.3,
		CostPer1MInCached: 0.01,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
	},
}

type providerConfig struct {
	name        string
	id          catwalk.InferenceProvider
	apiKey      string
	apiEndpoint string
	tokenPlan   bool
}

type providerFile struct {
	path   string
	config providerConfig
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
	_ = os.WriteFile("tmp/xiaomi-mimo-response.json", body, 0o600)

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

func buildModels(apiModelIDs []string, zeroCost bool) []catwalk.Model {
	var models []catwalk.Model
	for _, id := range apiModelIDs {
		spec, ok := knownModels[id]
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

		if zeroCost {
			m.CostPer1MIn = 0
			m.CostPer1MOut = 0
			m.CostPer1MInCached = 0
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
	apiKey := os.Getenv("MIMO_API_KEY")
	if apiKey == "" {
		log.Fatal("MIMO_API_KEY environment variable is not set")
	}

	// Default to the pay-as-you-go endpoint; allow override (e.g. for token
	// plan keys that only work on regional endpoints).
	fetchEndpoint := os.Getenv("MIMO_API_ENDPOINT")
	if fetchEndpoint == "" {
		fetchEndpoint = "https://api.xiaomimimo.com/v1"
	}

	apiModelIDs, err := fetchModels(fetchEndpoint, apiKey)
	if err != nil {
		log.Fatal("Error fetching Xiaomi MiMo models:", err)
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

	providers := []providerFile{
		{
			path: "internal/providers/configs/xiaomi-mimo.json",
			config: providerConfig{
				name:        "Xiaomi MiMo",
				id:          catwalk.InferenceProviderXiaomiMiMo,
				apiKey:      "$MIMO_API_KEY",
				apiEndpoint: "https://api.xiaomimimo.com/v1",
			},
		},
		{
			path: "internal/providers/configs/xiaomi-mimo-token-plan-ams.json",
			config: providerConfig{
				name:        "Xiaomi MiMo Token Plan (Europe)",
				id:          catwalk.InferenceProviderXiaomiMiMoTokenPlanAMS,
				apiKey:      "$MIMO_TOKEN_PLAN_API_KEY",
				apiEndpoint: "https://token-plan-ams.xiaomimimo.com/v1",
				tokenPlan:   true,
			},
		},
		{
			path: "internal/providers/configs/xiaomi-mimo-token-plan-cn.json",
			config: providerConfig{
				name:        "Xiaomi MiMo Token Plan (China)",
				id:          catwalk.InferenceProviderXiaomiMiMoTokenPlanCN,
				apiKey:      "$MIMO_TOKEN_PLAN_API_KEY",
				apiEndpoint: "https://token-plan-cn.xiaomimimo.com/v1",
				tokenPlan:   true,
			},
		},
		{
			path: "internal/providers/configs/xiaomi-mimo-token-plan-sgp.json",
			config: providerConfig{
				name:        "Xiaomi MiMo Token Plan (Singapore)",
				id:          catwalk.InferenceProviderXiaomiMiMoTokenPlanSGP,
				apiKey:      "$MIMO_TOKEN_PLAN_API_KEY",
				apiEndpoint: "https://token-plan-sgp.xiaomimimo.com/v1",
				tokenPlan:   true,
			},
		},
	}

	for _, pf := range providers {
		models := buildModels(apiModelIDs, pf.config.tokenPlan)

		p := catwalk.Provider{
			Name:                pf.config.name,
			ID:                  pf.config.id,
			APIKey:              pf.config.apiKey,
			APIEndpoint:         pf.config.apiEndpoint,
			Type:                catwalk.TypeOpenAICompat,
			DefaultLargeModelID: "mimo-v2.5-pro",
			DefaultSmallModelID: "mimo-v2.5",
			Models:              models,
		}

		writeProvider(pf.path, p)
	}
}
