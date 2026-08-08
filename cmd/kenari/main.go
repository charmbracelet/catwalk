// Package main provides a command-line tool to fetch models from Kenari
// and generate a configuration file for the provider.
//
// Kenari is an Indonesian LLM gateway that bills a prepaid IDR wallet
// rather than per-token USD, so all cost fields are written as zero and
// callers should not surface these numbers to end users.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

const (
	apiEndpoint = "https://kenari.id/v1"
	configPath  = "internal/providers/configs/kenari.json"
)

// Model mirrors the /v1/models response from kenari.id.
type Model struct {
	ID               string   `json:"id"`
	Object           string   `json:"object"`
	OwnedBy          string   `json:"owned_by"`
	Endpoints        []string `json:"endpoints"`
	ContextLength    *int64   `json:"context_length"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	ToolCall         bool     `json:"tool_call"`
	Reasoning        bool     `json:"reasoning"`
	ReasoningToggle  bool     `json:"reasoning_toggle"`
	ReasoningOptions []string `json:"reasoning_options"`
	Pricing          struct {
		Free      bool   `json:"free"`
		Currency  string `json:"currency"`
		Unit      string `json:"unit"`
		Input     int64  `json:"input"`
		Output    int64  `json:"output"`
		CacheRead int64  `json:"cache_read"`
	} `json:"pricing"`
}

// ModelsResponse is the top-level /v1/models response.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func fetchModels(endpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d from %s/models", resp.StatusCode, endpoint)
	}

	var out ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, nil
}

func isFreeVariant(id string) bool {
	return strings.HasSuffix(id, ":free")
}

// toCatwalkModel converts a Kenari API model into a catwalk.Model. Costs
// are always zero because Kenari bills a prepaid IDR wallet; a USD
// per-token figure would be invented and unstable.
func toCatwalkModel(m Model) (catwalk.Model, bool) {
	if m.ContextLength == nil {
		return catwalk.Model{}, false
	}
	if m.Pricing.Free {
		return catwalk.Model{}, false
	}
	if isFreeVariant(m.ID) {
		return catwalk.Model{}, false
	}
	if !slices.Contains(m.Endpoints, "chat") {
		return catwalk.Model{}, false
	}
	if !m.ToolCall {
		return catwalk.Model{}, false
	}

	name := m.ID
	// Catwalk uses Title-Case display names elsewhere; map dashes and
	// capitalise. Owned-by is metadata we deliberately do not include
	// (the original PR did not, and per the repo's "no backend names
	// on customer surfaces" rule we keep it that way).
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	name = strings.Join(parts, " ")

	defaultMaxTokens := *m.ContextLength / 10
	if defaultMaxTokens > 32768 {
		defaultMaxTokens = 32768
	}
	if defaultMaxTokens < 1024 {
		defaultMaxTokens = 1024
	}

	out := catwalk.Model{
		ID:                 m.ID,
		Name:               name,
		CostPer1MIn:        0,
		CostPer1MOut:       0,
		CostPer1MInCached:  0,
		CostPer1MOutCached: 0,
		ContextWindow:      *m.ContextLength,
		DefaultMaxTokens:   defaultMaxTokens,
		CanReason:          m.Reasoning,
		SupportsImages:     slices.Contains(m.Modalities.Input, "image"),
	}

	if m.Reasoning && len(m.ReasoningOptions) > 0 {
		out.ReasoningLevels = m.ReasoningOptions
		// Pick a middle option as the default effort. The midpoint of
		// the list (rounded down) is the same convention the existing
		// committed kenari.json used for the medium/low/high tiers.
		out.DefaultReasoningEffort = m.ReasoningOptions[len(m.ReasoningOptions)/2]
	}

	return out, true
}

func main() {
	provider := catwalk.Provider{
		Name:                "Kenari",
		ID:                  catwalk.InferenceProviderKenari,
		APIKey:              "$KENARI_API_KEY",
		APIEndpoint:         apiEndpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-5",
		DefaultSmallModelID: "deepseek-v4-flash",
		Models:              []catwalk.Model{},
	}

	resp, err := fetchModels(apiEndpoint)
	if err != nil {
		log.Fatal("fetching kenari models: ", err)
	}

	for _, m := range resp.Data {
		cm, ok := toCatwalkModel(m)
		if !ok {
			continue
		}
		provider.Models = append(provider.Models, cm)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("marshaling provider: ", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		log.Fatal("writing config: ", err)
	}

	fmt.Printf("Generated %s with %d models\n", configPath, len(provider.Models))
}
