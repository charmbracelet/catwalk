// Package main provides a command-line tool to fetch models from OrcaRouter
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
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// APIModel represents a model from the OrcaRouter pricing API.
type APIModel struct {
	ModelName              string   `json:"model_name"`
	ModelRatio             float64  `json:"model_ratio"`
	CompletionRatio        float64  `json:"completion_ratio"`
	CacheRatio             float64  `json:"cache_ratio"`
	CreateCacheRatio       float64  `json:"create_cache_ratio"`
	ContextLength          int64    `json:"context_length"`
	MaxCompletionTokens    int64    `json:"max_completion_tokens"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types"`
	InputModalities        []string `json:"input_modalities"`
	OutputModalities       []string `json:"output_modalities"`
	SupportedParameters    []string `json:"supported_parameters"`
}

// PricingResponse is the response structure for the OrcaRouter pricing API.
type PricingResponse struct {
	Data    []APIModel `json:"data"`
	Success bool       `json:"success"`
}

const (
	pricingURL        = "https://www.orcarouter.ai/api/pricing"
	apiEndpoint       = "https://api.orcarouter.ai/v1"
	defaultLargeModel = "anthropic/claude-opus-4.8"
	defaultSmallModel = "google/gemini-3.5-flash"
	// quotaToUSD converts OrcaRouter's internal quota ratio to USD per 1M
	// tokens. See https://docs.orcarouter.ai for the pricing model.
	quotaToUSD = 2.0
	// defaultContextWindow is used when the pricing API does not report a
	// context_length for a model.
	defaultContextWindow = 128000
	minContextWindow     = 8192
	maxTokensFactor      = 10
)

// gpt5ProPattern matches OpenAI gpt-5(.X)-pro models, which are only served on
// the responses endpoint and not on chat completions.
var gpt5ProPattern = regexp.MustCompile(`openai/gpt-5(\.\d+)?-pro`)

func fetchOrcaRouterModels() (*PricingResponse, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		pricingURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var pr PricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &pr, nil
}

func contains(list []string, item string) bool {
	return slices.Contains(list, item)
}

