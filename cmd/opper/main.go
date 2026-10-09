// Package main provides a command-line tool to fetch models from the Opper
// catalog and generate a configuration file for the provider.
package main

import (
	"context"
	"encoding/json"
	"errors"
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

// poolNames lists the Opper model pools to include. A pool groups every
// provider route that serves the same model under one bare ID, and Opper
// picks the route for each request.
var poolNames = []string{
	"claude-haiku-4-5",
	"claude-opus-5",
	"claude-opus-5-5",
	"claude-sonnet-4-6",
	"claude-sonnet-5-5",
	"deepseek-v4-pro",
	"gemini-3.1-pro-preview",
	"gemini-3.8-flash",
	"glm-5.3",
	"gpt-5.4-mini",
	"gpt-5.5",
	"gpt-6.1-sol",
	"kimi-k3",
	"mistral-large-2512",
	"qwen3.8-max",
}

// effortLevels lists the reasoning_effort values in increasing order.
var effortLevels = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// Pricing contains a route's prices in USD per million tokens. Each field
// lists the price tiers, and the first entry is the base tier.
type Pricing struct {
	Input         []float64 `json:"input"`
	Output        []float64 `json:"output"`
	CachedInput   []float64 `json:"cached_input"`
	CacheCreation []float64 `json:"cache_creation"`
}

// Reasoning lists the reasoning_effort values a route accepts.
type Reasoning struct {
	Supported []string `json:"supported"`
}

// Params contains the request parameters a route accepts.
type Params struct {
	Reasoning *Reasoning `json:"reasoning"`
}

// Model represents a single provider route from the Opper models API.
type Model struct {
	Pool            string   `json:"model"`
	Name            string   `json:"name"`
	Pooled          bool     `json:"pooled"`
	Access          string   `json:"access"`
	RetiredAt       string   `json:"retired_at"`
	Capabilities    []string `json:"capabilities"`
	ContextWindow   int64    `json:"context_window"`
	MaxOutputTokens int64    `json:"max_output_tokens"`
	Pricing         *Pricing `json:"pricing"`
	Params          Params   `json:"params"`
}

// ModelsResponse is the response structure for the Opper models API.
type ModelsResponse struct {
	Models []Model `json:"models"`
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// basePrice returns the base tier of a price, or 0 when it is not listed.
func basePrice(tiers []float64) float64 {
	if len(tiers) == 0 {
		return 0
	}
	return tiers[0]
}

// minPositive returns the smaller of a and b, ignoring values that are not
// positive, which the catalog uses for unknown limits.
func minPositive(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	return min(a, b)
}

// allSupportEffort reports whether every route accepts the reasoning_effort
// value.
func allSupportEffort(routes []Model, effort string) bool {
	for _, route := range routes {
		if route.Params.Reasoning == nil || !slices.Contains(route.Params.Reasoning.Supported, effort) {
			return false
		}
	}
	return true
}

func fetchOpperModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	// The catalog is public. A non-positive limit returns every row.
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.opper.ai/v3/models?type=llm&limit=0",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating models request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

// poolModel merges the routes of one pool into a single model. Prices are the
// highest and limits the lowest across the routes, so they hold whichever
// route serves a request. Cache prices, attachments and reasoning levels are
// only set when every route supports them.
func poolModel(id string, routes []Model) (catwalk.Model, error) {
	if len(routes) == 0 {
		return catwalk.Model{}, errors.New("no usable routes")
	}

	m := catwalk.Model{
		ID:             id,
		Name:           routes[0].Name,
		SupportsImages: true,
	}
	var contextWindow, maxOutput int64
	cacheRead, cacheWrite := true, true
	for _, route := range routes {
		if !slices.Contains(route.Capabilities, "tools") {
			return catwalk.Model{}, errors.New("a route does not support tools")
		}
		m.CostPer1MIn = max(m.CostPer1MIn, basePrice(route.Pricing.Input))
		m.CostPer1MOut = max(m.CostPer1MOut, basePrice(route.Pricing.Output))
		m.CostPer1MInCached = max(m.CostPer1MInCached, basePrice(route.Pricing.CacheCreation))
		m.CostPer1MOutCached = max(m.CostPer1MOutCached, basePrice(route.Pricing.CachedInput))
		cacheRead = cacheRead && len(route.Pricing.CachedInput) > 0
		cacheWrite = cacheWrite && len(route.Pricing.CacheCreation) > 0
		contextWindow = minPositive(contextWindow, route.ContextWindow)
		maxOutput = minPositive(maxOutput, route.MaxOutputTokens)
		m.SupportsImages = m.SupportsImages && slices.Contains(route.Capabilities, "vision")
	}
	if contextWindow <= 0 {
		return catwalk.Model{}, errors.New("unknown context window")
	}

	if !cacheRead {
		m.CostPer1MOutCached = 0
	}
	if !cacheWrite {
		m.CostPer1MInCached = 0
	}
	m.CostPer1MIn = roundCost(m.CostPer1MIn)
	m.CostPer1MOut = roundCost(m.CostPer1MOut)
	m.CostPer1MInCached = roundCost(m.CostPer1MInCached)
	m.CostPer1MOutCached = roundCost(m.CostPer1MOutCached)

	// DefaultMaxTokens: use half of max_output_tokens when available,
	// capped at 15% of context_window; otherwise 10% of context_window.
	m.ContextWindow = contextWindow
	m.DefaultMaxTokens = contextWindow / 10
	if maxOutput > 0 && maxOutput/2 <= contextWindow*15/100 {
		m.DefaultMaxTokens = maxOutput / 2
	}

	// Crush sends reasoning_effort, so only offer the levels every route
	// accepts.
	levels := make([]string, 0, len(effortLevels))
	for _, effort := range effortLevels {
		if allSupportEffort(routes, effort) {
			levels = append(levels, effort)
		}
	}
	if len(levels) > 0 {
		m.CanReason = true
		m.ReasoningLevels = levels
		m.DefaultReasoningEffort = levels[len(levels)/2]
		if slices.Contains(levels, "medium") {
			m.DefaultReasoningEffort = "medium"
		}
	}

	return m, nil
}

func main() {
	opperProvider := catwalk.Provider{
		Name:                "Opper",
		ID:                  catwalk.InferenceProviderOpper,
		APIKey:              "$OPPER_API_KEY",
		APIEndpoint:         "https://api.opper.ai/v3/compat",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-4-6",
		DefaultSmallModelID: "gpt-5.4-mini",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchOpperModels()
	if err != nil {
		log.Fatal("Error fetching Opper models:", err)
	}

	// Group the pooled routes by pool. Skip routes the catalog marks as
	// restricted, since not every account can use them, and routes that are
	// scheduled for retirement.
	routes := make(map[string][]Model)
	for _, model := range modelsResp.Models {
		if !model.Pooled || model.Access == "restricted" || model.RetiredAt != "" || model.Pricing == nil {
			continue
		}
		routes[model.Pool] = append(routes[model.Pool], model)
	}

	for _, name := range poolNames {
		m, err := poolModel(name, routes[name])
		if err != nil {
			fmt.Printf("Skipping pool %s: %v\n", name, err)
			continue
		}
		opperProvider.Models = append(opperProvider.Models, m)
	}

	slices.SortFunc(opperProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(opperProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Opper provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/opper.json", data, 0o600); err != nil {
		log.Fatal("Error writing Opper provider config:", err)
	}

	fmt.Printf("Generated opper.json with %d models\n", len(opperProvider.Models))
}
