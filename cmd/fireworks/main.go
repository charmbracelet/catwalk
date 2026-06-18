// Package main provides a command-line tool to fetch models from Fireworks.ai
// and generate configuration files for the provider and its Firepass variant.
package main

import (
	"cmp"
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
	ID                 string `json:"id"`
	SupportsChat       bool   `json:"supports_chat"`
	SupportsImageInput bool   `json:"supports_image_input"`
	SupportsTools      bool   `json:"supports_tools"`
	ContextLength      int64  `json:"context_length"`
}

// modelSpec holds metadata for each known Fireworks model. The API does not
// return pricing, so pricing and capabilities are sourced from here.
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

// knownModels maps model IDs to their metadata. Only models present in
// both the API response and this map are included in fireworks.json.
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
	"accounts/fireworks/models/kimi-k2p5": {
		Name:              "Kimi K2.5",
		CostPer1MIn:       0.60,
		CostPer1MOut:      3.00,
		CostPer1MInCached: 0.10,
		ContextWindow:     262_144,
		DefaultMaxTokens:  65_536,
		CanReason:         true,
		SupportsImages:    false,
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

func fetchModels(endpoint, apiKey string) ([]FireworksModel, error) {
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
	return mr.Data, nil
}

// buildModels builds the pay-as-you-go Fireworks models from the API response,
// enriching them with pricing from the knownModels map.
func buildModels(fireworksModels []FireworksModel) []catwalk.Model {
	var models []catwalk.Model
	for _, m := range fireworksModels {
		if !m.SupportsChat || !m.SupportsTools {
			continue
		}

		spec, ok := knownModels[m.ID]
		if !ok {
			continue
		}

		var reasoningEffort string
		var levels []string
		if spec.CanReason {
			levels = reasoningLevels
			reasoningEffort = "medium"
		}

		ctxWindow := cmp.Or(spec.ContextWindow, m.ContextLength)

		m := catwalk.Model{
			ID:                     m.ID,
			Name:                   spec.Name,
			CostPer1MIn:            spec.CostPer1MIn,
			CostPer1MOut:           spec.CostPer1MOut,
			CostPer1MInCached:      spec.CostPer1MInCached,
			CostPer1MOutCached:     0,
			ContextWindow:          ctxWindow,
			DefaultMaxTokens:       spec.DefaultMaxTokens,
			CanReason:              spec.CanReason,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: reasoningEffort,
			SupportsImages:         spec.SupportsImages || m.SupportsImageInput,
		}

		models = append(models, m)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	return models
}

// buildFirepassModels builds Firepass (subscription) models purely from the API
// response. All costs are zero since Firepass is a flat-rate subscription.
func buildFirepassModels(fireworksModels []FireworksModel) []catwalk.Model {
	var models []catwalk.Model
	for _, am := range fireworksModels {
		ctxWindow := cmp.Or(am.ContextLength, 262_144)
		defaultMaxTokens := ctxWindow / 4

		m := catwalk.Model{
			ID:                     am.ID,
			Name:                   prettyName(am.ID),
			CostPer1MIn:            0,
			CostPer1MOut:           0,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			ContextWindow:          ctxWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              true,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: "medium",
			SupportsImages:         am.SupportsImageInput,
		}
		models = append(models, m)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	return models
}

// prettyName converts a router model ID into a human-readable name.
func prettyName(id string) string {
	parts := strings.Split(id, "/")
	short := parts[len(parts)-1]
	short = strings.ReplaceAll(short, "-", " ")
	short = strings.ReplaceAll(short, "p", ".")
	// Capitalize each word
	var result strings.Builder
	capitalize := true
	for _, r := range short {
		if capitalize && r >= 'a' && r <= 'z' {
			result.WriteRune(r - 32)
			capitalize = false
		} else if r == ' ' {
			result.WriteRune(r)
			capitalize = true
		} else {
			result.WriteRune(r)
			capitalize = false
		}
	}
	return result.String()
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
	fireworksKey := os.Getenv("FIREWORKS_API_KEY")
	if fireworksKey == "" {
		log.Fatal("FIREWORKS_API_KEY environment variable is not set")
	}

	endpoint := cmp.Or(os.Getenv("FIREWORKS_API_ENDPOINT"), "https://api.fireworks.ai/inference/v1")

	// Fetch Fireworks pay-as-you-go models
	apiModels, err := fetchModels(endpoint, fireworksKey)
	if err != nil {
		log.Fatal("Error fetching Fireworks models:", err)
	}

	var fireworksAPIModels []FireworksModel
	for _, am := range apiModels {
		if !strings.HasPrefix(am.ID, "accounts/fireworks/routers/") {
			fireworksAPIModels = append(fireworksAPIModels, am)
		}
	}

	knownCount := 0
	for _, am := range fireworksAPIModels {
		if _, ok := knownModels[am.ID]; ok {
			knownCount++
		} else {
			log.Printf("Warning: model %q found in API but not in known models map; skipping", am.ID)
		}
	}
	if knownCount == 0 {
		log.Fatal("No known models found in API response")
	}

	fireworksModels := buildModels(fireworksAPIModels)
	writeProvider("internal/providers/configs/fireworks.json", catwalk.Provider{
		Name:                "Fireworks",
		ID:                  catwalk.InferenceProviderFireworks,
		APIKey:              "$FIREWORKS_API_KEY",
		APIEndpoint:         endpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/models/kimi-k2p6",
		DefaultSmallModelID: "accounts/fireworks/models/gpt-oss-120b",
		Models:              fireworksModels,
	})

	// Fetch Firepass models with a separate API key (optional)
	firepassKey := os.Getenv("FIREPASS_API_KEY")
	if firepassKey == "" {
		log.Println("FIREPASS_API_KEY not set; skipping Firepass config generation")
		return
	}

	firepassAPIModels, err := fetchModels(endpoint, firepassKey)
	if err != nil {
		log.Fatal("Error fetching Firepass models:", err)
	}

	var firepassRouterModels []FireworksModel
	for _, am := range firepassAPIModels {
		if strings.HasPrefix(am.ID, "accounts/fireworks/routers/") {
			firepassRouterModels = append(firepassRouterModels, am)
		}
	}

	if len(firepassRouterModels) == 0 {
		log.Println("No Firepass router models found in API response; skipping Firepass config")
		return
	}

	firepassModels := buildFirepassModels(firepassRouterModels)
	defaultFirepassModel := firepassModels[0].ID
	writeProvider("internal/providers/configs/fireworks-firepass.json", catwalk.Provider{
		Name:                "Fireworks Firepass",
		ID:                  catwalk.InferenceProviderFireworksFirepass,
		APIKey:              "$FIREPASS_API_KEY",
		APIEndpoint:         endpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: defaultFirepassModel,
		DefaultSmallModelID: defaultFirepassModel,
		Models:              firepassModels,
	})
}
