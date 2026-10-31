// Package main provides a command-line tool to fetch models from the DotHoosh
// gateway and generate a configuration file for the provider.
//
// The public, keyless model list holds the models callable on the OpenAI
// routes, with OpenRouter-standard pricing, limits, modalities and
// supported_parameters. Claude models are served only on /anthropic/v1/messages,
// and catwalk's openai-compat provider has a single type and base URL, so they
// are not part of this provider. Models that list /v1/responses are skipped for
// the same reason: openai-compat speaks Chat Completions, and the GPT-6 models
// admit function tools on Chat Completions only with reasoning off or not at all,
// so tool use needs the Responses route.
//
// The Iran-hosted API (gateway.dothoosh.com) is not reachable from outside Iran, GitHub
// runners included, so the list is read from the public mirror of the same catalog at
// www.dothoosh.com/gateway. Only ids, limits, prices, modalities and capability flags are
// copied, never the catalog version or verification timestamps, which rotate.
package main

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	modelsEndpoint = "https://www.dothoosh.com/gateway/v1/models"
	apiEndpoint    = "https://gateway.dothoosh.com/v1"
	outputFile     = "internal/providers/configs/dothoosh.json"

	// A truncated or empty answer must never replace a good catalog.
	minModels = 3

	chatProtocol      = "openai-chat-completions"
	responsesProtocol = "openai-responses"
)

// noEffortControl lists the models whose reasoning_effort the gateway does not take: Kimi K2.6
// has an on/off thinking toggle only, and Kimi K2.7 Code always thinks (gateway
// docs/runbooks/model-offering.md, Moonshot row; the listing carries no effort values). Catwalk
// offers them as reasoning models without levels, so no effort is ever sent. The listing cannot
// say this, so the exception is inherent to the generator, like the effort values themselves.
var noEffortControl = []string{"kimi-k2.6", "kimi-k2.7-code"}

// Model is one row of the gateway's model list.
type Model struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	ContextLength   int64        `json:"context_length"`
	ContextWindow   int64        `json:"context_window"`
	MaxOutputTokens int64        `json:"max_output_tokens"`
	Architecture    Architecture `json:"architecture"`
	Pricing         Pricing      `json:"pricing"`
	TopProvider     TopProvider  `json:"top_provider"`
	SupportedParams []string     `json:"supported_parameters"`
	Protocols       []Protocol   `json:"protocols"`
}

// Architecture defines the model's modalities.
type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// Pricing is USD per single token, as decimal strings.
type Pricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// TopProvider describes the serving limits.
type TopProvider struct {
	ContextLength       int64  `json:"context_length"`
	MaxCompletionTokens *int64 `json:"max_completion_tokens"`
}

// Protocol is an API a model is served on.
type Protocol struct {
	Protocol string `json:"protocol"`
}

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Data []Model `json:"data"`
}

