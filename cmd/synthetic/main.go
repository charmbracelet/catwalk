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

	"github.com/charmbracelet/catwalk/pkg/catwalk"
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

// priceOverrides contains manual pricing for models where the API doesn't
// provide pricing information. Keys are model IDs, values are [input, output]
// costs per 1M tokens.
// TODO: Remove this when Synthetic adds pricing to their models endpoint.
var priceOverrides = map[string][2]float64{
	"hf:deepseek-ai/DeepSeek-R1-0528":                      {3.00, 8.00},
	"hf:deepseek-ai/DeepSeek-V3":                           {1.25, 1.25},
	"hf:deepseek-ai/DeepSeek-V3-0324":                      {1.20, 1.20},
	"hf:deepseek-ai/DeepSeek-V3.1":                         {0.56, 1.68},
	"hf:deepseek-ai/DeepSeek-V3.1-Terminus":                {1.20, 1.20},
	"hf:deepseek-ai/DeepSeek-V3.2":                         {0.56, 1.68},
	"hf:meta-llama/Llama-3.3-70B-Instruct":                 {0.90, 0.90},
	"hf:meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8": {0.22, 0.88},
	"hf:MiniMaxAI/MiniMax-M2":                              {0.30, 1.20},
	"hf:MiniMaxAI/MiniMax-M2.1":                            {0.55, 2.19},
	"hf:moonshotai/Kimi-K2-Instruct-0905":                  {1.20, 1.20},
	"hf:moonshotai/Kimi-K2-Thinking":                       {0.55, 2.19},
	"hf:openai/gpt-oss-120b":                               {0.10, 0.10},
	"hf:Qwen/Qwen3-235B-A22B-Instruct-2507":                {0.22, 0.88},
	"hf:Qwen/Qwen3-235B-A22B-Thinking-2507":                {0.65, 3.00},
	"hf:Qwen/Qwen3-Coder-480B-A35B-Instruct":               {0.45, 1.80},
	"hf:Qwen/Qwen3-VL-235B-A22B-Instruct":                  {0.22, 0.88},
	"hf:zai-org/GLM-4.5":                                   {0.55, 2.19},
	"hf:zai-org/GLM-4.5-Open":                              {0.55, 2.19},
	"hf:zai-org/GLM-4.6":                                   {0.55, 2.19},
}

func getPricing(model Model) ModelPricing {
	// Check for manual price override first
	// Synthetic doesn't have caching afaict
	if override, ok := priceOverrides[model.ID]; ok {
		return ModelPricing{
			CostPer1MIn:        override[0],
			CostPer1MOut:       override[1],
			CostPer1MInCached:  override[0],
			CostPer1MOutCached: override[1],
		}
	}

	// Fall back to API pricing
	pricing := ModelPricing{}
	costPrompt, err := strconv.ParseFloat(model.Pricing.Prompt, 64)
	if err != nil {
		costPrompt = 0.0
	}
	pricing.CostPer1MIn = costPrompt * 1_000_000
	costCompletion, err := strconv.ParseFloat(model.Pricing.Completion, 64)
	if err != nil {
		costCompletion = 0.0
	}
	pricing.CostPer1MOut = costCompletion * 1_000_000

	costPromptCached, err := strconv.ParseFloat(model.Pricing.InputCacheWrites, 64)
	if err != nil {
		costPromptCached = 0.0
	}
	pricing.CostPer1MInCached = costPromptCached * 1_000_000
	costCompletionCached, err := strconv.ParseFloat(model.Pricing.InputCacheReads, 64)
	if err != nil {
		costCompletionCached = 0.0
	}
	pricing.CostPer1MOutCached = costCompletionCached * 1_000_000
	return pricing
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

	// Has correct metadata already, but the Kimi-K2 matcher (next) would
	// override it to omit reasoning
	case strings.HasPrefix(model.ID, "hf:moonshotai/Kimi-K2-Thinking"):
		model.SupportedFeatures = []string{"tools", "reasoning"}

	case strings.HasPrefix(model.ID, "hf:moonshotai/Kimi-K2"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:zai-org/GLM-4.5"):
		model.SupportedFeatures = []string{"tools"}

	case strings.HasPrefix(model.ID, "hf:openai/gpt-oss"):
		model.SupportedFeatures = []string{"tools"}
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
		DefaultSmallModelID: "hf:deepseek-ai/DeepSeek-V3.1-Terminus",
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

	fmt.Printf("Generated synthetic.json with %d models (API pricing)\n", len(syntheticProvider.Models))

	// Generate Synthetic Pro/Max provider with zero pricing
	proMaxProvider := catwalk.Provider{
		Name:                "Synthetic Pro/Max",
		ID:                  "synthetic-promax",
		APIKey:              "$SYNTHETIC_API_KEY",
		APIEndpoint:         "https://api.synthetic.new/openai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: syntheticProvider.DefaultLargeModelID,
		DefaultSmallModelID: syntheticProvider.DefaultSmallModelID,
		Models:              make([]catwalk.Model, len(syntheticProvider.Models)),
	}

	// Copy models with zero pricing
	for i, model := range syntheticProvider.Models {
		model.CostPer1MIn = 0
		model.CostPer1MOut = 0
		model.CostPer1MInCached = 0
		model.CostPer1MOutCached = 0
		proMaxProvider.Models[i] = model
	}

	proMaxData, err := json.MarshalIndent(proMaxProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Synthetic Pro/Max provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/synthetic-promax.json", proMaxData, 0o600); err != nil {
		log.Fatal("Error writing Synthetic Pro/Max provider config:", err)
	}

	fmt.Printf("Generated synthetic-promax.json with %d models (subscription pricing)\n", len(proMaxProvider.Models))
}
