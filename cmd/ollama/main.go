// Package main provides a command-line tool to fetch models from a local
// Ollama instance and generate a configuration file for the provider.
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

// Ollama API types for /api/tags.

type OllamaDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type OllamaModel struct {
	Name       string        `json:"name"`
	Model      string        `json:"model"`
	ModifiedAt string        `json:"modified_at"`
	Size       int64         `json:"size"`
	Digest     string        `json:"digest"`
	Details    OllamaDetails `json:"details"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// Ollama API types for /api/show.

type OllamaShowResponse struct {
	Details      OllamaDetails    `json:"details"`
	ModelInfo    map[string]any   `json:"model_info"`
	Capabilities []string         `json:"capabilities"`
	Template     string           `json:"template"`
	System       string           `json:"system"`
	Parameters   string           `json:"parameters"`
}

// contextWindowKey returns the architecture-specific key for context length
// in the model_info map returned by /api/show.
func contextWindowKey(family string) string {
	return family + ".context_length"
}

// fetchOllamaTags retrieves the list of locally installed models.
func fetchOllamaTags(baseURL string) (*OllamaTagsResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("connecting to Ollama at %s: %w", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, body)
	}

	var tags OllamaTagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &tags, nil
}

// fetchOllamaShow retrieves detailed information about a specific model.
func fetchOllamaShow(baseURL, modelName string) (*OllamaShowResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	payload := fmt.Sprintf(`{"model":"%s"}`, modelName)
	resp, err := client.Post(
		baseURL+"/api/show",
		"application/json",
		strings.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching model info for %s: %w", modelName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, body)
	}

	var show OllamaShowResponse
	if err := json.Unmarshal(body, &show); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &show, nil
}

// contextWindowFromShow extracts the context window from /api/show response.
func contextWindowFromShow(show *OllamaShowResponse) int64 {
	key := contextWindowKey(show.Details.Family)
	if v, ok := show.ModelInfo[key]; ok {
		if f, ok := v.(float64); ok && f > 0 {
			return int64(f)
		}
	}
	return 0
}

// modelDisplayName derives a human-readable name from the model ID.
func modelDisplayName(id string) string {
	name := id
	if idx := strings.Index(name, ":"); idx != -1 {
		name = name[:idx]
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

// hasCapability checks if a model has a specific capability.
func hasCapability(caps []string, target string) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}

func main() {
	baseURL := "http://localhost:11434"

	ollamaProvider := catwalk.Provider{
		Name:                "Ollama",
		ID:                  "ollama",
		APIEndpoint:         baseURL + "/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "llama3.2",
		DefaultSmallModelID: "gemma3",
	}

	// Fetch locally installed models.
	tags, err := fetchOllamaTags(baseURL)
	if err != nil {
		log.Fatalf("Error fetching Ollama models: %v\n"+
			"Make sure Ollama is running: https://ollama.com/download", err)
	}

	if len(tags.Models) == 0 {
		log.Fatal("No models found. Pull a model first: ollama pull llama3.2")
	}

	fmt.Printf("Found %d locally installed models\n", len(tags.Models))

	for _, tag := range tags.Models {
		modelID := tag.Name

		// Fetch detailed info for context window and capabilities.
		show, err := fetchOllamaShow(baseURL, modelID)
		if err != nil {
			fmt.Printf("Warning: could not fetch info for %s: %v\n", modelID, err)
			continue
		}

		// Get context window from model metadata.
		ctx := contextWindowFromShow(show)
		if ctx == 0 {
			// Fallback based on model family.
			switch show.Details.Family {
			case "llama", "qwen2", "qwen3", "deepseek2", "gemma3", "mistral", "phi3", "command-r":
				ctx = 128000
			case "gemma2", "phi", "stablelm":
				ctx = 8192
			default:
				ctx = 8192
			}
		}

		supportsImages := hasCapability(show.Capabilities, "vision")
		canReason := show.Details.Family == "deepseek2" || show.Details.Family == "qwen3"

		// Use first model as default large if available.
		if ollamaProvider.DefaultLargeModelID == "llama3.2" {
			if !strings.HasPrefix(modelID, "llama3.2") {
				ollamaProvider.DefaultLargeModelID = modelID
			}
		}

		m := catwalk.Model{
			ID:               modelID,
			Name:             modelDisplayName(modelID),
			ContextWindow:    ctx,
			DefaultMaxTokens: ctx / 4,
			CanReason:        canReason,
			SupportsImages:   supportsImages,
		}

		ollamaProvider.Models = append(ollamaProvider.Models, m)
	}

	slices.SortFunc(ollamaProvider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(ollamaProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/ollama.json", data, 0o600); err != nil {
		log.Fatal("Error writing config:", err)
	}

	fmt.Printf("Generated ollama.json with %d models\n", len(ollamaProvider.Models))
}