// roundCost keeps micro-dollars per million tokens, the listing's own precision
// (0.000000385001 per token is 0.385001); coarser rounding changes real prices.
func roundCost(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// perMillion converts a per-token USD string to USD per 1M tokens.
func perMillion(price string) (float64, error) {
	if price == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(price, 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("invalid price %q", price)
	}
	return roundCost(v * 1_000_000), nil
}

func fetchModels(ctx context.Context, client *http.Client, url string) (*ModelsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read models response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	// for debugging
	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/dothoosh-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

// servedOnChat reports whether a model can be called on Chat Completions. A row
// that does not state its protocols is taken to be on the list's own route.
func servedOnChat(m Model) bool {
	if len(m.Protocols) == 0 {
		return true
	}
	return slices.ContainsFunc(m.Protocols, func(p Protocol) bool { return p.Protocol == chatProtocol })
}

// listsResponses reports whether a model is also served on the Responses API. Such a model is
// Responses-first, and this chat-only provider does not offer it.
func listsResponses(m Model) bool {
	return slices.ContainsFunc(m.Protocols, func(p Protocol) bool { return p.Protocol == responsesProtocol })
}

func toCatwalkModel(m Model) (catwalk.Model, error) {
	// A served model always has prompt and completion prices; a row without them is broken,
	// and must not be written as a free model.
	if m.Pricing.Prompt == "" || m.Pricing.Completion == "" {
		return catwalk.Model{}, fmt.Errorf("%s: missing prompt or completion price", m.ID)
	}
	in, err := perMillion(m.Pricing.Prompt)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("%s: %w", m.ID, err)
	}
	out, err := perMillion(m.Pricing.Completion)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("%s: %w", m.ID, err)
	}
	cacheWrite, err := perMillion(m.Pricing.InputCacheWrite)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("%s: %w", m.ID, err)
	}
	cacheRead, err := perMillion(m.Pricing.InputCacheRead)
	if err != nil {
		return catwalk.Model{}, fmt.Errorf("%s: %w", m.ID, err)
	}

	contextWindow := m.ContextLength
	if contextWindow == 0 {
		contextWindow = m.TopProvider.ContextLength
	}
	if contextWindow == 0 {
		contextWindow = m.ContextWindow
	}

	// Each field is checked on its own: a 0 or null in the first must not hide a real value in
	// the next.
	maxOutput := int64(0)
	if m.TopProvider.MaxCompletionTokens != nil && *m.TopProvider.MaxCompletionTokens > 0 {
		maxOutput = *m.TopProvider.MaxCompletionTokens
	} else if m.MaxOutputTokens > 0 {
		maxOutput = m.MaxOutputTokens
	}
	defaultMaxTokens := contextWindow / 10
	if maxOutput > 0 {
		defaultMaxTokens = maxOutput / 2
	}

	name := m.Name
	if name == "" {
		name = m.ID
	}

	canReason := slices.Contains(m.SupportedParams, "reasoning")
	var reasoningLevels []string
	var defaultReasoning string
	if canReason && !slices.Contains(noEffortControl, m.ID) {
		// The list does not carry a model's effort values. low and high are
		// accepted by every reasoning model that takes an effort at all.
		reasoningLevels = []string{"low", "high"}
		defaultReasoning = "high"
	}

	return catwalk.Model{
		ID:                     m.ID,
		Name:                   name,
		CostPer1MIn:            in,
		CostPer1MOut:           out,
		CostPer1MInCached:      cacheWrite,
		CostPer1MOutCached:     cacheRead,
		ContextWindow:          contextWindow,
		DefaultMaxTokens:       defaultMaxTokens,
		CanReason:              canReason,
		DefaultReasoningEffort: defaultReasoning,
		ReasoningLevels:        reasoningLevels,
		SupportsImages:         slices.Contains(m.Architecture.InputModalities, "image"),
	}, nil
}

