// Package main provides a command-line tool to fetch LLM models from Novita.ai
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

// ModelsResponse is the OpenAI-compatible /openai/v1/models response.
type ModelsResponse struct {
	Data []NovitaModel `json:"data"`
}

// NovitaModel represents a single model entry in the /models response.
type NovitaModel struct {
	ID                     string  `json:"id"`
	Title                  string  `json:"title"`
	Description            string  `json:"description"`
	ContextSize            int64   `json:"context_size"`
	InputTokenPricePerM    float64 `json:"input_token_price_per_m"`
	OutputTokenPricePerM   float64 `json:"output_token_price_per_m"`
	CacheReadTokenPricePerM float64 `json:"cache_read_token_price_per_m"`
}

var reasoningLevels = []string{"low", "medium", "high"}

func fetchModels(endpoint, apiKey string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		endpoint+"/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/novita-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}

	return &mr, nil
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// isTextModel filters out embedding, reranker, image, OCR, and other non-text-chat models.
func isTextModel(m NovitaModel) bool {
	id := strings.ToLower(m.ID)
	title := strings.ToLower(m.Title)

	// Exclude embeddings, rerankers, and non-chat models
	excludePatterns := []string{
		"embedding",
		"reranker",
		"bge-",
		"paddleocr",
		"-ocr",
		"-vl-",      // vision-language variants
		"-vl$",      // vision-only suffix
		"omni-",     // omni (audio/video) variants
		"mt-plus",   // machine translation
		"autoglm",   // phone automation
		"prover",    // theorem prover
	}
	for _, p := range excludePatterns {
		if strings.Contains(id, p) || strings.Contains(title, p) {
			return false
		}
	}

	// Exclude very small context models (less than 4k)
	if m.ContextSize > 0 && m.ContextSize < 4096 {
		return false
	}

	// Exclude models with zero pricing info (likely not chat models)
	if m.InputTokenPricePerM == 0 && m.OutputTokenPricePerM == 0 {
		// Exception: free models have explicit $0 pricing
		if !strings.Contains(strings.ToLower(m.Description), "free") {
			return false
		}
	}

	return true
}

// canReason infers reasoning capability from model name/family.
func canReason(id string) bool {
	lower := strings.ToLower(id)
	reasoningFamilies := []string{
		"deepseek-r1",
		"deepseek-v4",
		"deepseek-v3.2",
		"deepseek-v3.1",
		"kimi-k2",
		"glm-5",
		"qwen3.",
		"qwen3-",
		"minimax-m",
		"step-3",
		"nemotron-3",
		"ring-2",
		"ling-2",
	}
	for _, family := range reasoningFamilies {
		if strings.Contains(lower, family) {
			return true
		}
	}
	return false
}

func supportsImages(id string) bool {
	lower := strings.ToLower(id)
	return strings.Contains(lower, "kimi-k2.6") ||
		strings.Contains(lower, "kimi-k2.5") ||
		strings.Contains(lower, "kimi-k2.7") ||
		strings.Contains(lower, "qwen3-vl") ||
		strings.Contains(lower, "glm-4.6v") ||
		strings.Contains(lower, "glm-4.5v") ||
		strings.Contains(lower, "ernie-4.5-vl") ||
		strings.Contains(lower, "gpt-oss") ||
		strings.Contains(lower, "llama-4")
}

func prettyName(title, id string) string {
	if title != "" {
		return title
	}
	return id
}

func buildModels(apiModels []NovitaModel) []catwalk.Model {
	var models []catwalk.Model
	for _, am := range apiModels {
		if !isTextModel(am) {
			continue
		}

		ctxWindow := am.ContextSize
		if ctxWindow == 0 {
			ctxWindow = 8_192
		}
		defaultMaxTokens := ctxWindow / 4

		canReasonVal := canReason(am.ID)
		var levels []string
		var reasoningEffort string
		if canReasonVal {
			levels = reasoningLevels
			reasoningEffort = "medium"
		}

		m := catwalk.Model{
			ID:                     am.ID,
			Name:                   prettyName(am.Title, am.ID),
			CostPer1MIn:            roundCost(am.InputTokenPricePerM),
			CostPer1MOut:           roundCost(am.OutputTokenPricePerM),
			CostPer1MInCached:      roundCost(am.CacheReadTokenPricePerM),
			CostPer1MOutCached:     0,
			ContextWindow:          ctxWindow,
			DefaultMaxTokens:       defaultMaxTokens,
			CanReason:              canReasonVal,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: reasoningEffort,
			SupportsImages:         supportsImages(am.ID),
		}

		models = append(models, m)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	return models
}

func main() {
	apiKey := os.Getenv("NOVITA_API_KEY")
	if apiKey == "" {
		log.Fatal("NOVITA_API_KEY environment variable is not set")
	}

	endpoint := os.Getenv("NOVITA_API_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.novita.ai/openai/v1"
	}

	modelsResp, err := fetchModels(endpoint, apiKey)
	if err != nil {
		log.Fatal("Error fetching Novita models:", err)
	}

	models := buildModels(modelsResp.Data)
	if len(models) == 0 {
		log.Fatal("No suitable models found in API response")
	}

	data, err := json.MarshalIndent(catwalk.Provider{
		Name:                "Novita",
		ID:                  catwalk.InferenceProviderNovita,
		APIKey:              "$NOVITA_API_KEY",
		APIEndpoint:         endpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek/deepseek-v4-pro",
		DefaultSmallModelID: "deepseek/deepseek-v4-flash",
		Models:              models,
	}, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Novita provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/novita.json", data, 0o600); err != nil {
		log.Fatal("Error writing Novita provider config:", err)
	}

	fmt.Printf("Generated novita.json with %d models\n", len(models))
}
