// Package main generates the Fire Pass provider configuration.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"charm.land/catwalk/pkg/catwalk"
)

func main() {
	provider := catwalk.Provider{
		Name:                "Fireworks (Firepass)",
		ID:                  catwalk.InferenceProviderFirePass,
		APIKey:              "$FIREPASS_API_KEY",
		APIEndpoint:         "https://api.fireworks.ai/inference/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/routers/kimi-k2p6-turbo",
		DefaultSmallModelID: "accounts/fireworks/routers/kimi-k2p6-turbo",
		Models: []catwalk.Model{
			{
				ID:               "accounts/fireworks/routers/kimi-k2p6-turbo",
				Name:             "Kimi K2.6 Turbo",
				CostPer1MIn:      0,
				CostPer1MOut:     0,
				CostPer1MInCached: 0,
				ContextWindow:    262000,
				DefaultMaxTokens: 262000,
				CanReason:        true,
				ReasoningLevels:  []string{"low", "medium", "high"},
				DefaultReasoningEffort: "medium",
				SupportsImages:   false,
			},
		},
	}

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/firepass.json", data, 0o600); err != nil {
		log.Fatal("Error writing provider config:", err)
	}

	fmt.Printf("Generated firepass.json with %d model\n", len(provider.Models))
}