// isChatLLM filters out non chat-completion models (image, video, embedding,
// TTS, rerank) and models that are only served on the responses or completions
// endpoints.
func isChatLLM(m APIModel) bool {
	name := strings.ToLower(m.ModelName)
	eps := m.SupportedEndpointTypes

	if contains(eps, "image-generation") || contains(eps, "openai-video") {
		return false
	}
	if contains(m.OutputModalities, "image") {
		return false
	}
	for _, k := range []string{"imagen", "dall-e", "gpt-image", "grok-imagine"} {
		if strings.Contains(name, k) {
			return false
		}
	}
	if strings.Contains(name, "embedding") ||
		strings.Contains(name, "tts") ||
		strings.HasSuffix(name, "-speech") {
		return false
	}
	for _, k := range []string{"whisper", "transcrib", "rerank"} {
		if strings.Contains(name, k) {
			return false
		}
	}
	// Responses-only models are not usable through chat completions.
	if contains(eps, "openai-response") && !contains(eps, "openai") {
		return false
	}
	// Codex and gpt-5-pro models use the completions / responses endpoints.
	if strings.Contains(name, "codex") {
		return false
	}
	if gpt5ProPattern.MatchString(name) {
		return false
	}
	return true
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func contextWindow(m APIModel) int64 {
	if m.ContextLength > 0 {
		return m.ContextLength
	}
	if m.MaxCompletionTokens > 0 {
		return m.MaxCompletionTokens
	}
	return defaultContextWindow
}

func calculateMaxTokens(contextWindow, maxOutput int64) int64 {
	if maxOutput == 0 || maxOutput > contextWindow/2 {
		return contextWindow / maxTokensFactor
	}
	return maxOutput
}

// reasoningExcludedVendors are upstreams whose reasoning models cannot be
// driven through Crush's openai-compat path (POST /v1/chat/completions with a
// flat `reasoning_effort` field):
//
//   - anthropic: rejects `reasoning_effort`; it expects Anthropic's native
//     `thinking` block, which the openai-compat path does not emit.
//   - openai: rejects `tools` + `reasoning_effort` together on chat completions
//     ("use /v1/responses instead"). Crush is agentic and always sends tools,
//     so reasoning and tool calls cannot be combined here.
//
// Models from these vendors are still served for regular chat and tool calls;
// they just don't advertise reasoning. Every other vendor (Gemini, Grok, Qwen,
// DeepSeek, MiniMax) accepts tools + reasoning_effort on chat completions.
var reasoningExcludedVendors = []string{"anthropic/", "openai/"}

func canReason(m APIModel) bool {
	for _, prefix := range reasoningExcludedVendors {
		if strings.HasPrefix(m.ModelName, prefix) {
			return false
		}
	}
	return contains(m.SupportedParameters, "reasoning") ||
		contains(m.SupportedParameters, "include_reasoning")
}

func reasoningConfig(reason bool) ([]string, string) {
	if !reason {
		return nil, ""
	}
	return []string{"low", "medium", "high"}, "medium"
}

// autoModel is OrcaRouter's adaptive router. It is a virtual model that is not
// returned by the pricing API, so we add it manually. Routing decisions (and
// therefore real cost, context, and capabilities) depend on the upstream the
// router selects; we use conservative values here.
func autoModel() catwalk.Model {
	return catwalk.Model{
		ID:               "orcarouter/auto",
		Name:             "OrcaRouter Auto (adaptive routing)",
		CostPer1MIn:      0,
		CostPer1MOut:     0,
		ContextWindow:    defaultContextWindow,
		DefaultMaxTokens: defaultContextWindow / maxTokensFactor,
		CanReason:        false,
		SupportsImages:   true,
	}
}

func main() {
	pricing, err := fetchOrcaRouterModels()
	if err != nil {
		log.Fatal("Error fetching OrcaRouter models:", err)
	}

	provider := catwalk.Provider{
		Name:                "OrcaRouter",
		ID:                  catwalk.InferenceProviderOrcaRouter,
		APIKey:              "$ORCAROUTER_API_KEY",
		APIEndpoint:         apiEndpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: defaultLargeModel,
		DefaultSmallModelID: defaultSmallModel,
		DefaultHeaders: map[string]string{
			"HTTP-Referer": "https://www.orcarouter.ai/",
			"X-Title":      "Crush",
		},
		Models: []catwalk.Model{autoModel()},
	}

	for _, m := range pricing.Data {
		if !isChatLLM(m) {
			continue
		}
		ctx := contextWindow(m)
		if ctx < minContextWindow {
			continue
		}

		reason := canReason(m)
		levels, defaultEffort := reasoningConfig(reason)

		var inCached, outCached float64
		if m.CacheRatio > 0 {
			outCached = roundCost(m.ModelRatio * m.CacheRatio * quotaToUSD)
		}
		if m.CreateCacheRatio > 0 {
			inCached = roundCost(m.ModelRatio * m.CreateCacheRatio * quotaToUSD)
		}

		provider.Models = append(provider.Models, catwalk.Model{
			ID:                     m.ModelName,
			Name:                   m.ModelName,
			CostPer1MIn:            roundCost(m.ModelRatio * quotaToUSD),
			CostPer1MOut:           roundCost(m.ModelRatio * m.CompletionRatio * quotaToUSD),
			CostPer1MInCached:      inCached,
			CostPer1MOutCached:     outCached,
			ContextWindow:          ctx,
			DefaultMaxTokens:       calculateMaxTokens(ctx, m.MaxCompletionTokens),
			CanReason:              reason,
			ReasoningLevels:        levels,
			DefaultReasoningEffort: defaultEffort,
			SupportsImages:         contains(m.InputModalities, "image"),
		})

		fmt.Printf("Added model %s (context window %d)\n", m.ModelName, ctx)
	}

	if len(provider.Models) <= 1 {
		log.Fatal("No models found or no models met the criteria")
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling OrcaRouter provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/orcarouter.json", data, 0o600); err != nil {
		log.Fatal("Error writing OrcaRouter provider config:", err)
	}

	fmt.Printf("\nSuccessfully wrote %d models to internal/providers/configs/orcarouter.json\n", len(provider.Models))
}
