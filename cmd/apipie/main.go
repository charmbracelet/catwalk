// Package main provides a command-line tool to fetch models from APIpie
// and generate a configuration file for the provider.
//
// LLM-Enhanced Display Names:
// This tool uses APIpie.ai's LLM service to generate professional display names
// for models based on their IDs and descriptions. The API key is donated to
// improve the user experience of this open source project.
//
// API Key Configuration:
// Set APIPIE_DISPLAY_NAME_API_KEY environment variable to enable LLM-generated
// display names. This should be set in GitHub Actions secrets.
//
// Fallback Behavior:
// If the APIpie API key is not working or not provided, the tool will fall back
// to using the raw model ID as the display name. This ensures the tool never
// breaks due to API issues.
//
// Example usage:
//
//	export APIPIE_DISPLAY_NAME_API_KEY="your-apipie-api-key"
//	go run cmd/apipie/main.go
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// retryableHTTPRequest performs an HTTP request with exponential backoff retry for 502 errors
func retryableHTTPRequest(req *http.Request, operation string) (*http.Response, error) {
	maxRetries := 3
	baseDelay := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		client := &http.Client{Timeout: 30 * time.Second}

		resp, err := client.Do(req)
		if err != nil {
			if attempt == maxRetries-1 {
				return nil, fmt.Errorf("%s failed after %d retries: %w", operation, maxRetries, err)
			}
			delay := baseDelay * time.Duration(1<<attempt)
			log.Printf("%s failed, retrying in %v (attempt %d/%d): %v", operation, delay, attempt+1, maxRetries, err)
			time.Sleep(delay)
			continue
		}

		// Success or non-retryable error
		if resp.StatusCode == 200 || resp.StatusCode != 502 {
			return resp, nil
		}

		// 502 error - retry with backoff
		if attempt == maxRetries-1 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("%s returned status %d after %d retries: %s", operation, resp.StatusCode, maxRetries, string(body))
		}

		resp.Body.Close()
		delay := baseDelay * time.Duration(1<<attempt)
		log.Printf("%s returned 502, retrying in %v (attempt %d/%d)", operation, delay, attempt+1, maxRetries)
		time.Sleep(delay)
	}

	return nil, fmt.Errorf("%s max retries exceeded", operation)
}

