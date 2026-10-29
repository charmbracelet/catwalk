// Package main provides a command-line tool to fetch models from Prime
// Inference and generate a configuration file for the provider.
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

// The Prime Inference models endpoint is public, so the tool needs no API key.
const modelsURL = "https://api.pinference.ai/api/v1/models"

// The API returns the OpenAI-style list of models along with pricing, context
// window and modality metadata.
type PrimeInferenceModel struct {
	ID                  string     `json:"id"`
	DisplayName         string     `json:"display_name"`
	Pricing             Pricing    `json:"pricing"`
	Specs               Specs      `json:"specs"`
	Reasoning           *Reasoning `json:"reasoning"`
	ContextWindow       *int64     `json:"context_window"`
	MaxOutputTokens     *int64     `json:"max_output_tokens"`
	SupportedParameters []string   `json:"supported_parameters"`
}

type Pricing struct {
	InputUSDPerMTok      *float64 `json:"input_usd_per_mtok"`
	OutputUSDPerMTok     *float64 `json:"output_usd_per_mtok"`
	CacheReadUSDPerMTok  *float64 `json:"cache_read_usd_per_mtok"`
	CacheWriteUSDPerMTok *float64 `json:"cache_write_usd_per_mtok"`
}

type Specs struct {
	ContextWindow     *int64     `json:"context_window"`
	MaxOutputTokens   *int64     `json:"max_output_tokens"`
	Modalities        Modalities `json:"modalities"`
	SupportsReasoning bool       `json:"supports_reasoning"`
}

type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type Reasoning struct {
	SupportedEfforts []string `json:"supported_efforts"`
	DefaultEffort    string   `json:"default_effort"`
}

type ModelsResponse struct {
	Data []PrimeInferenceModel `json:"data"`
}

func cost(v *float64) float64 {
	if v == nil {
		return 0
	}
	return math.Round(*v*1e5) / 1e5
}

func hasModality(m PrimeInferenceModel, modality string) bool {
	return slices.Contains(m.Specs.Modalities.Input, modality)
}

// internal and frontier host Prime Intellect-internal deployments, which are
// not part of the public catalog.
func isInternalDeployment(id string) bool {
	return slices.Contains([]string{"internal", "frontier"}, strings.SplitN(id, "/", 2)[0])
}

func modelDisplayName(id string) string {
	_, name, found := strings.Cut(id, "/")
	if !found || name == "" {
		return id
	}
	return name
}

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", modelsURL, nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching Prime Inference models:", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading Prime Inference models response:", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error fetching Prime Inference models: status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/prime-inference-response.json", body, 0o600)

	var modelsResp ModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		log.Fatal("Error parsing Prime Inference models response:", err)
	}

	provider, err := generateProvider(modelsResp)
	if err != nil {
		log.Fatal("Error generating Prime Inference provider:", err)
	}

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Prime Inference provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("./internal/providers/configs/prime-inference.json", data, 0o600); err != nil {
		log.Fatal("Error writing Prime Inference provider config:", err)
	}
}

func generateProvider(modelsResp ModelsResponse) (catwalk.Provider, error) {
	var models []catwalk.Model
	for _, m := range modelsResp.Data {
		if !slices.Contains(m.SupportedParameters, "tools") {
			continue
		}
		if !hasModality(m, "text") {
			continue
		}
		if !slices.Contains(m.Specs.Modalities.Output, "text") {
			continue
		}
		if isInternalDeployment(m.ID) {
			continue
		}

		contextWindow := m.ContextWindow
		if contextWindow == nil {
			contextWindow = m.Specs.ContextWindow
		}
		if contextWindow == nil || *contextWindow <= 0 {
			continue
		}
		maxOutputTokens := m.MaxOutputTokens
		if maxOutputTokens == nil {
			maxOutputTokens = m.Specs.MaxOutputTokens
		}
		if maxOutputTokens == nil {
			maxOutputTokens = contextWindow
		}

		canReason := m.Specs.SupportsReasoning || m.Reasoning != nil
		var reasoningLevels []string
		var defaultReasoning string
		if m.Reasoning != nil {
			reasoningLevels = m.Reasoning.SupportedEfforts
			defaultReasoning = m.Reasoning.DefaultEffort
		}

		name := m.DisplayName
		if name == "" {
			name = modelDisplayName(m.ID)
		}

		model := catwalk.Model{
			ID:                     m.ID,
			Name:                   name,
			CostPer1MIn:            cost(m.Pricing.InputUSDPerMTok),
			CostPer1MOut:           cost(m.Pricing.OutputUSDPerMTok),
			CostPer1MInCached:      cost(m.Pricing.CacheWriteUSDPerMTok),
			CostPer1MOutCached:     cost(m.Pricing.CacheReadUSDPerMTok),
			ContextWindow:          *contextWindow,
			DefaultMaxTokens:       *maxOutputTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         hasModality(m, "image"),
		}
		models = append(models, model)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	provider := catwalk.Provider{
		Name:                "Prime Inference",
		ID:                  catwalk.InferenceProviderPrimeInference,
		APIKey:              "$PRIME_API_KEY",
		APIEndpoint:         "https://api.pinference.ai/api/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek/deepseek-v4.1-flash",
		DefaultSmallModelID: "qwen/qwen3.7-flash",
		Models:              models,
	}

	for _, id := range []string{provider.DefaultLargeModelID, provider.DefaultSmallModelID} {
		if !slices.ContainsFunc(models, func(m catwalk.Model) bool { return m.ID == id }) {
			return catwalk.Provider{}, fmt.Errorf("default model %q is missing from the eligible catalog; keeping the existing config", id)
		}
	}
	return provider, nil
}
