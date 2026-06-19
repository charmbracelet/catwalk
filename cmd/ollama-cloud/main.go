// Package main provides a command-line tool to fetch models from Ollama Cloud
// and generate a configuration file for the provider.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type OllamaDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type OllamaShowResponse struct {
	Capabilities []string       `json:"capabilities"`
	Details      OllamaDetails  `json:"details"`
	ModelInfo    map[string]any `json:"model_info"`
	ModifiedAt   string         `json:"modified_at"`
}

type OllamaTagsResponse struct {
	Models []struct {
		Name    string        `json:"name"`
		Model   string        `json:"model"`
		Details OllamaDetails `json:"details"`
	} `json:"models"`
}

type ModelMeta struct {
	ContextWindow    int64
	DefaultMaxTokens int64
	CanReason        bool
	SupportsImages   bool
}

// defaultMaxTokens returns a sensible default output token limit for a model.
// It uses the context window as a cap but never exceeds common model limits.
func defaultMaxTokens(ctx int64) int64 {
	if ctx <= 0 {
		return 8192
	}
	if ctx >= 128000 {
		return 8192
	}
	if ctx >= 32000 {
		return 8192
	}
	if ctx >= 8192 {
		return 4096
	}
	return 4096
}

// contextWindowFromShow extracts the context window from the /api/show response.
// The model_info key is architecture-specific, e.g. "gptoss.context_length".
func contextWindowFromShow(show *OllamaShowResponse) int64 {
	family := show.Details.Family
	if family == "" {
		return 0
	}
	key := family + ".context_length"
	if v, ok := show.ModelInfo[key]; ok {
		if f, ok := v.(float64); ok && f > 0 {
			return int64(f)
		}
	}
	// Fallback: try to find any context_length key.
	for k, v := range show.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if f, ok := v.(float64); ok && f > 0 {
				return int64(f)
			}
		}
	}
	return 0
}

func modelMetaFromShow(show *OllamaShowResponse) ModelMeta {
	ctx := contextWindowFromShow(show)
	if ctx == 0 {
		ctx = 128000
	}

	canReason := false
	supportsImages := false
	for _, c := range show.Capabilities {
		switch c {
		case "thinking":
			canReason = true
		case "vision":
			supportsImages = true
		}
	}

	return ModelMeta{
		ContextWindow:    ctx,
		DefaultMaxTokens: defaultMaxTokens(ctx),
		CanReason:        canReason,
		SupportsImages:   supportsImages,
	}
}

func modelDisplayName(id string) string {
	name := id
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, ":", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

func fetchOllamaCloudModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiEndpoint + "/models")
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &mr, nil
}

func fetchOllamaCloudShow(apiEndpoint, modelID string) (*OllamaShowResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	payload := fmt.Sprintf(`{"model":"%s"}`, modelID)
	resp, err := client.Post(
		apiEndpoint+"/show",
		"application/json",
		strings.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching model info for %s: %w", modelID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var show OllamaShowResponse
	if err := json.Unmarshal(body, &show); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &show, nil
}

func main() {
	baseURL := "https://ollama.com"
	ollamaCloudProvider := catwalk.Provider{
		Name:                "Ollama Cloud",
		ID:                  "ollama-cloud",
		APIKey:              "$OLLAMA_API_KEY",
		APIEndpoint:         baseURL + "/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "gemma3:12b",
		DefaultSmallModelID: "gemma3:4b",
	}

	modelsResp, err := fetchOllamaCloudModels(ollamaCloudProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Ollama Cloud models:", err)
	}

	for _, model := range modelsResp.Data {
		show, err := fetchOllamaCloudShow(baseURL+"/api", model.ID)
		if err != nil {
			fmt.Printf("Warning: could not fetch info for %s: %v\n", model.ID, err)
			continue
		}

		meta := modelMetaFromShow(show)

		m := catwalk.Model{
			ID:               model.ID,
			Name:             modelDisplayName(model.ID),
			ContextWindow:    meta.ContextWindow,
			DefaultMaxTokens: meta.DefaultMaxTokens,
			CanReason:        meta.CanReason,
			SupportsImages:   meta.SupportsImages,
		}

		ollamaCloudProvider.Models = append(ollamaCloudProvider.Models, m)
	}

	slices.SortFunc(ollamaCloudProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(ollamaCloudProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/ollama-cloud.json", data, 0o600); err != nil {
		log.Fatal("Error writing config:", err)
	}

	fmt.Printf("Generated ollama-cloud.json with %d models\n", len(ollamaCloudProvider.Models))
}
