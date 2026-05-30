// Package main provides a command-line tool to generate Xiaomi MiMo provider
// configuration files.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"charm.land/catwalk/pkg/catwalk"
)

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

var reasoningLevels = []string{"low", "medium", "high"}

func payAsYouGoModels() []catwalk.Model {
	return []catwalk.Model{
		{
			ID:                     "mimo-v2.5-pro",
			Name:                   "MiMo V2.5 Pro",
			CostPer1MIn:            0.435,
			CostPer1MOut:           0.87,
			CostPer1MInCached:      0.0036,
			CostPer1MOutCached:     0,
			ContextWindow:          1048576,
			DefaultMaxTokens:       131072,
			CanReason:              true,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: "medium",
			SupportsImages:         false,
		},
		{
			ID:                     "mimo-v2.5",
			Name:                   "MiMo V2.5",
			CostPer1MIn:            0.14,
			CostPer1MOut:           0.28,
			CostPer1MInCached:      0.0028,
			CostPer1MOutCached:     0,
			ContextWindow:          1048576,
			DefaultMaxTokens:       131072,
			CanReason:              true,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: "medium",
			SupportsImages:         true,
		},
		{
			ID:                     "mimo-v2-flash",
			Name:                   "MiMo V2 Flash",
			CostPer1MIn:            0.1,
			CostPer1MOut:           0.3,
			CostPer1MInCached:      0.01,
			CostPer1MOutCached:     0,
			ContextWindow:          262144,
			DefaultMaxTokens:       65536,
			CanReason:              true,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: "medium",
			SupportsImages:         false,
		},
	}
}

func tokenPlanModels() []catwalk.Model {
	models := payAsYouGoModels()
	for i := range models {
		models[i].CostPer1MIn = 0
		models[i].CostPer1MOut = 0
		models[i].CostPer1MInCached = 0
		models[i].CostPer1MOutCached = 0
	}
	return models
}

func provider(config providerConfig) catwalk.Provider {
	models := payAsYouGoModels()
	if config.tokenPlan {
		models = tokenPlanModels()
	}

	return catwalk.Provider{
		Name:                config.name,
		ID:                  config.id,
		APIKey:              config.apiKey,
		APIEndpoint:         config.apiEndpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "mimo-v2.5-pro",
		DefaultSmallModelID: "mimo-v2-flash",
		Models:              models,
	}
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

	for _, providerFile := range providers {
		writeProvider(providerFile.path, provider(providerFile.config))
	}
}
