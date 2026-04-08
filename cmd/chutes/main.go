// Package main provides a command-line tool to fetch models from Chutes
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

type ChutesModel struct {
	ID                string   `json:"id"`
	ContextLength     int64    `json:"context_length"`
	MaxOutputLength   int64    `json:"max_output_length"`
	InputModalities   []string `json:"input_modalities"`
	OutputModalities  []string `json:"output_modalities"`
	SupportedFeatures []string `json:"supported_features"`
	Pricing           Pricing  `json:"pricing"`
}

type Pricing struct {
	Prompt         float64 `json:"prompt"`
	Completion     float64 `json:"completion"`
	InputCacheRead float64 `json:"input_cache_read"`
}

type ModelsResponse struct {
	Data []ChutesModel `json:"data"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func hasFeature(m ChutesModel, feature string) bool {
	return slices.Contains(m.SupportedFeatures, feature)
}

func hasModality(m ChutesModel, modality string) bool {
	return slices.Contains(m.InputModalities, modality)
}

func modelDisplayName(id string) string {
	return strings.SplitN(id, "/", 2)[1]
}

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://llm.chutes.ai/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching Chutes models:", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading Chutes models response:", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error fetching Chutes models: status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/chutes-response.json", body, 0o600)

	var modelsResp ModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		log.Fatal("Error parsing Chutes models response:", err)
	}

	var models []catwalk.Model
	for _, m := range modelsResp.Data {
		if !hasFeature(m, "tools") {
			continue
		}
		if !hasModality(m, "text") {
			continue
		}
		if !slices.Contains(m.OutputModalities, "text") {
			continue
		}

		var (
			canReason        = hasFeature(m, "reasoning")
			reasoningLevels  []string
			defaultReasoning string
		)
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		model := catwalk.Model{
			ID:                     m.ID,
			Name:                   modelDisplayName(m.ID),
			CostPer1MIn:            roundCost(m.Pricing.Prompt),
			CostPer1MOut:           roundCost(m.Pricing.Completion),
			CostPer1MInCached:      roundCost(m.Pricing.InputCacheRead),
			ContextWindow:          m.ContextLength,
			DefaultMaxTokens:       m.MaxOutputLength,
			CanReason:              canReason,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
			SupportsImages:         hasModality(m, "image"),
		}
		models = append(models, model)
		fmt.Printf("Added model %s\n", m.ID)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	chutesProvider := catwalk.Provider{
		Name:                "Chutes",
		ID:                  "chutes",
		APIKey:              "$CHUTES_API_KEY",
		APIEndpoint:         "https://llm.chutes.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "zai-org/GLM-5-TEE",
		DefaultSmallModelID: "zai-org/GLM-5-Turbo",
		Models:              models,
	}

	data, err := json.MarshalIndent(chutesProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Chutes provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("./internal/providers/configs/chutes.json", data, 0o600); err != nil {
		log.Fatal("Error writing Chutes provider config:", err)
	}

	fmt.Println("Chutes provider configuration generated successfully!")
}
