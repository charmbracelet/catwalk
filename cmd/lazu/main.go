// Package main provides a command-line tool to fetch models from Lazu
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

	"charm.land/catwalk/v3/pkg/catwalk"
)

const (
	lazuPricingURL    = "https://lazu.ai/api/pricing"
	minContextWindow  = 20000
	defaultLargeModel = "claude-sonnet-5-5"
	defaultSmallModel = "claude-haiku-5-5"
	maxTokensFallback = 16384
)

type lazuCapabilities struct {
	SupportsVision     bool `json:"supports_vision"`
	SupportsReasoning  bool `json:"supports_reasoning"`
	SupportsAudioInput bool `json:"supports_audio_input"`
}

type lazuReasoningOptions struct {
	Toggle  bool     `json:"toggle"`
	Efforts []string `json:"efforts"`
	Off     string   `json:"off"`
}

type lazuModel struct {
	ModelName              string               `json:"model_name"`
	QuotaType              int                  `json:"quota_type"`
	HasPrice               bool                 `json:"has_price"`
	ContextLength          int64                `json:"context_length"`
	MaxOutputTokens        int64                `json:"max_output_tokens"`
	InputPerMTok           float64              `json:"input_per_mtok"`
	OutputPerMTok          float64              `json:"output_per_mtok"`
	CacheReadPerMTok       float64              `json:"cache_read_per_mtok"`
	CacheWrite5mPerMTok    float64              `json:"cache_write_5m_per_mtok"`
	Capabilities           lazuCapabilities     `json:"capabilities"`
	ModalityInput          []string             `json:"modality_input"`
	ModalityOutput         []string             `json:"modality_output"`
	SupportedEndpointTypes []string             `json:"supported_endpoint_types"`
	ReasoningOptions       lazuReasoningOptions `json:"reasoning_options"`
}

type lazuPricingResponse struct {
	Data    []lazuModel `json:"data"`
	Success bool        `json:"success"`
}

func fetchLazuModels() ([]lazuModel, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, lazuPricingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Catwalk/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Lazu models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var parsed lazuPricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return parsed.Data, nil
}

