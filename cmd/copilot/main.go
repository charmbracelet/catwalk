// Package main implements a tool to fetch GitHub Copilot models and generate a Catwalk provider configuration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

type Response struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Version            string     `json:"version"`
	Vendor             string     `json:"vendor"`
	Preview            bool       `json:"preview"`
	ModelPickerEnabled bool       `json:"model_picker_enabled"`
	Capabilities       Capability `json:"capabilities"`
	Policy             *Policy    `json:"policy,omitempty"`
}

type Capability struct {
	Family    string   `json:"family"`
	Type      string   `json:"type"`
	Tokenizer string   `json:"tokenizer"`
	Limits    Limits   `json:"limits"`
	Supports  Supports `json:"supports"`
}

type Limits struct {
	MaxContextWindowTokens int `json:"max_context_window_tokens,omitempty"`
	MaxOutputTokens        int `json:"max_output_tokens,omitempty"`
	MaxPromptTokens        int `json:"max_prompt_tokens,omitempty"`
}

type Supports struct {
	ToolCalls         bool `json:"tool_calls,omitempty"`
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	MaxThinkingBudget int  `json:"max_thinking_budget,omitempty"`
	MinThinkingBudget int  `json:"min_thinking_budget,omitempty"`
}

type Policy struct {
	State string `json:"state"`
	Terms string `json:"terms"`
}

var versionedModelRegexp = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	copilotModels, err := fetchCopilotModels()
	if err != nil {
		return err
	}

	// NOTE(@andreynering): Exclude versioned models and keep only the main version of each.
	copilotModels = slices.DeleteFunc(copilotModels, func(m Model) bool {
		return m.ID != m.Version || versionedModelRegexp.MatchString(m.ID) || strings.Contains(m.ID, "embedding")
	})

	catwalkModels := modelsToCatwalk(copilotModels)
	slices.SortStableFunc(catwalkModels, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	provider := catwalk.Provider{
		ID:                  catwalk.InferenceProviderCopilot,
		Name:                "GitHub Copilot",
		Models:              catwalkModels,
		APIEndpoint:         "https://api.githubcopilot.com",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "claude-sonnet-4.5",
		DefaultSmallModelID: "claude-haiku-4.5",
	}
	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to marshal json: %w", err)
	}
	if err := os.WriteFile("internal/providers/configs/copilot.json", data, 0o600); err != nil {
		return fmt.Errorf("unable to write copilog.json: %w", err)
	}
	return nil
}

func fetchCopilotModels() ([]Model, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		"https://api.githubcopilot.com/models",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", copilotToken()))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to make http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	bts, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	// for debugging
	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/copilot-response.json", bts, 0o600)

	var data Response
	if err := json.Unmarshal(bts, &data); err != nil {
		return nil, fmt.Errorf("unable to unmarshal json: %w", err)
	}
	return data.Data, nil
}

func modelsToCatwalk(m []Model) []catwalk.Model {
	models := make([]catwalk.Model, 0, len(m))
	for _, model := range m {
		models = append(models, modelToCatwalk(model))
	}
	return models
}

func modelToCatwalk(m Model) catwalk.Model {
	canReason, reasoningLevels, defaultReasoning := detectReasoningCapabilities(m)
	supportsAttachments := detectAttachmentSupport(m)

	return catwalk.Model{
		ID:                     m.ID,
		Name:                   m.Name,
		DefaultMaxTokens:       int64(m.Capabilities.Limits.MaxOutputTokens),
		ContextWindow:          int64(m.Capabilities.Limits.MaxContextWindowTokens),
		CanReason:              canReason,
		ReasoningLevels:        reasoningLevels,
		DefaultReasoningEffort: defaultReasoning,
		SupportsImages:         supportsAttachments,
	}
}

func detectReasoningCapabilities(m Model) (canReason bool, levels []string, defaultLevel string) {
	// Claude models with reasoning support
	if m.ID == "claude-3.7-sonnet" ||
		m.ID == "claude-haiku-4.5" ||
		m.ID == "claude-opus-4.5" ||
		m.ID == "claude-sonnet-4" ||
		m.ID == "claude-sonnet-4.5" {
		return true, nil, ""
	}

	// Gemini models with reasoning support
	if strings.HasPrefix(m.ID, "gemini-2.5-") || strings.HasPrefix(m.ID, "gemini-3-") {
		return true, []string{"low", "medium", "high"}, "medium"
	}

	// GPT-5 series with reasoning levels
	if strings.HasPrefix(m.ID, "gpt-5") && !strings.Contains(m.ID, "chat") {
		return true, []string{"low", "medium", "high"}, "medium"
	}

	// OpenAI o-series with reasoning levels
	if strings.HasPrefix(m.ID, "o3-") || strings.HasPrefix(m.ID, "o4-") {
		return true, []string{"low", "medium", "high"}, "medium"
	}

	// DeepSeek R1 models
	if strings.HasPrefix(m.ID, "deepseek-r1") {
		return true, nil, ""
	}

	// Grok models with reasoning
	if m.ID == "grok-3-mini" || m.ID == "grok-3-mini-beta" ||
		strings.HasPrefix(m.ID, "grok-4") ||
		m.ID == "grok-code-fast-1" {
		return true, []string{"low", "medium", "high"}, "medium"
	}

	return false, nil, ""
}

func detectAttachmentSupport(m Model) bool {
	// Claude models support attachments (vision/multimodal)
	if strings.HasPrefix(m.ID, "claude-") {
		return true
	}

	// Gemini models support attachments (vision/multimodal)
	if strings.HasPrefix(m.ID, "gemini-") {
		return true
	}

	// GPT-5 models support attachments (based on OpenRouter pattern)
	if strings.HasPrefix(m.ID, "gpt-5") {
		return true
	}

	// Older GPT models do not support attachments
	if strings.HasPrefix(m.ID, "gpt-4") || strings.HasPrefix(m.ID, "gpt-3.5") {
		return false
	}

	// Grok models - only grok-4 supports attachments
	if m.ID == "grok-4" || strings.HasPrefix(m.ID, "grok-4-") {
		return true
	}

	return false
}

func copilotToken() string {
	if token := os.Getenv("COPILOT_TOKEN"); token != "" {
		return token
	}
	return tokenFromDisk()
}

func tokenFromDisk() string {
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		return ""
	}
	var content map[string]struct {
		User        string `json:"user"`
		OAuthToken  string `json:"oauth_token"`
		GitHubAppID string `json:"githubAppId"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return ""
	}
	if app, ok := content["github.com:Iv1.b507a08c87ecfe98"]; ok {
		return app.OAuthToken
	}
	return ""
}

func tokenFilePath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "github-copilot/apps.json")
	default:
		return filepath.Join(os.Getenv("HOME"), ".config/github-copilot/apps.json")
	}
}
