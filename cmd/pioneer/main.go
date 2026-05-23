// Package main provides a command-line tool to fetch models from Pioneer
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

type PioneerModel struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	ContextWindow int64   `json:"context_window"`
	InputPrice    float64 `json:"input_price_per_million"`
	OutputPrice   float64 `json:"output_price_per_million"`
	TaskType      string  `json:"task_type"`
	IsChatModel   bool    `json:"is_chat_model"`
	Tier          string  `json:"tier"`
}

type PioneerResponse struct {
	Models []PioneerModel `json:"models"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.pioneer.ai/base-models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching Pioneer models:", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading Pioneer models response:", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error fetching Pioneer models: status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/pioneer-response.json", body, 0o600)

	var respData PioneerResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		log.Fatal("Error parsing Pioneer models response:", err)
	}

	var models []catwalk.Model
	for _, m := range respData.Models {
		if !m.IsChatModel {
			continue
		}
		if m.TaskType != "decoder" {
			continue
		}

		contextWindow := m.ContextWindow
		if contextWindow == 0 {
			contextWindow = 8192
		}

		defaultMaxTokens := contextWindow / 4
		if defaultMaxTokens > 128000 {
			defaultMaxTokens = 128000
		}
		if defaultMaxTokens < 4096 {
			defaultMaxTokens = 4096
		}

		isDeepSeek := strings.Contains(m.ID, "deepseek") || strings.Contains(m.Label, "DeepSeek")
		isQwen := strings.Contains(m.ID, "Qwen") || strings.Contains(m.Label, "Qwen3")

		var (
			canReason        bool
			reasoningLevels  []string
			defaultReasoning string
		)
		if isDeepSeek || isQwen {
			canReason = true
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		model := catwalk.Model{
			ID:                     m.ID,
			Name:                   m.Label,
			CostPer1MIn:            roundCost(m.InputPrice),
			CostPer1MOut:           roundCost(m.OutputPrice),
			ContextWindow:          contextWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReason,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
		}
		models = append(models, model)
		fmt.Printf("Added model %s\n", m.ID)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	pioneerProvider := catwalk.Provider{
		Name:                "Pioneer",
		ID:                  catwalk.InferenceProvider("pioneer"),
		APIKey:              "$PIONEER_API_KEY",
		APIEndpoint:         "https://api.pioneer.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-opus-4-6",
		DefaultSmallModelID: "Qwen/Qwen3.5-9B",
		Models:              models,
	}

	data, err := json.MarshalIndent(pioneerProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Pioneer provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("./internal/providers/configs/pioneer.json", data, 0o600); err != nil {
		log.Fatal("Error writing Pioneer provider config:", err)
	}

	fmt.Println("Pioneer provider configuration generated successfully!")
}