func roundCost(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

func hasSupportedChatEndpoint(types []string) bool {
	for _, supported := range []string{"anthropic", "openai-response", "openai"} {
		if slices.Contains(types, supported) {
			return true
		}
	}
	return false
}

func isEligibleTextModel(m lazuModel) bool {
	if m.QuotaType != 0 || !m.HasPrice || m.ContextLength < minContextWindow {
		return false
	}
	if strings.HasSuffix(m.ModelName, "-decisions") {
		return false
	}
	if !hasSupportedChatEndpoint(m.SupportedEndpointTypes) {
		return false
	}
	if !slices.Contains(m.ModalityInput, "text") || !slices.Contains(m.ModalityOutput, "text") {
		return false
	}
	for _, excluded := range []string{"image-generation", "video-generation", "embedding", "embeddings", "rerank", "decisions"} {
		if slices.Contains(m.SupportedEndpointTypes, excluded) {
			return false
		}
	}
	return true
}

func endpointTypeForModel(m lazuModel) catwalk.Type {
	if slices.Contains(m.SupportedEndpointTypes, "anthropic") {
		return catwalk.TypeMessages
	}
	if slices.Contains(m.SupportedEndpointTypes, "openai-response") &&
		(strings.HasPrefix(m.ModelName, "gpt-") || !slices.Contains(m.SupportedEndpointTypes, "openai")) {
		return catwalk.TypeResponses
	}
	return ""
}

func titleWord(s string) string {
	switch strings.ToLower(s) {
	case "ai":
		return "AI"
	case "gpt":
		return "GPT"
	case "glm":
		return "GLM"
	case "deepseek":
		return "DeepSeek"
	case "minimax":
		return "MiniMax"
	case "qwen":
		return "Qwen"
	case "v2", "v3", "v4", "v5":
		return strings.ToUpper(s)
	}
	if len(s) == 0 {
		return s
	}
	if len(s) >= 2 && (s[0] == 'k' || s[0] == 'm' || s[0] == 'r' || s[0] == 'v') && s[1] >= '0' && s[1] <= '9' {
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func formatModelName(id string) string {
	switch {
	case strings.HasPrefix(id, "MiniMax-"):
		return id
	case strings.HasPrefix(id, "claude-"):
		parts := strings.Split(id, "-")
		if len(parts) >= 4 && len(parts[len(parts)-1]) == 8 && isDigits(parts[len(parts)-1]) {
			parts = parts[:len(parts)-1]
		}
		if len(parts) >= 4 && isDigits(parts[len(parts)-2]) && isDigits(parts[len(parts)-1]) {
			ver := parts[len(parts)-2] + "." + parts[len(parts)-1]
			parts = append(parts[:len(parts)-2], ver)
		}
		for i, p := range parts {
			parts[i] = titleWord(p)
		}
		return strings.Join(parts, " ")
	case strings.HasPrefix(id, "gpt-"):
		parts := strings.Split(id, "-")
		if len(parts) >= 2 {
			head := "GPT-" + parts[1]
			if len(parts) == 2 {
				return head
			}
			tail := make([]string, len(parts)-2)
			for i, p := range parts[2:] {
				tail[i] = titleWord(p)
			}
			return head + " " + strings.Join(tail, " ")
		}
	case strings.HasPrefix(id, "glm-"):
		parts := strings.Split(id, "-")
		if len(parts) >= 2 {
			head := "GLM-" + parts[1]
			if len(parts) == 2 {
				return head
			}
			tail := make([]string, len(parts)-2)
			for i, p := range parts[2:] {
				tail[i] = titleWord(p)
			}
			return head + " " + strings.Join(tail, " ")
		}
	case strings.HasPrefix(id, "qwen"):
		parts := strings.Split(id, "-")
		for i, p := range parts {
			if i == 0 && strings.HasPrefix(p, "qwen") {
				parts[i] = "Qwen" + strings.TrimPrefix(p, "qwen")
			} else {
				parts[i] = titleWord(p)
			}
		}
		return strings.Join(parts, "-")
	case strings.HasPrefix(id, "gemini-"):
		formatted := strings.ReplaceAll(id, "flash-lite", "Flash-Lite")
		parts := strings.Split(formatted, "-")
		out := make([]string, 0, len(parts))
		for i := 0; i < len(parts); i++ {
			if parts[i] == "Flash" && i+1 < len(parts) && parts[i+1] == "Lite" {
				out = append(out, "Flash-Lite")
				i++
				continue
			}
			out = append(out, titleWord(parts[i]))
		}
		return strings.Join(out, " ")
	}

	parts := strings.Split(id, "-")
	for i, p := range parts {
		parts[i] = titleWord(p)
	}
	return strings.Join(parts, " ")
}

func filterEffortLevels(raw []string) []string {
	allowed := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	levels := make([]string, 0, len(raw))
	for _, item := range raw {
		v := strings.ToLower(strings.TrimSpace(item))
		if slices.Contains(allowed, v) && !slices.Contains(levels, v) {
			levels = append(levels, v)
		}
	}
	return levels
}

func defaultEffortForLevels(levels []string) string {
	for _, preferred := range []string{"medium", "high", "low"} {
		if slices.Contains(levels, preferred) {
			return preferred
		}
	}
	return levels[0]
}

func buildReasoning(m lazuModel) catwalk.Reasoning {
	if !m.Capabilities.SupportsReasoning {
		return catwalk.Reasoning{Thinking: catwalk.ThinkingNever}
	}

	canDisable := m.ReasoningOptions.Toggle || slices.Contains(m.ReasoningOptions.Efforts, "none")
	if m.ReasoningOptions.Off != "" {
		canDisable = m.ReasoningOptions.Off != "never"
	}

	thinking := catwalk.ThinkingAlways
	if canDisable {
		thinking = catwalk.ThinkingToggleable
	}

	levels := filterEffortLevels(m.ReasoningOptions.Efforts)
	if len(levels) == 0 {
		return catwalk.Reasoning{Thinking: thinking}
	}
	return catwalk.Reasoning{
		Thinking:           thinking,
		EffortLevels:       catwalk.NewEffortLevels(levels...),
		DefaultEffortLevel: defaultEffortForLevels(levels),
	}
}

func main() {
	rawModels, err := fetchLazuModels()
	if err != nil {
		log.Fatal("Error fetching Lazu models: ", err)
	}

	provider := catwalk.Provider{
		Name:                  "Lazu",
		ID:                    catwalk.InferenceProviderLazu,
		APIKey:                "$LAZU_API_KEY",
		APIEndpoint:           "https://api.lazu.ai/v1",
		Type:                  catwalk.TypeCompletions,
		SessionAffinityHeader: "x-session-id",
		DefaultLargeModelID:   defaultLargeModel,
		DefaultSmallModelID:   defaultSmallModel,
		Models:                make([]catwalk.Model, 0, len(rawModels)),
	}

	for _, m := range rawModels {
		if !isEligibleTextModel(m) {
			continue
		}

		maxTokens := m.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = maxTokensFallback
		}

		provider.Models = append(provider.Models, catwalk.Model{
			ID:   m.ModelName,
			Type: endpointTypeForModel(m),
			Name: formatModelName(m.ModelName),
			Pricing: catwalk.Pricing{
				Input:       roundCost(m.InputPerMTok),
				Output:      roundCost(m.OutputPerMTok),
				CacheCreate: roundCost(m.CacheWrite5mPerMTok),
				CacheHit:    roundCost(m.CacheReadPerMTok),
			},
			ContextWindow:    m.ContextLength,
			DefaultMaxTokens: maxTokens,
			Reasoning:        buildReasoning(m),
			Capabilities: catwalk.Capabilities{
				Vision: m.Capabilities.SupportsVision,
				Audio:  m.Capabilities.SupportsAudioInput,
			},
		})
	}

	if len(provider.Models) == 0 {
		log.Fatal("No eligible Lazu models found")
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Lazu provider: ", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/lazu.json", data, 0o600); err != nil {
		log.Fatal("Error writing Lazu provider config: ", err)
	}

	fmt.Printf("Generated lazu.json with %d models\n", len(provider.Models))
}
