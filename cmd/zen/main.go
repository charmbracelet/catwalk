// Package main provides a command-line tool to fetch models from Zen
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

// Model represents a model from the Zen API (OpenAI-compatible format).
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse is the response structure for the Zen models API.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ModelDetails contains the hardcoded details for each Zen model.
type ModelDetails struct {
	Name               string  `json:"name,omitempty"`
	CostPer1MIn        float64 `json:"cost_per_1m_in,omitempty"`
	CostPer1MOut       float64 `json:"cost_per_1m_out,omitempty"`
	CostPer1MInCached  float64 `json:"cost_per_1m_in_cached,omitempty"`
	CostPer1MOutCached float64 `json:"cost_per_1m_out_cached,omitempty"`
	ContextWindow      int64   `json:"context_window,omitempty"`
	DefaultMaxTokens   int64   `json:"default_max_tokens,omitempty"`
	CanReason          bool    `json:"can_reason,omitempty"`
	SupportsImages     bool    `json:"supports_images,omitempty"`
}

// ZenModelDetails contains the known details for Zen models from documentation.
var ZenModelDetails = map[string]ModelDetails{
	"gpt-5.2": {
		Name:               "GPT 5.2",
		CostPer1MIn:        1.75,
		CostPer1MOut:       14.0,
		CostPer1MInCached:  0.175,
		CostPer1MOutCached: 0.175,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5.2-codex": {
		Name:               "GPT 5.2 Codex",
		CostPer1MIn:        1.75,
		CostPer1MOut:       14.0,
		CostPer1MInCached:  0.175,
		CostPer1MOutCached: 0.175,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5.1": {
		Name:               "GPT 5.1",
		CostPer1MIn:        1.07,
		CostPer1MOut:       8.5,
		CostPer1MInCached:  0.107,
		CostPer1MOutCached: 0.107,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5.1-codex": {
		Name:               "GPT 5.1 Codex",
		CostPer1MIn:        1.07,
		CostPer1MOut:       8.5,
		CostPer1MInCached:  0.107,
		CostPer1MOutCached: 0.107,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5.1-codex-max": {
		Name:               "GPT 5.1 Codex Max",
		CostPer1MIn:        1.25,
		CostPer1MOut:       10.0,
		CostPer1MInCached:  0.125,
		CostPer1MOutCached: 0.125,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5.1-codex-mini": {
		Name:               "GPT 5.1 Codex Mini",
		CostPer1MIn:        0.25,
		CostPer1MOut:       2.0,
		CostPer1MInCached:  0.025,
		CostPer1MOutCached: 0.025,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5": {
		Name:               "GPT 5",
		CostPer1MIn:        1.07,
		CostPer1MOut:       8.5,
		CostPer1MInCached:  0.107,
		CostPer1MOutCached: 0.107,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5-codex": {
		Name:               "GPT 5 Codex",
		CostPer1MIn:        1.07,
		CostPer1MOut:       8.5,
		CostPer1MInCached:  0.107,
		CostPer1MOutCached: 0.107,
		ContextWindow:      400000,
		DefaultMaxTokens:   128000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gpt-5-nano": {
		Name:               "GPT 5 Nano",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      128000,
		DefaultMaxTokens:   16384,
		CanReason:          false,
		SupportsImages:     false,
	},
	"claude-sonnet-4-5": {
		Name:               "Claude Sonnet 4.5",
		CostPer1MIn:        3.0,
		CostPer1MOut:       15.0,
		CostPer1MInCached:  0.3,
		CostPer1MOutCached: 3.75,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"claude-sonnet-4": {
		Name:               "Claude Sonnet 4",
		CostPer1MIn:        3.0,
		CostPer1MOut:       15.0,
		CostPer1MInCached:  0.3,
		CostPer1MOutCached: 3.75,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"claude-haiku-4-5": {
		Name:               "Claude Haiku 4.5",
		CostPer1MIn:        1.0,
		CostPer1MOut:       5.0,
		CostPer1MInCached:  0.1,
		CostPer1MOutCached: 1.25,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          false,
		SupportsImages:     true,
	},
	"claude-3-5-haiku": {
		Name:               "Claude Haiku 3.5",
		CostPer1MIn:        0.8,
		CostPer1MOut:       4.0,
		CostPer1MInCached:  0.08,
		CostPer1MOutCached: 1.0,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          false,
		SupportsImages:     true,
	},
	"claude-opus-4-5": {
		Name:               "Claude Opus 4.5",
		CostPer1MIn:        5.0,
		CostPer1MOut:       25.0,
		CostPer1MInCached:  0.5,
		CostPer1MOutCached: 6.25,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"claude-opus-4-1": {
		Name:               "Claude Opus 4.1",
		CostPer1MIn:        15.0,
		CostPer1MOut:       75.0,
		CostPer1MInCached:  1.5,
		CostPer1MOutCached: 18.75,
		ContextWindow:      200000,
		DefaultMaxTokens:   64000,
		CanReason:          true,
		SupportsImages:     true,
	},
	"gemini-3-pro": {
		Name:               "Gemini 3 Pro",
		CostPer1MIn:        2.0,
		CostPer1MOut:       12.0,
		CostPer1MInCached:  0.2,
		CostPer1MOutCached: 0,
		ContextWindow:      1000000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"gemini-3-flash": {
		Name:               "Gemini 3 Flash",
		CostPer1MIn:        0.5,
		CostPer1MOut:       3.0,
		CostPer1MInCached:  0.05,
		CostPer1MOutCached: 0,
		ContextWindow:      1000000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"minimax-m2.1": {
		Name:               "MiniMax M2.1",
		CostPer1MIn:        0.3,
		CostPer1MOut:       1.2,
		CostPer1MInCached:  0.1,
		CostPer1MOutCached: 0,
		ContextWindow:      1000000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"minimax-m2.1-free": {
		Name:               "MiniMax M2.1 Free",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      1000000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"glm-4.7": {
		Name:               "GLM 4.7",
		CostPer1MIn:        0.6,
		CostPer1MOut:       2.2,
		CostPer1MInCached:  0.1,
		CostPer1MOutCached: 0,
		ContextWindow:      128000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"glm-4.7-free": {
		Name:               "GLM 4.7 Free",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      128000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"glm-4.6": {
		Name:               "GLM 4.6",
		CostPer1MIn:        0.6,
		CostPer1MOut:       2.2,
		CostPer1MInCached:  0.1,
		CostPer1MOutCached: 0,
		ContextWindow:      128000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"kimi-k2.5": {
		Name:               "Kimi K2.5",
		CostPer1MIn:        0.6,
		CostPer1MOut:       3.0,
		CostPer1MInCached:  0.08,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"kimi-k2.5-free": {
		Name:               "Kimi K2.5 Free",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"kimi-k2-thinking": {
		Name:               "Kimi K2 Thinking",
		CostPer1MIn:        0.4,
		CostPer1MOut:       2.5,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          true,
		SupportsImages:     true,
	},
	"kimi-k2": {
		Name:               "Kimi K2",
		CostPer1MIn:        0.4,
		CostPer1MOut:       2.5,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"qwen3-coder": {
		Name:               "Qwen3 Coder 480B",
		CostPer1MIn:        0.45,
		CostPer1MOut:       1.5,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"big-pickle": {
		Name:               "Big Pickle",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
	"trinity-large-preview-free": {
		Name:               "Trinity Large Preview Free",
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      256000,
		DefaultMaxTokens:   8192,
		CanReason:          false,
		SupportsImages:     true,
	},
}

func fetchZenModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://opencode.ai/zen/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

// This is used to generate the zen.json config file.
func main() {
	modelsResp, err := fetchZenModels()
	if err != nil {
		log.Fatal("Error fetching Zen models:", err)
	}

	zenProvider := catwalk.Provider{
		Name:                "Zen",
		ID:                  "zen",
		APIKey:              "$OPENCODE_API_KEY",
		APIEndpoint:         "https://opencode.ai/zen/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-4-5",
		DefaultSmallModelID: "gpt-5-nano",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		details, exists := ZenModelDetails[model.ID]
		if !exists {
			fmt.Printf("Skipping unknown model: %s\n", model.ID)
			continue
		}

		// Skip models with small context windows
		if details.ContextWindow < 20000 {
			continue
		}

		// Set reasoning levels for models that support reasoning
		var reasoningLevels []string
		var defaultReasoning string
		if details.CanReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   details.Name,
			CostPer1MIn:            details.CostPer1MIn,
			CostPer1MOut:           details.CostPer1MOut,
			CostPer1MInCached:      details.CostPer1MInCached,
			CostPer1MOutCached:     details.CostPer1MOutCached,
			ContextWindow:          details.ContextWindow,
			DefaultMaxTokens:       details.DefaultMaxTokens,
			CanReason:              details.CanReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         details.SupportsImages,
		}

		zenProvider.Models = append(zenProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, details.ContextWindow)
	}

	slices.SortFunc(zenProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/zen.json
	data, err := json.MarshalIndent(zenProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Zen provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/zen.json", data, 0o600); err != nil {
		log.Fatal("Error writing Zen provider config:", err)
	}

	fmt.Printf("Generated zen.json with %d models\n", len(zenProvider.Models))
}
