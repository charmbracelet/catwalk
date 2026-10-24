// Package main provides a command-line tool to fetch models from ai& and
// generate a configuration file for the provider.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// maxDefaultMaxTokens caps default_max_tokens so a model with a very large
// output limit does not reserve most of the context window on every request.
const maxDefaultMaxTokens = 131072

// Model represents a model from the ai& models API.
type Model struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ContextWindow          int64    `json:"context_window"`
	MaxOutputTokens        int64    `json:"max_output_tokens"`
	Capabilities           []string `json:"capabilities"`
	ReasoningEfforts       []string `json:"reasoning_efforts"`
	ReasoningEffortDefault string   `json:"reasoning_effort_default"`
	InputPer1M             string   `json:"input_per_1m"`
	OutputPer1M            string   `json:"output_per_1m"`
	CachedInputPer1M       string   `json:"cached_input_per_1m"`
}

// Response is the response structure for the ai& models API.
type Response struct {
	Data []Model `json:"data"`
}

// This is used to generate the aiand.json config file.
func main() {
	provider := catwalk.Provider{
		Name:                "ai&",
		ID:                  catwalk.InferenceProviderAiand,
		APIKey:              "$AIAND_API_KEY",
		APIEndpoint:         "https://api.aiand.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "zai-org/glm-5.3",
		DefaultSmallModelID: "zai-org/glm-5.3-flash",
	}

	resp, err := fetchModels(provider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching ai& models:", err)
	}

	provider.Models = make([]catwalk.Model, 0, len(resp.Data))
	for _, model := range resp.Data {
		m, err := toCatwalkModel(model)
		if err != nil {
			log.Printf("Skipping %s: %v", model.ID, err)
			continue
		}
		provider.Models = append(provider.Models, m)
	}

	// A config without its default models fails TestValidDefaultModels, so
	// keep the last good file rather than write one.
	for _, id := range []string{provider.DefaultLargeModelID, provider.DefaultSmallModelID} {
		if !slices.ContainsFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			log.Fatalf("Default model %s is missing from the ai& models API", id)
		}
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	// Without SetEscapeHTML(false) the provider name is written as "ai\u0026".
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(provider); err != nil {
		log.Fatal("Error marshaling ai& provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/aiand.json", buf.Bytes(), 0o600); err != nil {
		log.Fatal("Error writing ai& provider config:", err)
	}

	fmt.Printf("Generated aiand.json with %d models\n", len(provider.Models))
}

// displayName drops the organization prefix from the API's model name, so
// "deepseek-ai/DeepSeek-V4.1-Flash" reads as "DeepSeek-V4.1-Flash".
func displayName(model Model) string {
	name := model.Name
	if name == "" {
		name = model.ID
	}
	if _, after, ok := strings.Cut(name, "/"); ok {
		return after
	}
	return name
}

func toCatwalkModel(model Model) (catwalk.Model, error) {
	in, err := parsePrice(model.InputPer1M)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("input price: %w", err)
	}
	out, err := parsePrice(model.OutputPer1M)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("output price: %w", err)
	}
	// The API publishes one cached price, the cache read price. A model
	// without one has no cache discount.
	var cacheRead float64
	if model.CachedInputPer1M != "" {
		if cacheRead, err = parsePrice(model.CachedInputPer1M); err != nil {
			return catwalk.Model{}, fmt.Errorf("cached input price: %w", err)
		}
	}
	if model.ContextWindow <= 0 || model.MaxOutputTokens <= 0 {
		return catwalk.Model{}, fmt.Errorf("context_window=%d max_output_tokens=%d", model.ContextWindow, model.MaxOutputTokens)
	}

	m := catwalk.Model{
		ID:                 model.ID,
		Name:               displayName(model),
		CostPer1MIn:        in,
		CostPer1MOut:       out,
		CostPer1MOutCached: cacheRead,
		ContextWindow:      model.ContextWindow,
		DefaultMaxTokens:   min(model.MaxOutputTokens, maxDefaultMaxTokens),
		CanReason:          slices.Contains(model.Capabilities, "reasoning"),
		SupportsImages:     slices.Contains(model.Capabilities, "vision"),
	}
	if m.CanReason {
		// Crush sends the default on every request, so one the API does not
		// list would be refused.
		if !slices.Contains(model.ReasoningEfforts, model.ReasoningEffortDefault) {
			return catwalk.Model{}, fmt.Errorf("default reasoning effort %q is not one of %v", model.ReasoningEffortDefault, model.ReasoningEfforts)
		}
		m.ReasoningLevels = model.ReasoningEfforts
		m.DefaultReasoningEffort = model.ReasoningEffortDefault
	}
	return m, nil
}

func parsePrice(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid price %q: %w", s, err)
	}
	return v, nil
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr Response
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("unable to unmarshal json: %w", err)
	}
	return &mr, nil
}
