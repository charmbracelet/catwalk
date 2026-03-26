// Package main provides a command-line tool to fetch models from io.net
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
	xstrings "github.com/charmbracelet/x/exp/strings"
)

// Model represents a model from the io.net API.
type Model struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	ContextWindow        int     `json:"context_window"`
	SupportsImagesInput  bool    `json:"supports_images_input"`
	InputTokenPrice      float64 `json:"input_token_price"`
	OutputTokenPrice     float64 `json:"output_token_price"`
	CacheWriteTokenPrice float64 `json:"cache_write_token_price"`
	CacheReadTokenPrice  float64 `json:"cache_read_token_price"`
}

// Response is the response structure for the io.net models API.
type Response struct {
	Data []Model `json:"data"`
}

// This is used to generate the ionet.json config file.
func main() {
	provider := catwalk.Provider{
		Name:                "io.net",
		ID:                  "ionet",
		APIKey:              "$IONET_API_KEY",
		APIEndpoint:         "https://api.intelligence.io.solutions/api/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "moonshotai/Kimi-K2.5",
		DefaultSmallModelID: "zai-org/GLM-4.7-Flash",
	}

	resp, err := fetchModels(provider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching io.net models:", err)
	}

	provider.Models = make([]catwalk.Model, 0, len(resp.Data))

	modelIDSet := make(map[string]struct{})

	for _, model := range resp.Data {
		// Avoid duplicate entries
		if _, ok := modelIDSet[model.ID]; ok {
			continue
		}
		modelIDSet[model.ID] = struct{}{}

		if model.ContextWindow < 20000 {
			continue
		}
		if !supportsTools(model.ID) {
			continue
		}

		canReason := isReasoningModel(model.ID)
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// Convert token prices (per token) to cost per 1M tokens
		roundCost := func(v float64) float64 { return math.Round(v*1e5) / 1e5 }
		costPer1MIn := roundCost(model.InputTokenPrice * 1_000_000)
		costPer1MOut := roundCost(model.OutputTokenPrice * 1_000_000)
		costPer1MInCached := roundCost(model.CacheReadTokenPrice * 1_000_000)
		costPer1MOutCached := roundCost(model.CacheWriteTokenPrice * 1_000_000)

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      costPer1MInCached,
			CostPer1MOutCached:     costPer1MOutCached,
			ContextWindow:          int64(model.ContextWindow),
			DefaultMaxTokens:       int64(model.ContextWindow) / 10,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         model.SupportsImagesInput,
		}

		provider.Models = append(provider.Models, m)
		fmt.Printf("Added model %s with context window %d\n", model.ID, model.ContextWindow)
	}

	slices.SortFunc(provider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/ionet.json
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling io.net provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/ionet.json", data, 0o600); err != nil {
		log.Fatal("Error writing io.net provider config:", err)
	}

	fmt.Printf("Generated ionet.json with %d models\n", len(provider.Models))
}

func fetchModels(apiEndpoint string) (*Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("User-Agent", "Charm-Catwalk/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)

	// for debugging
	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/io-net-response.json", body, 0o600)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr Response
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("unable to unmarshal json: %w", err)
	}
	return &mr, nil
}

// isReasoningModel checks if the model ID indicates reasoning capability.
func isReasoningModel(modelID string) bool {
	return xstrings.ContainsAnyOf(
		strings.ToLower(modelID),
		"-thinking",
		"deepseek",
		"glm",
		"gpt-oss",
		"llama",
	)
}

// supportsTools determines if a model supports tool calling based on its ID.
func supportsTools(modelID string) bool {
	return !xstrings.ContainsAnyOf(
		strings.ToLower(modelID),
		"deepseek",
		"llama-4",
		"mistral-nemo",
		"qwen2.5",
	)
}