// Model represents the complete model configuration from APIpie detailed endpoint.
type Model struct {
	ID                string   `json:"id"`
	Model             string   `json:"model"`
	Route             string   `json:"route,omitempty"`
	Description       string   `json:"description,omitempty"`
	MaxTokens         int64    `json:"max_tokens,omitempty"`
	MaxResponseTokens int64    `json:"max_response_tokens,omitempty"`
	Type              string   `json:"type,omitempty"`
	Subtype           string   `json:"subtype,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Pool              string   `json:"pool,omitempty"`
	InstructType      string   `json:"instruct_type,omitempty"`
	Quantization      string   `json:"quantization,omitempty"`
	Enabled           int      `json:"enabled,omitempty"`
	Available         int      `json:"available,omitempty"`
	InputModalities   []string `json:"input_modalities,omitempty"`
	OutputModalities  []string `json:"output_modalities,omitempty"`
	Pricing           struct {
		Confirmed struct {
			InputCost  string `json:"input_cost"`
			OutputCost string `json:"output_cost"`
		} `json:"confirmed"`
		Advertised struct {
			InputCostPerToken  string `json:"input_cost_per_token"`
			OutputCostPerToken string `json:"output_cost_per_token"`
			InternalReasoning  string `json:"internal_reasoning"`
		} `json:"advertised"`
	} `json:"pricing"`
}

// ModelsResponse is the response structure for the models API.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func fetchAPIpieModels() (*ModelsResponse, error) {
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://apipie.ai/v1/models/detailed",
		nil,
	)
	req.Header.Set("User-Agent", "Catwalk-Client/1.0")

	// Try to use API key if available
	if apiKey := os.Getenv("APIPIE_API_KEY"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	resp, err := retryableHTTPRequest(req, "Model fetch")
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

func isTextModel(model Model) bool {
	// Check if model is enabled, available, and is an LLM
	return model.Enabled == 1 && model.Available == 1 && model.Type == "llm"
}

func supportsImages(model Model) bool {
	// Check if input modalities include image support
	return slices.Contains(model.InputModalities, "image") ||
		strings.Contains(model.Subtype, "multimodal") ||
		strings.Contains(model.Subtype, "vision") ||
		strings.Contains(strings.ToLower(model.Description), "vision") ||
		strings.Contains(strings.ToLower(model.Description), "image")
}

func canReason(model Model) bool {
	// Check if model has reasoning capabilities based on subtype field
	if model.Subtype != "" {
		return strings.Contains(model.Subtype, "reasoning")
	}

	return false
}

func hasReasoningEfforts(cache *Cache, model Model) bool {
	// Only analyze models that can reason (have "reasoning" in subtype)
	if !canReason(model) {
		return false
	}

	// For reasoning models, determine if they support controllable reasoning depth
	if model.Description != "" {
		// Primary approach: LLM analysis of description field with caching
		if hasEffort, found := cache.GetReasoningEffort(model.Description); found {
			return hasEffort
		}

		// Cache miss - analyze with LLM
		result := analyzeReasoningEffortsWithLLM(model.Description)

		// Cache the result (both positive and negative)
		if err := cache.SetReasoningEffort(model.Description, result); err != nil {
			log.Printf("Failed to cache reasoning effort for model %s: %v", model.ID, err)
		}

		// If LLM analysis succeeded, return result
		if result {
			return true
		}
	}

	// Fallback: static phrase matching for controllable reasoning indicators
	if model.Description != "" {
		desc := strings.ToLower(model.Description)

		// Common phrases that indicate CONTROLLABLE reasoning depth
		controllableReasoningIndicators := []string{
			"thinking tokens", "reasoning budget", "controllable reasoning",
			"thinking depth", "reasoning depth", "controllable depth",
			"thinking budget", "reasoning effort", "configurable reasoning",
			"adjustable reasoning",
		}

		for _, indicator := range controllableReasoningIndicators {
			if strings.Contains(desc, indicator) {
				return true
			}
		}
	}

	return false
}

// analyzeReasoningEffortsWithLLM uses APIpie.ai to determine if a model
// supports controllable reasoning efforts based on its description
func analyzeReasoningEffortsWithLLM(description string) bool {
	// Use dedicated API key (same as for display name generation, donated for this project)
	apiKey := os.Getenv("APIPIE_DISPLAY_NAME_API_KEY")
	if apiKey == "" {
		return false // Fallback to false if no API key
	}

	// Create a focused prompt for controllable reasoning effort detection
	prompt := fmt.Sprintf(`You are an AI model capability analyzer. Determine if this model supports controllable reasoning effort/depth.

Look for indicators that users can control HOW MUCH reasoning the model does, such as:
- Thinking token budgets/limits
- Controllable reasoning depth
- Adjustable thinking effort
- Reasoning parameter control
- Step-by-step thinking control
- Configurable reasoning modes

Description: "%s"

Answer only "YES" if the model clearly supports controllable reasoning effort, or "NO" if it doesn't or if unclear.`, strings.Split(description, "\n")[0])

	reqBody := APIpieRequest{
		Messages: []APIpieMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Model:       "claude-sonnet-4-5",
		MaxTokens:   10,
		Temperature: 0.1, // Low temperature for consistent results
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Failed to marshal reasoning effort analysis request: %v", err)
		return false
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		"https://apipie.ai/v1/chat/completions",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		log.Printf("Failed to create reasoning effort analysis request: %v", err)
		return false
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := retryableHTTPRequest(req, "Reasoning effort analysis")
	if err != nil {
		log.Printf("Reasoning effort analysis failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Reasoning effort analysis returned status %d: %s", resp.StatusCode, string(body))
		return false
	}

	var apipieResp APIpieResponse
	if err := json.NewDecoder(resp.Body).Decode(&apipieResp); err != nil {
		log.Printf("Failed to decode reasoning effort analysis response: %v", err)
		return false
	}

	if len(apipieResp.Choices) == 0 {
		log.Printf("Reasoning effort analysis returned empty choices")
		return false
	}

	// Parse the response
	response := strings.TrimSpace(strings.ToUpper(apipieResp.Choices[0].Message.Content))
	return strings.Contains(response, "YES")
}

// APIpieRequest represents a request to the APIpie chat completions API
type APIpieRequest struct {
	Messages    []APIpieMessage `json:"messages"`
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
}

// APIpieMessage represents a message in the APIpie API request
type APIpieMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// APIpieResponse represents a response from the APIpie API
type APIpieResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// generateDisplayNamesForGroup uses APIpie.ai to generate professional display names
// for a group of models with the same ID, helping users differentiate between variants.
func generateDisplayNamesForGroup(models []Model) map[string]string {
	// Use dedicated API key for display name generation (donated for this project)
	apiKey := os.Getenv("APIPIE_DISPLAY_NAME_API_KEY")
	if apiKey == "" {
		return nil
	}

	// Build enhanced prompt for multiple variants
	prompt := `You are a model naming expert. Generate professional display names for AI models that help users differentiate between variants.

MODELS TO NAME:
`
	for i, model := range models {
		// Format modalities for display
		inputMods := strings.Join(model.InputModalities, ", ")
		if inputMods == "" {
			inputMods = "text"
		}
		outputMods := strings.Join(model.OutputModalities, ", ")
		if outputMods == "" {
			outputMods = "text"
		}

		// Format context window
		contextInfo := ""
		if model.MaxTokens > 0 {
			if model.MaxTokens >= 1000000 {
				contextInfo = fmt.Sprintf(" (%dM tokens)", model.MaxTokens/1000000)
			} else if model.MaxTokens >= 1000 {
				contextInfo = fmt.Sprintf(" (%dK tokens)", model.MaxTokens/1000)
			} else {
				contextInfo = fmt.Sprintf(" (%d tokens)", model.MaxTokens)
			}
		}

		prompt += fmt.Sprintf(`[%d] Model ID: "%s"
    Base Model: "%s"
    Provider: "%s"
    Route: "%s"
    Pool: "%s"
    Subtype: "%s"
    Input Modalities: %s
    Output Modalities: %s
    Context Window: %s
    Description: "%s"

`, i+1, model.ID, model.Model, model.Provider, model.Route, model.Pool, model.Subtype,
			inputMods, outputMods, strings.TrimSpace(contextInfo), strings.Split(model.Description, "\n")[0])
	}

	prompt += `NAMING RULES:
1. If one model has provider="pool", give it the simple canonical name (this is the meta-model)
2. For provider-specific variants, add provider name: "GPT-4 (OpenAI)", "GPT-4 (Azure)"
3. For multimodal variants, highlight capabilities: "GPT-4 Vision", "Claude 3.5 Sonnet (Vision)", "Gemini Pro (Audio)"
4. For context window differences, include size when significant: "Claude 3.5 Sonnet (200K)", "GPT-4 Turbo (128K)"
5. For feature variants, highlight differences: "GPT-4 Turbo", "Llama 3.1 Instruct", "Mistral 7B (Quantized)"
6. Keep names under 50 characters
7. Use proper capitalization and formatting
8. Make differences clear and concise
9. Prioritize: modalities > provider > context size > other features

Generate names in this exact format (one per line):
[1] -> Display Name Here
[2] -> Display Name Here
etc.`

	reqBody := APIpieRequest{
		Messages: []APIpieMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Model:       "claude-sonnet-4-5",
		MaxTokens:   300,
		Temperature: 0.1, // Low temperature for consistent results
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Failed to marshal APIpie request for group display name generation: %v", err)
		return nil
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		"https://apipie.ai/v1/chat/completions",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		log.Printf("Failed to create APIpie request for group display name generation: %v", err)
		return nil
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := retryableHTTPRequest(req, "Group display name generation")
	if err != nil {
		log.Printf("APIpie API failed for group display name generation: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("APIpie API returned status %d for group display name generation: %s", resp.StatusCode, string(body))
		return nil
	}

	var apipieResp APIpieResponse
	if err := json.NewDecoder(resp.Body).Decode(&apipieResp); err != nil {
		log.Printf("Failed to decode APIpie response for group display name generation: %v", err)
		return nil
	}

	if len(apipieResp.Choices) == 0 {
		log.Printf("APIpie returned empty choices for group display name generation")
		return nil
	}

	// Parse the response to extract names
	response := strings.TrimSpace(apipieResp.Choices[0].Message.Content)
	return parseGroupNamesResponse(response, models)
}

// parseGroupNamesResponse parses the LLM response and maps names to models
func parseGroupNamesResponse(response string, models []Model) map[string]string {
	lines := strings.Split(response, "\n")
	result := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "] ->") {
			// Parse format: "[1] -> Display Name"
			parts := strings.SplitN(line, "] ->", 2)
			if len(parts) == 2 {
				indexStr := strings.TrimPrefix(strings.TrimSpace(parts[0]), "[")
				name := strings.TrimSpace(parts[1])

				// Convert to 0-based index
				if idx := parseIndex(indexStr); idx >= 0 && idx < len(models) {
					model := models[idx]
					key := getModelCacheKey(model)
					if len(name) > 0 && len(name) <= 60 && !strings.Contains(name, "\n") {
						result[key] = name
					}
				}
			}
		}
	}

	return result
}

// parseIndex converts string index to int, returns -1 if invalid
func parseIndex(s string) int {
	if idx, err := strconv.Atoi(s); err == nil && idx > 0 {
		return idx - 1 // Convert to 0-based
	}
	return -1
}

// createDisplayName generates a display name for a model using cache-first approach.
// This is used for individual models that don't have duplicates.
func createDisplayName(cache *Cache, model Model) string {
	// Use the same prompt as group processing (for consistency)
	result := createDisplayNamesForGroup(cache, []Model{model})
	key := getModelCacheKey(model)
	if name, exists := result[key]; exists {
		return name
	}
	// Fallback to model ID if something went wrong
	return model.ID
}

// createDisplayNamesForGroup generates display names for a group of models with the same ID
func createDisplayNamesForGroup(cache *Cache, models []Model) map[string]string {
	result := make(map[string]string)
	uncachedModels := []Model{}

	// Check cache for each model in the group
	for _, model := range models {
		if cachedName := cache.Get(model); cachedName != "" {
			key := getModelCacheKey(model)
			result[key] = cachedName
		} else {
			uncachedModels = append(uncachedModels, model)
		}
	}

	// If all models are cached, return cached results
	if len(uncachedModels) == 0 {
		return result
	}

	// Generate names for uncached models as a group
	if groupNames := generateDisplayNamesForGroup(uncachedModels); groupNames != nil {
		// Cache successful results
		for key, name := range groupNames {
			result[key] = name

			// Find the model for this key to cache it
			for _, model := range uncachedModels {
				modelKey := getModelCacheKey(model)
				if modelKey == key {
					if err := cache.Set(model, name); err != nil {
						log.Printf("Failed to cache group display name for %s: %v", model.ID, err)
					} else {
						log.Printf("Cached group LLM-generated name for %s: %s", model.ID, name)
					}
					break
				}
			}
		}
	}

	// For any remaining uncached models, use fallback
	for _, model := range uncachedModels {
		key := getModelCacheKey(model)
		if _, exists := result[key]; !exists {
			result[key] = model.ID // Fallback to model ID
		}
	}

	return result
}

// getModelCacheKey generates a unique cache key for a model including all metadata
func getModelCacheKey(model Model) string {
	return model.ID + "|" + hashModelMetadata(model)
}

// hashModelMetadata creates a SHA256 hash of all differentiating model metadata
// This ensures models with same ID but different providers/routes/descriptions get separate cache entries
func hashModelMetadata(model Model) string {
	// Include all metadata that could differentiate models with the same ID
	metadata := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d",
		model.Description,
		model.Provider,
		model.Route,
		model.Pool,
		model.Subtype,
		model.InstructType,
		model.Quantization,
		model.Model,                              // Base model name
		strings.Join(model.InputModalities, ","), // Input modalities (text, image, audio, etc.)
		strings.Join(model.OutputModalities, ","), // Output modalities
		model.MaxTokens, // Context window size
	)
	hash := sha256.Sum256([]byte(metadata))
	return fmt.Sprintf("%x", hash)
}

