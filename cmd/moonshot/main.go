// Package main provides a command-line tool to fetch models from Moonshot.ai
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

// ModelsResponse is the OpenAI-compatible /v1/models response.
type ModelsResponse struct {
	Data []MoonshotModel `json:"data"`
}

// MoonshotModel represents a single model entry in the /v1/models response.
type MoonshotModel struct {
	ID                string `json:"id"`
	ContextLength     int64  `json:"context_length"`
	SupportsImageIn   bool   `json:"supports_image_in"`
	SupportsReasoning bool   `json:"supports_reasoning"`
}

// modelSpec holds pricing and metadata for each known Moonshot model. The API
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
	"kimi-k2.7-code": {
		Name:              "Kimi K2.7 Code",
		CostPer1MIn:       0.95,
		CostPer1MOut:      4.00,
		CostPer1MInCached: 0.19,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"kimi-k2.7-code-highspeed": {
		Name:              "Kimi K2.7 Code Highspeed",
		CostPer1MIn:       1.90,
		CostPer1MOut:      8.00,
		CostPer1MInCached: 0.38,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"kimi-k2.6": {
		Name:              "Kimi K2.6",
		CostPer1MIn:       0.95,
		CostPer1MOut:      4.00,
		CostPer1MInCached: 0.16,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"kimi-k2.5": {
		Name:              "Kimi K2.5",
		CostPer1MIn:       0.60,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0.10,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    true,
	},
	"moonshot-v1-8k": {
		Name:              "Moonshot V1 8K",
		CostPer1MIn:       0.20,
		CostPer1MOut:      2.00,
		CostPer1MInCached: 0,
		ContextWindow:     8_192,
		DefaultMaxTokens:  4_096,
		CanReason:         false,
		SupportsImages:    false,
	},
	"moonshot-v1-32k": {
		Name:              "Moonshot V1 32K",
		CostPer1MIn:       1.00,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0,
		ContextWindow:     32_768,
		DefaultMaxTokens:  8_192,
		CanReason:         false,
		SupportsImages:    false,
	},
	"moonshot-v1-128k": {
		Name:              "Moonshot V1 128K",
		CostPer1MIn:       2.00,
		CostPer1MOut:      5.00,
		CostPer1MInCached: 0,
		ContextWindow:     131_072,
		DefaultMaxTokens:  16_384,
		CanReason:         false,
		SupportsImages:    false,
	},
	"moonshot-v1-8k-vision-preview": {
		Name:              "Moonshot V1 8K Vision",
		CostPer1MIn:       0.20,
		CostPer1MOut:      2.00,
		CostPer1MInCached: 0,
		ContextWindow:     8_192,
		DefaultMaxTokens:  4_096,
		CanReason:         false,
		SupportsImages:    true,
	},
	"moonshot-v1-32k-vision-preview": {
		Name:              "Moonshot V1 32K Vision",
		CostPer1MIn:       1.00,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0,
		ContextWindow:     32_768,
		DefaultMaxTokens:  8_192,
		CanReason:         false,
		SupportsImages:    true,
	},
	"moonshot-v1-128k-vision-preview": {
		Name:              "Moonshot V1 128K Vision",
		CostPer1MIn:       2.00,
		CostPer1MOut:      5.00,
		CostPer1MInCached: 0,
		ContextWindow:     131_072,
		DefaultMaxTokens:  16_384,
		CanReason:         false,
		SupportsImages:    true,
	},
}

func fetchModels(endpoint, apiKey string) ([]MoonshotModel, error) {
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
	_ = os.WriteFile("tmp/moonshot-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}

	return mr.Data, nil
}

func buildModels(apiModels []MoonshotModel) []catwalk.Model {
	var models []catwalk.Model
	for _, am := range apiModels {
		spec, ok := knownModels[am.ID]
		if !ok {
			continue
		}

		var reasoningEffort string
		var levels []string
		canReason := spec.CanReason || am.SupportsReasoning
		if canReason {
			levels = reasoningLevels
			reasoningEffort = "medium"
		}

		ctxWindow := spec.ContextWindow
		if ctxWindow == 0 {
			ctxWindow = am.ContextLength
		}

		m := catwalk.Model{
			ID:                     am.ID,
			Name:                   spec.Name,
			CostPer1MIn:            spec.CostPer1MIn,
			CostPer1MOut:           spec.CostPer1MOut,
			CostPer1MInCached:      spec.CostPer1MInCached,
			CostPer1MOutCached:     0,
			ContextWindow:          ctxWindow,
			DefaultMaxTokens:       spec.DefaultMaxTokens,
			CanReason:              canReason,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: reasoningEffort,
			SupportsImages:         spec.SupportsImages || am.SupportsImageIn,
		}

		models = append(models, m)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	return models
}

func main() {
	apiKey := os.Getenv("MOONSHOT_API_KEY")
	if apiKey == "" {
		log.Fatal("MOONSHOT_API_KEY environment variable is not set")
	}

	endpoint := os.Getenv("MOONSHOT_API_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.moonshot.ai/v1"
	}

	apiModels, err := fetchModels(endpoint, apiKey)
	if err != nil {
		log.Fatal("Error fetching Moonshot models:", err)
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
		Name:                "Moonshot",
		ID:                  catwalk.InferenceProviderMoonshot,
		APIKey:              "$MOONSHOT_API_KEY",
		APIEndpoint:         endpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "kimi-k2.7-code",
		DefaultSmallModelID: "kimi-k2.5",
		Models:              models,
	}, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Moonshot provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/moonshot.json", data, 0o600); err != nil {
		log.Fatal("Error writing Moonshot provider config:", err)
	}

	fmt.Printf("Generated moonshot.json with %d models\n", len(models))
}