// buildProvider turns the model list into the provider config. The result is
// deterministic: models sorted by name then ID, defaults chosen by price.
func buildProvider(resp *ModelsResponse) (catwalk.Provider, error) {
	if resp == nil || len(resp.Data) < minModels {
		n := 0
		if resp != nil {
			n = len(resp.Data)
		}
		return catwalk.Provider{}, fmt.Errorf("gateway returned %d models, below the %d needed to trust the list", n, minModels)
	}

	models := []catwalk.Model{}
	seen := map[string]bool{}
	for _, m := range resp.Data {
		if m.ID != "" {
			if seen[m.ID] {
				return catwalk.Provider{}, fmt.Errorf("duplicate model id %q in the gateway list", m.ID)
			}
			seen[m.ID] = true
		}
		if m.ID == "" || !servedOnChat(m) || listsResponses(m) {
			continue
		}
		// skip non-text models or those without tools
		if !slices.Contains(m.SupportedParams, "tools") ||
			!slices.Contains(m.Architecture.InputModalities, "text") ||
			!slices.Contains(m.Architecture.OutputModalities, "text") {
			continue
		}
		model, err := toCatwalkModel(m)
		if err != nil {
			return catwalk.Provider{}, err
		}
		if model.ContextWindow <= 0 {
			continue
		}
		models = append(models, model)
	}
	// The same floor applies to what is left after filtering: a degraded answer that keeps the
	// rows but loses their capability flags must not shrink the catalog.
	if len(models) < minModels {
		return catwalk.Provider{}, fmt.Errorf("only %d chat models with tool support, below the %d needed to trust the list", len(models), minModels)
	}

	slices.SortFunc(models, func(a, b catwalk.Model) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	large, small := defaultModels(models)
	return catwalk.Provider{
		Name:                "DotHoosh",
		ID:                  catwalk.InferenceProviderDotHoosh,
		APIKey:              "$DOTHOOSH_API_KEY",
		APIEndpoint:         apiEndpoint,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: large,
		DefaultSmallModelID: small,
		Models:              models,
	}, nil
}

// previousModelIDs returns the model ids of the checked-in catalog, the last good one. A missing
// file means there is nothing to protect; an unreadable one is an error rather than a skipped guard.
func previousModelIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the previous catalog: %w", err)
	}
	var previous catwalk.Provider
	if err := json.Unmarshal(data, &previous); err != nil {
		return nil, fmt.Errorf("the previous catalog %s is not valid JSON: %w", path, err)
	}
	ids := make([]string, 0, len(previous.Models))
	for _, m := range previous.Models {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// checkRetirement refuses a list that would drop more than a third of the previous catalog at
// once: that is a truncated or degraded answer, not a retirement. Retiring a model or two is
// routine; a real mass retirement is done by editing the file by hand.
func checkRetirement(previous, ids []string) error {
	var retired []string
	for _, id := range previous {
		if !slices.Contains(ids, id) {
			retired = append(retired, id)
		}
	}
	if len(retired)*3 > len(previous) {
		return fmt.Errorf("the list would retire %d of %d models (%s); treating it as truncated, edit %s by hand if this is intended",
			len(retired), len(previous), strings.Join(retired, ", "), outputFile)
	}
	return nil
}

// defaultModels picks the most and least expensive model by output price (ties
// break on ID), so a new model reaches the defaults without a code change.
func defaultModels(models []catwalk.Model) (large, small string) {
	best, cheapest := models[0], models[0]
	for _, m := range models[1:] {
		if m.CostPer1MOut > best.CostPer1MOut || (m.CostPer1MOut == best.CostPer1MOut && m.ID < best.ID) {
			best = m
		}
		if m.CostPer1MOut < cheapest.CostPer1MOut || (m.CostPer1MOut == cheapest.CostPer1MOut && m.ID < cheapest.ID) {
			cheapest = m
		}
	}
	return best.ID, cheapest.ID
}

// This is used to generate the dothoosh.json config file.
func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	modelsResp, err := fetchModels(context.Background(), client, modelsEndpoint)
	if err != nil {
		log.Fatal("Error fetching DotHoosh models:", err)
	}

	provider, err := buildProvider(modelsResp)
	if err != nil {
		log.Fatal("Error building DotHoosh provider:", err)
	}

	previous, err := previousModelIDs(outputFile)
	if err != nil {
		log.Fatal("Error reading the previous DotHoosh catalog:", err)
	}
	ids := make([]string, 0, len(provider.Models))
	for _, m := range provider.Models {
		ids = append(ids, m.ID)
	}
	if err := checkRetirement(previous, ids); err != nil {
		log.Fatal("Error checking the DotHoosh list against the previous catalog:", err)
	}

	// save the json in internal/providers/configs/dothoosh.json
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling DotHoosh provider:", err)
	}
	if err := os.WriteFile(outputFile, data, 0o600); err != nil {
		log.Fatal("Error writing DotHoosh provider config:", err)
	}
}
