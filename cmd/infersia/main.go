// Package main provides a command-line tool to fetch models from Infersia
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
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// Model represents a model from the Infersia API. The feed is
// OpenRouter-shaped, so per-token prices arrive as strings.
type Model struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	ContextLength       int64        `json:"context_length"`
	MaxCompletionTokens int64        `json:"max_completion_tokens"`
	Pricing             Pricing      `json:"pricing"`
	Architecture        Architecture `json:"architecture"`
	SupportedParameters []string     `json:"supported_parameters"`
}

// Pricing contains per-token prices as decimal strings.
type Pricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

// Architecture describes the modalities a model accepts.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// ModelsResponse is the response structure for the Infersia models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

// reasoningLevels holds the effort values that were measured to change
// behaviour on a given model. Infersia's /v1/models advertises that
// reasoning_effort is accepted but not which values are meaningful, and
// several models accept the full OpenAI set while treating most of it as
// inert. Listing only the levels that do something keeps Crush from
// offering a choice that has no effect.
var reasoningLevels = map[string][]string{
	"deepseek/deepseek-v4-flash-0731": {"none", "max"},
}

// defaultReasoningEffort is the level a model starts at. DeepSeek V4 Flash
// ships with thinking disabled, so "none" matches the endpoint's own default
// rather than opting every Crush user into slower, costlier responses.
var defaultReasoningEffort = map[string]string{
	"deepseek/deepseek-v4-flash-0731": "none",
}

func fetchInfersiaModels() (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.infersia.com/v1/models",
		nil,
	)
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

// perMillion converts a per-token price string to dollars per million tokens.
// An unparseable or absent price yields 0 rather than failing the run, which
// matches how the other generators treat missing fields.
//
// The result is rounded to six decimal places purely to strip binary
// floating-point residue: 0.00000005 * 1e6 is 0.049999999999999996, which
// serialises verbatim and reads as noise. Six places is deliberate — the
// cheapest cached-input rate here is $0.006/M and rounding to two would
// report $0.05/M as $0.05 but $0.035/M as $0.04, a 14% overstatement.
func perMillion(price string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(price), 64)
	if err != nil {
		return 0
	}
	return math.Round(v*1_000_000*1e6) / 1e6
}

func main() {
	modelsResp, err := fetchInfersiaModels()
	if err != nil {
		log.Fatal("Error fetching Infersia models:", err)
	}

	infersiaProvider := catwalk.Provider{
		Name:                "Infersia",
		ID:                  catwalk.InferenceProviderInfersia,
		APIKey:              "$INFERSIA_API_KEY",
		APIEndpoint:         "https://api.infersia.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "deepseek/deepseek-v4-flash-0731",
		DefaultSmallModelID: "qwen/qwen3-8b",
		Models:              []catwalk.Model{},
	}

	for _, model := range modelsResp.Data {
		// The ":free" variants are rate-limited rather than differently
		// capable, and listing both would show the same model twice.
		if strings.HasSuffix(model.ID, ":free") {
			continue
		}

		// Crush sends chat completions, so a model belongs in this list only
		// if it takes text in AND returns text out. Both sides are load-bearing.
		//
		// Testing the output alone was not enough. It correctly rejected the
		// rerankers this filter was written for, but speech-to-text is
		// audio->text: its output modality IS text, so it passed, and Crush
		// would have listed a transcription model, sent it a chat completion
		// and got a 404 — with a context window of 448, which is a decoder-token
		// ceiling for one 30-second audio chunk and has nothing to do with a
		// prompt.
		//
		// A two-sided test rather than a skip-list of known-bad modalities: a
		// skip-list is what let this through, and it fails open on whatever
		// modality comes next.
		if !slices.Contains(model.Architecture.InputModalities, "text") ||
			!slices.Contains(model.Architecture.OutputModalities, "text") {
			continue
		}

		canReason := slices.Contains(model.SupportedParameters, "reasoning_effort")

		m := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            perMillion(model.Pricing.Prompt),
			CostPer1MOut:           perMillion(model.Pricing.Completion),
			CostPer1MInCached:      perMillion(model.Pricing.InputCacheRead),
			CostPer1MOutCached:     0,
			ContextWindow:          model.ContextLength,
			DefaultMaxTokens:       model.MaxCompletionTokens,
			CanReason:              canReason,
			ReasoningLevels:        reasoningLevels[model.ID],
			DefaultReasoningEffort: defaultReasoningEffort[model.ID],
			SupportsImages:         slices.Contains(model.Architecture.InputModalities, "image"),
		}

		infersiaProvider.Models = append(infersiaProvider.Models, m)
	}

	slices.SortFunc(infersiaProvider.Models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/infersia.json
	data, err := json.MarshalIndent(infersiaProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Infersia provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/infersia.json", data, 0o600); err != nil {
		log.Fatal("Error writing Infersia provider config:", err)
	}

	fmt.Printf("Generated infersia.json with %d models\n", len(infersiaProvider.Models))
}
