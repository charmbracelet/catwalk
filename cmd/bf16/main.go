// Package main provides a command-line tool to fetch models from bf16.ai
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
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
)

const (
	defaultLargeModelID = "zai-org/glm-5.3-uncensored"
	defaultSmallModelID = "deepseek/deepseek-v4.1-flash-uncensored"
)

type modelsResponse struct {
	Data []bf16Model `json:"data"`
}

type bf16Model struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	ContextWindow     int64        `json:"context_window"`
	ContextLength     int64        `json:"context_length"`
	MaxOutputTokens   int64        `json:"max_output_tokens"`
	MaxTokens         int64        `json:"max_tokens"`
	SupportsReasoning bool         `json:"supports_reasoning"`
	SupportsTools     bool         `json:"supports_tools"`
	SupportsVision    bool         `json:"supports_vision"`
	Pricing           *rawPricing  `json:"pricing"`
	BF16              *bf16Details `json:"bf16"`
}

type rawPricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

type bf16Details struct {
	DisplayName       string           `json:"display_name"`
	Family            string           `json:"family"`
	ContextWindow     int64            `json:"context_window"`
	MaxOutputTokens   int64            `json:"max_output_tokens"`
	SupportsReasoning bool             `json:"supports_reasoning"`
	SupportsTools     bool             `json:"supports_tools"`
	SupportsVision    bool             `json:"supports_vision"`
	Status            string           `json:"status"`
	PricingUSDPer1M   *pricingUSDPer1M `json:"pricing_usd_per_1m"`
}

type pricingUSDPer1M struct {
	Input       string `json:"input"`
	CachedInput string `json:"cached_input"`
	Output      string `json:"output"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func parsePrice(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return roundCost(v)
}

func extractPricing(m bf16Model) catwalk.Pricing {
	if m.BF16 != nil && m.BF16.PricingUSDPer1M != nil {
		return catwalk.Pricing{
			Input:    parsePrice(m.BF16.PricingUSDPer1M.Input),
			Output:   parsePrice(m.BF16.PricingUSDPer1M.Output),
			CacheHit: parsePrice(m.BF16.PricingUSDPer1M.CachedInput),
		}
	}
	if m.Pricing != nil {
		return catwalk.Pricing{
			Input:    roundCost(parsePrice(m.Pricing.Prompt) * 1e6),
			Output:   roundCost(parsePrice(m.Pricing.Completion) * 1e6),
			CacheHit: roundCost(parsePrice(m.Pricing.InputCacheRead) * 1e6),
		}
	}
	return catwalk.Pricing{}
}

func firstPositive(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://api.bf16.ai/v1/models",
		nil,
	)
	if err != nil {
		log.Fatal("Error building bf16.ai request:", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching bf16.ai models:", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading bf16.ai models response:", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error fetching bf16.ai models: status %d: %s", resp.StatusCode, body)
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		log.Fatal("Error parsing bf16.ai models response:", err)
	}

	var models []catwalk.Model
	for _, m := range modelsResp.Data {
		if m.BF16 != nil && m.BF16.Family == "router" {
			continue
		}
		if m.BF16 != nil && m.BF16.Status != "" && m.BF16.Status != "active" {
			continue
		}

		supportsTools := m.SupportsTools || (m.BF16 != nil && m.BF16.SupportsTools)
		if !supportsTools {
			continue
		}

		var bf16Ctx, bf16Out int64
		if m.BF16 != nil {
			bf16Ctx = m.BF16.ContextWindow
			bf16Out = m.BF16.MaxOutputTokens
		}
		contextWindow := firstPositive(m.ContextWindow, m.ContextLength, bf16Ctx)
		maxOutput := firstPositive(m.MaxOutputTokens, m.MaxTokens, bf16Out)
		if contextWindow == 0 || maxOutput == 0 {
			continue
		}

		name := strings.TrimSpace(m.Name)
		if name == "" && m.BF16 != nil {
			name = strings.TrimSpace(m.BF16.DisplayName)
		}
		if name == "" {
			name = m.ID
		}

		supportsReasoning := m.SupportsReasoning || (m.BF16 != nil && m.BF16.SupportsReasoning)
		reasoning := catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
		if supportsReasoning {
			reasoning = catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("low", "medium", "high", "max"),
				DefaultEffortLevel: "medium",
			}
		}

		supportsVision := m.SupportsVision || (m.BF16 != nil && m.BF16.SupportsVision)
		models = append(models, catwalk.Model{
			ID:               m.ID,
			Name:             name,
			Pricing:          extractPricing(m),
			ContextWindow:    contextWindow,
			DefaultMaxTokens: maxOutput,
			Reasoning:        reasoning,
			Capabilities:     catwalk.Capabilities{Vision: supportsVision},
		})
	}

	if len(models) == 0 {
		log.Fatal("No eligible bf16.ai models found")
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	largeID := defaultLargeModelID
	smallID := defaultSmallModelID
	hasModel := func(id string) bool {
		return slices.ContainsFunc(models, func(m catwalk.Model) bool {
			return m.ID == id
		})
	}
	if !hasModel(largeID) {
		largeID = models[0].ID
	}
	if !hasModel(smallID) {
		smallID = models[0].ID
	}

	provider := catwalk.Provider{
		Name:                "bf16.ai",
		ID:                  catwalk.InferenceProviderBF16,
		APIKey:              "$BF16_API_KEY",
		APIEndpoint:         "https://api.bf16.ai/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: largeID,
		DefaultSmallModelID: smallID,
		Models:              models,
	}

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling bf16.ai provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("./internal/providers/configs/bf16.json", data, 0o600); err != nil {
		log.Fatal("Error writing bf16.ai provider config:", err)
	}

	fmt.Println("bf16.ai provider configuration generated successfully!")
}