func getDefaultMaxTokens(model Model) int64 {
	if model.MaxResponseTokens > 0 {
		return model.MaxResponseTokens
	}
	if model.MaxTokens > 0 {
		return model.MaxTokens / 4 // Conservative default
	}
	return 4096 // reasonable default
}

func getContextWindow(model Model) int64 {
	if model.MaxTokens > 0 {
		return model.MaxTokens
	}
	return 32768
}

// This is used to generate the apipie.json config file.
func main() {
	// Initialize cache
	cache, err := NewCache("cmd/apipie/cache.db")
	if err != nil {
		log.Fatal("Error initializing cache:", err)
	}
	defer cache.Close()

	// Clean old cache entries (older than 30 days)
	if err := cache.CleanOldEntries(30 * 24 * time.Hour); err != nil {
		log.Printf("Warning: Failed to clean old cache entries: %v", err)
	}

	// Get cache stats
	if cacheCount, err := cache.GetStats(); err == nil {
		log.Printf("Cache initialized with %d entries", cacheCount)
	}

	modelsResp, err := fetchAPIpieModels()
	if err != nil {
		log.Fatal("Error fetching APIpie models:", err)
	}

	apipieProvider := catwalk.Provider{
		Name:                "APIpie",
		ID:                  "apipie",
		APIKey:              "$APIPIE_API_KEY",
		APIEndpoint:         "https://apipie.ai/v1",
		Type:                catwalk.TypeOpenAI,
		DefaultLargeModelID: "claude-sonnet-4-5",
		DefaultSmallModelID: "claude-haiku-4-5",
		Models:              []catwalk.Model{},
	}

	// Group models by ID to handle duplicates intelligently
	modelGroups := make(map[string][]Model)
	for _, model := range modelsResp.Data {
		if isTextModel(model) {
			modelGroups[model.ID] = append(modelGroups[model.ID], model)
		}
	}

	// Process each group
	for modelID, models := range modelGroups {
		var displayNames map[string]string

		if len(models) == 1 {
			// Single model - use individual processing
			model := models[0]
			displayName := createDisplayName(cache, model)
			key := getModelCacheKey(model)
			displayNames = map[string]string{key: displayName}
		} else {
			// Multiple models with same ID - use group processing
			log.Printf("Processing %d variants of model %s", len(models), modelID)
			displayNames = createDisplayNamesForGroup(cache, models)
		}

		// Create catwalk.Model entries for each model
		for _, model := range models {
			key := getModelCacheKey(model)
			displayName, exists := displayNames[key]
			if !exists {
				displayName = model.ID // Fallback
			}

			// Parse and convert costs to per-million-tokens
			var costPer1MIn, costPer1MOut float64

			// Confirmed pricing is already per-million-tokens, advertised is per-token
			if model.Pricing.Confirmed.InputCost != "" {
				costPer1MIn, _ = strconv.ParseFloat(model.Pricing.Confirmed.InputCost, 64)
			} else if model.Pricing.Advertised.InputCostPerToken != "" {
				inputCostPerToken, _ := strconv.ParseFloat(model.Pricing.Advertised.InputCostPerToken, 64)
				costPer1MIn = inputCostPerToken * 1_000_000
			}

			if model.Pricing.Confirmed.OutputCost != "" {
				costPer1MOut, _ = strconv.ParseFloat(model.Pricing.Confirmed.OutputCost, 64)
			} else if model.Pricing.Advertised.OutputCostPerToken != "" {
				outputCostPerToken, _ := strconv.ParseFloat(model.Pricing.Advertised.OutputCostPerToken, 64)
				costPer1MOut = outputCostPerToken * 1_000_000
			}

			m := catwalk.Model{
				ID:                 model.ID,
				Name:               displayName,
				CostPer1MIn:        costPer1MIn,
				CostPer1MOut:       costPer1MOut,
				CostPer1MInCached:  0,
				CostPer1MOutCached: 0,
				ContextWindow:      model.MaxTokens,
				DefaultMaxTokens:   getDefaultMaxTokens(model),
				CanReason:          canReason(model),
				HasReasoningEffort: hasReasoningEfforts(cache, model),
				SupportsImages:     supportsImages(model),
			}

			apipieProvider.Models = append(apipieProvider.Models, m)
			fmt.Printf("Added model %s (%s) with context window %d\n", model.ID, displayName, m.ContextWindow)
		}
	}

	// Sort models by name for consistency
	slices.SortFunc(apipieProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Save the JSON in internal/providers/configs/apipie.json
	data, err := json.MarshalIndent(apipieProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling APIpie provider:", err)
	}

	// Write to file
	if err := os.WriteFile("internal/providers/configs/apipie.json", data, 0o600); err != nil {
		log.Fatal("Error writing APIpie provider config:", err)
	}

	// Final cache stats
	if finalCount, err := cache.GetStats(); err == nil {
		log.Printf("Cache now contains %d entries", finalCount)
	}

	fmt.Printf("Successfully generated APIpie provider config with %d models\n", len(apipieProvider.Models))
}
