// Package main provides a command-line tool to fetch models from Friendli
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

// FriendliModel represents a model from the Friendli models API.
type FriendliModel struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	ContextLength       int64            `json:"context_length"`
	MaxCompletionTokens int64            `json:"max_completion_tokens"`
	Pricing             FriendliPricing  `json:"pricing"`
	Functionality       FriendliFeatures `json:"functionality"`
	Reasoning           bool             `json:"reasoning"`
	ReasoningOptions    []ReasoningOpt   `json:"reasoning_options"`
	InputModalities     []string         `json:"input_modalities"`
	DeprecationDate     *string          `json:"deprecation_date,omitempty"`
}

// FriendliPricing contains per-token pricing from the Friendli API.
type FriendliPricing struct {
	Input          string `json:"input"`
	Output         string `json:"output"`
	InputCacheRead string `json:"input_cache_read"`
}

// FriendliFeatures describes what the model supports.
type FriendliFeatures struct {
	ToolCall         bool `json:"tool_call"`
	StructuredOutput bool `json:"structured_output"`
}

// ReasoningOpt represents a reasoning control option.
type ReasoningOpt struct {
	Type string `json:"type"`
	Min  int64  `json:"min,omitempty"`
	Max  int64  `json:"max,omitempty"`
}

// ModelsResponse is the response from the Friendli models endpoint.
type ModelsResponse struct {
	Data []FriendliModel `json:"data"`
}

func fetchFriendliModels() (*ModelsResponse, error) {
	apiKey := os.Getenv("FRIENDLI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("FRIENDLI_API_KEY environment variable is not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.friendli.ai/serverless/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/friendli-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func hasModality(m FriendliModel, modality string) bool {
	return slices.Contains(m.InputModalities, modality)
}

// parsePrice converts a per-token price string to cost per 1M tokens.
func parsePrice(perToken string) float64 {
	var v float64
	if err := json.Unmarshal([]byte(perToken), &v); err != nil {
		return 0
	}
	return math.Round(v*1e6*1e5) / 1e5
}

// displayName returns a human-friendly name from the model ID by stripping
// the organization prefix (e.g. "zai-org/GLM-5.2" → "GLM-5.2").
func displayName(id string) string {
	if idx := strings.Index(id, "/"); idx >= 0 {
		return id[idx+1:]
	}
	return id
}

func main() {
	modelsResp, err := fetchFriendliModels()
	if err != nil {
		log.Fatal("Error fetching Friendli models:", err)
	}

	friendliProvider := catwalk.Provider{
		Name:                "Friendli",
		ID:                  catwalk.InferenceProviderFriendli,
		APIKey:              "$FRIENDLI_API_KEY",
		APIEndpoint:         "https://api.friendli.ai/serverless/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "zai-org/GLM-5.2",
		DefaultSmallModelID: "deepseek-ai/DeepSeek-V3.2",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		// Skip deprecated models.
		if model.DeprecationDate != nil {
			continue
		}
		// Skip models without text input or tool calling.
		if !hasModality(model, "text") {
			continue
		}
		if !model.Functionality.ToolCall {
			continue
		}

		m := catwalk.Model{
			ID:                 model.ID,
			Name:               displayName(model.ID),
			CostPer1MIn:        parsePrice(model.Pricing.Input),
			CostPer1MOut:       parsePrice(model.Pricing.Output),
			CostPer1MInCached:  parsePrice(model.Pricing.InputCacheRead),
			CostPer1MOutCached: 0,
			ContextWindow:      model.ContextLength,
			DefaultMaxTokens:   model.MaxCompletionTokens,
			CanReason:          model.Reasoning,
			SupportsImages:     hasModality(model, "image"),
		}

		friendliProvider.Models = append(friendliProvider.Models, m)
	}

	slices.SortFunc(friendliProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(friendliProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Friendli provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/friendli.json", data, 0o600); err != nil {
		log.Fatal("Error writing Friendli provider config:", err)
	}

	fmt.Printf("Generated friendli.json with %d models\n", len(friendliProvider.Models))
}
