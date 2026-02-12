// Package main provides a command-line tool to fetch models from Synthetic
// and generate a configuration file for the provider.
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

// Model represents a model from the Synthetic API.
type Model struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	InputModalities   []string `json:"input_modalities"`
	OutputModalities  []string `json:"output_modalities"`
	ContextLength     int64    `json:"context_length"`
	MaxOutputLength   int64    `json:"max_output_length,omitempty"`
	Pricing           Pricing  `json:"pricing"`
	SupportedFeatures []string `json:"supported_features,omitempty"`
}

// Pricing contains the pricing information for different operations.
type Pricing struct {
	Prompt           string `json:"prompt"`
	Completion       string `json:"completion"`
	Image            string `json:"image"`
	Request          string `json:"request"`
	InputCacheReads  string `json:"input_cache_reads"`
	InputCacheWrites string `json:"input_cache_writes"`
}

// ModelsResponse is the response structure for the Synthetic models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

// ModelPricing is the pricing structure for a model, detailing costs per
// million tokens for input and output, both cached and uncached.
type ModelPricing struct {
	CostPer1MIn        float64 `json:"cost_per_1m_in"`
	CostPer1MOut       float64 `json:"cost_per_1m_out"`
	CostPer1MInCached  float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached float64 `json:"cost_per_1m_out_cached"`
}

// parsePrice extracts a float from Synthetic's price format (e.g. "$0.00000055").
func parsePrice(s string) float64 {
	s = strings.TrimPrefix(s, "$")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return v
}

func getPricing(model Model) ModelPricing {
	return ModelPricing{
		CostPer1MIn:        parsePrice(model.Pricing.Prompt) * 1_000_000,
		CostPer1MOut:       parsePrice(model.Pricing.Completion) * 1_000_000,
		CostPer1MInCached:  parsePrice(model.Pricing.InputCacheReads) * 1_000_000,
		CostPer1MOutCached: parsePrice(model.Pricing.InputCacheReads) * 1_000_000,
	}
}

// applyModelOverrides sets supported_features for models where Synthetic
// omits this metadata.
// TODO: Remove this when they add the missing metadata.
func applyModelOverrides(model *Model) {
	switch {
	// All of llama support tools, none do reasoning yet
	case strings.HasPrefix(model.ID, "hf:meta-llama/Llama-"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:deepseek-ai/DeepSeek-R1"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:deepseek-ai/DeepSeek-V3.1"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:deepseek-ai/DeepSeek-V3.2"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:deepseek-ai/DeepSeek-V3"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:Qwen/Qwen3-235B-A22B-Thinking"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:Qwen/Qwen3-235B-A22B-Instruct"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	// The rest of Qwen3 don't support reasoning but do tools
	case strings.HasPrefix(model.ID, "hf:Qwen/Qwen3"):
		model.SupportedFeatures = []string{"tools"}

	// Has correct metadata already, but the following k2 matchers would
	// override it to omit reasoning
	case strings.HasPrefix(model.ID, "hf:moonshotai/Kimi-K2-Thinking"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:moonshotai/Kimi-K2.5"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:moonshotai/Kimi-K2"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:zai-org/GLM-4.5"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:openai/gpt-oss"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:MiniMaxAI/MiniMax-M2.1"):
		model.SupportedFeatures = []string{"tools", "reasoning"}
	}
}

func fetchSyntheticModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

// This is used to generate the synthetic.json config file.
func main() {
	syntheticProvider := catwalk.Provider{
		Name:                "Synthetic",
		ID:                  "synthetic",
		APIKey:              "$SYNTHETIC_API_KEY",
		APIEndpoint:         "https://api.synthetic.new/openai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "hf:zai-org/GLM-4.7",
		DefaultSmallModelID: "hf:deepseek-ai/DeepSeek-V3.2",
		Models:              []catwalk.Model{},
	}

	modelsResp, err := fetchSyntheticModels(syntheticProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Synthetic models:", err)
	}

	// Apply overrides for models missing supported_features metadata
	for i := range modelsResp.Data {
		applyModelOverrides(&modelsResp.Data[i])
	}

	for _, model := range modelsResp.Data {
		// Skip models with small context windows
		if model.ContextLength < 20000 {
			continue
		}

		// Skip non-text models
		if !slices.Contains(model.InputModalities, "text") ||
			!slices.Contains(model.OutputModalities, "text") {
			continue
		}

		// Ensure they support tools
		supportsTools := slices.Contains(model.SupportedFeatures, "tools")
		if !supportsTools {
			continue
		}

		pricing := getPricing(model)
		supportsImages := slices.Contains(model.InputModalities, "image")

		// Check if model supports reasoning
		canReason := slices.Contains(model.SupportedFeatures, "reasoning")
		var reasoningLevels []string
		var defaultReasoning string
		if canReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		// Strip everything before the first / for a cleaner name
		modelName := model.Name
		if idx := strings.Index(model.Name, "/"); idx != -1 {
			modelName = model.Name[idx+1:]
		}
		// Replace hyphens with spaces
		modelName = strings.ReplaceAll(modelName, "-", " ")

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   modelName,
			CostPer1MIn:            pricing.CostPer1MIn,
			CostPer1MOut:           pricing.CostPer1MOut,
			CostPer1MInCached:      pricing.CostPer1MInCached,
			CostPer1MOutCached:     pricing.CostPer1MOutCached,
			ContextWindow:          model.ContextLength,
			CanReason:              canReason,
			DefaultReasoningEffort: defaultReasoning,
			ReasoningLevels:        reasoningLevels,
			SupportsImages:         supportsImages,
		}

		// Set max tokens based on max_output_length if available, but cap at
		// 15% of context length
		maxFromOutput := model.MaxOutputLength / 2
		maxAt15Pct := (model.ContextLength * 15) / 100
		if model.MaxOutputLength > 0 && maxFromOutput <= maxAt15Pct {
			m.DefaultMaxTokens = maxFromOutput
		} else {
			m.DefaultMaxTokens = model.ContextLength / 10
		}

		syntheticProvider.Models = append(syntheticProvider.Models, m)
		fmt.Printf("Added model %s with context window %d\n",
			model.ID, model.ContextLength)
	}

	slices.SortFunc(syntheticProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/synthetic.json
	data, err := json.MarshalIndent(syntheticProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Synthetic provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/synthetic.json", data, 0o600); err != nil {
		log.Fatal("Error writing Synthetic provider config:", err)
	}

	fmt.Printf("Generated synthetic.json with %d models\n", len(syntheticProvider.Models))
}
