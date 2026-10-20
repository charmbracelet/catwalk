// Package main provides a command-line tool to fetch models from ai& and
// generate a configuration file for the provider.
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
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

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
		// Crush drives an agent, so a model that cannot call tools is no use.
		if !slices.Contains(model.Capabilities, "tool_calling") {
			continue
		}

		canReason := slices.Contains(model.Capabilities, "reasoning")
		m := catwalk.Model{
			ID:   model.ID,
			Name: displayName(model),
			// The API publishes one cached price, the cache read price.
			CostPer1MIn:        parsePrice(model.InputPer1M),
			CostPer1MOut:       parsePrice(model.OutputPer1M),
			CostPer1MOutCached: parsePrice(model.CachedInputPer1M),
			ContextWindow:      model.ContextWindow,
			// Capped so a model with a very large output limit does not reserve
			// most of the context window on every request.
			DefaultMaxTokens: min(model.MaxOutputTokens, 131072),
			CanReason:        canReason,
			SupportsImages:   slices.Contains(model.Capabilities, "vision"),
		}
		if canReason {
			m.ReasoningLevels = model.ReasoningEfforts
			m.DefaultReasoningEffort = model.ReasoningEffortDefault
		}
		provider.Models = append(provider.Models, m)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	// Save the JSON in internal/providers/configs/aiand.json
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling ai& provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/aiand.json", data, 0o600); err != nil {
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

func parsePrice(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
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
