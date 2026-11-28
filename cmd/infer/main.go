// Package main generates the Infer provider configuration from Infer's public
// model catalog.
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

	"charm.land/catwalk/v3/pkg/catwalk"
)

const (
	catalogURL = "https://infer.flow7.org/api/public/catalog"
	configPath = "internal/providers/configs/infer.json"

	// route is the route listed for each model: the lowest-cost supply that
	// passes Infer's checks.
	route = "low-cost"

	// maxDefaultTokens is the output cap Infer applies when a request sends
	// none. Infer holds a request's maximum cost before it starts, so a larger
	// default would hold more of a small wallet than a reply usually needs.
	maxDefaultTokens = 32768
)

// Preferred defaults, best first. Generation fails rather than guessing when
// none of them is listed, so the committed file keeps valid defaults.
var (
	largeModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "gpt-6.1-sol"}
	smallModels = []string{"claude-haiku-4-5", "gpt-6-luna", "gpt-6.1-terra"}
)

type inferCatalog struct {
	Environment   string         `json:"environment"`
	Simulated     bool           `json:"simulated"`
	LiveAvailable bool           `json:"live_available"`
	Models        []catalogModel `json:"models"`
}

type catalogModel struct {
	Slug    string                 `json:"slug"`
	Kind    string                 `json:"kind"`
	Runtime runtime                `json:"runtime"`
	Tiers   map[string]catalogTier `json:"tiers"`
}

type catalogTier struct {
	Selector    string  `json:"selector"`
	Available   bool    `json:"available"`
	Input       float64 `json:"input_per_million_usd"`
	CachedInput float64 `json:"cached_input_per_million_usd"`
	CacheWrite  float64 `json:"cache_write_per_million_usd"`
	Output      float64 `json:"output_per_million_usd"`
	Market      market  `json:"market"`
	Runtime     runtime `json:"runtime"`
}

// market compares a route's price with the lab's own list price.
type market struct {
	Discounts struct {
		Input  *float64 `json:"input_percent"`
		Output *float64 `json:"output_percent"`
	} `json:"discounts"`
}

type runtime struct {
	MaxContextTokens *int64 `json:"max_context_tokens"`
	MaxOutputTokens  *int64 `json:"max_output_tokens"`
	Tools            bool   `json:"tools"`
	Vision           bool   `json:"vision"`
	Audio            bool   `json:"audio"`
}

func main() {
	catalog, err := fetchCatalog()
	if err != nil {
		log.Fatal("Error fetching Infer catalog: ", err)
	}
	if catalog.Environment != "live" || catalog.Simulated || !catalog.LiveAvailable {
		log.Fatalf("Infer catalog is not live (environment %q, simulated %t)", catalog.Environment, catalog.Simulated)
	}

	models := make([]catwalk.Model, 0, len(catalog.Models))
	for _, m := range catalog.Models {
		if model, ok := toModel(m); ok {
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		log.Fatal("No Infer routes to list; leaving the existing config untouched")
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	large, err := pickDefault(models, largeModels)
	if err != nil {
		log.Fatal(err)
	}
	small, err := pickDefault(models, smallModels)
	if err != nil {
		log.Fatal(err)
	}

	inferProvider := catwalk.Provider{
		Name:                "Infer by Flow7",
		ID:                  catwalk.InferenceProviderInfer,
		APIKey:              "$INFER_API_KEY",
		APIEndpoint:         "https://infer.flow7.org/v1",
		Type:                catwalk.TypeCompletions,
		DefaultLargeModelID: large,
		DefaultSmallModelID: small,
		Models:              models,
	}

	data, err := json.MarshalIndent(inferProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Infer provider: ", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		log.Fatal("Error writing Infer provider config: ", err)
	}

	fmt.Printf("Generated infer.json with %d models\n", len(models))
}

func fetchCatalog() (inferCatalog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return inferCatalog{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return inferCatalog{}, fmt.Errorf("fetching catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return inferCatalog{}, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var catalog inferCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return inferCatalog{}, fmt.Errorf("decoding catalog: %w", err)
	}
	return catalog, nil
}

// toModel returns the catalog model's Low cost route as a catwalk model, or
// false when the route shouldn't be listed.
func toModel(m catalogModel) (catwalk.Model, bool) {
	tier, ok := m.Tiers[route]
	if m.Kind != "exact" || !ok || !tier.Available || !tier.Runtime.Tools || tier.Selector == "" {
		return catwalk.Model{}, false
	}

	// Only routes the catalog prices below the lab's own list price on both
	// input and output are listed as Low cost.
	if !positive(tier.Market.Discounts.Input) || !positive(tier.Market.Discounts.Output) {
		return catwalk.Model{}, false
	}

	// Gemini needs its thought signatures back on the turn after a tool call.
	// Infer streams them in reasoning_details, which Crush doesn't send back
	// yet, so Gemini routes are left out for now.
	if strings.HasPrefix(m.Slug, "gemini-") {
		return catwalk.Model{}, false
	}

	contextWindow := lowest(m.Runtime.MaxContextTokens, tier.Runtime.MaxContextTokens)
	maxOutput := lowest(m.Runtime.MaxOutputTokens, tier.Runtime.MaxOutputTokens)
	if contextWindow <= 0 || maxOutput <= 0 {
		return catwalk.Model{}, false
	}

	return catwalk.Model{
		ID:   tier.Selector,
		Name: displayName(m.Slug) + " (Low cost)",
		Pricing: catwalk.Pricing{
			Input:       tier.Input,
			Output:      tier.Output,
			CacheCreate: tier.CacheWrite,
			CacheHit:    tier.CachedInput,
		},
		ContextWindow:    contextWindow,
		DefaultMaxTokens: min(maxOutput, contextWindow, maxDefaultTokens),
		// Infer streams a thinking model's signed reasoning record as
		// reasoning_details, and Crush sends back only the reasoning text, so a
		// thinking model would reject the turn after a tool call. With no
		// effort set, Crush asks for no thinking.
		Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingNever},
		Capabilities: catwalk.Capabilities{
			Vision: tier.Runtime.Vision,
			Audio:  tier.Runtime.Audio,
		},
	}, true
}

func positive(v *float64) bool {
	return v != nil && *v > 0
}

// lowest returns the smallest published limit, or 0 when none is published.
func lowest(values ...*int64) int64 {
	var out int64
	for _, v := range values {
		if v != nil && *v > 0 && (out == 0 || *v < out) {
			out = *v
		}
	}
	return out
}

func pickDefault(models []catwalk.Model, slugs []string) (string, error) {
	for _, slug := range slugs {
		id := "infer/" + slug + ":" + route
		if slices.ContainsFunc(models, func(m catwalk.Model) bool { return m.ID == id }) {
			return id, nil
		}
	}
	return "", fmt.Errorf("none of %v is listed; leaving the existing config untouched", slugs)
}

// displayName turns a catalog slug into the lab's model name: claude-opus-5-5
// is Claude Opus 5.5, gpt-6.1-sol is GPT-6.1 Sol, and grok-4.7 is Grok 4.7.
func displayName(slug string) string {
	parts := strings.Split(slug, "-")
	switch {
	case parts[0] == "claude" && len(parts) >= 3:
		return "Claude " + capitalize(parts[1]) + " " + strings.Join(parts[2:], ".")
	case parts[0] == "gpt" && len(parts) >= 2:
		name := "GPT-" + parts[1]
		for _, part := range parts[2:] {
			name += " " + capitalize(part)
		}
		return name
	case parts[0] == "grok" && len(parts) >= 2:
		return "Grok " + strings.Join(parts[1:], " ")
	}
	return slug
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
