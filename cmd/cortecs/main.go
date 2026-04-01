// Package main provides a command-line tool to generate a configuration file
// for the Cortecs provider, which is OpenAI compatible.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

type CortecsModel struct {
	ID          string   `json:"id"`
	Object      string   `json:"object"`
	Created     int64    `json:"created"`
	OwnedBy     string   `json:"owned_by"`
	Description string   `json:"description"`
	Pricing     Pricing  `json:"pricing"`
	ContextSize int64    `json:"context_size"`
	Tags        []string `json:"tags,omitempty"`
}

func (m CortecsModel) hasTag(tagValue string) bool {
	if m.Tags != nil {
		for _, tag := range m.Tags {
			if strings.EqualFold(tag, tagValue) {
				return true
			}
		}
	}
	return false
}

type Pricing struct {
	InputToken  float64 `json:"input_token"`
	OutputToken float64 `json:"output_token"`
}

type ModelsResponse struct {
	Data []CortecsModel `json:"data"`
}

type ModelDetailResponse struct {
	Model ModelDetail `json:"model"`
}

type ModelDetail struct {
	ID         string  `json:"id"`
	ScreenName string  `json:"screen_name"`
	Context    int64   `json:"context"`
	InputCost  float64 `json:"input_tokens"`
	OutputCost float64 `json:"output_tokens"`
}

// This is used to generate the cortecs.json config file.
func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"GET",
		"https://api.cortecs.ai/v1/models",
		nil,
	)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching Cortecs models:", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading Cortecs models response:", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error fetching Cortecs models: status %d: %s", resp.StatusCode, body)
	}

	var modelsResp ModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		log.Fatal("Error parsing Cortecs models response:", err)
	}

	var models []catwalk.Model
	for _, model := range modelsResp.Data {
		// we skip models that don't support tool calling
		if !model.hasTag("Tools") {
			continue
		}

		// Fetch individual model details to get screen_name
		detailReq, _ := http.NewRequestWithContext(
			context.Background(),
			"GET",
			fmt.Sprintf("https://api.cortecs.ai/v1/models/%s", model.ID),
			nil,
		)
		detailReq.Header.Set("User-Agent", "Crush-Client/1.0")

		detailResp, err := client.Do(detailReq)
		if err != nil {
			log.Printf("Warning: Error fetching details for model %s: %v", model.ID, err)
			// Continue with default model.ID as name if we can't get details
			continue
		}
		defer func() {
			if err := detailResp.Body.Close(); err != nil {
				log.Printf("Warning: Error closing response body for model %s: %v", model.ID, err)
			}
		}()

		detailBody, err := io.ReadAll(detailResp.Body)
		if err != nil {
			log.Printf("Warning: Error reading details for model %s: %v", model.ID, err)
			continue
		}

		if detailResp.StatusCode != http.StatusOK {
			log.Printf("Warning: Error fetching details for model %s: status %d: %s", model.ID, detailResp.StatusCode, detailBody)
			continue
		}

		var detailRespData ModelDetailResponse
		if err := json.Unmarshal(detailBody, &detailRespData); err != nil {
			log.Printf("Warning: Error parsing details for model %s: %v", model.ID, err)
			continue
		}

		costPer1MIn := detailRespData.Model.InputCost
		costPer1MOut := detailRespData.Model.OutputCost

		canReason := model.hasTag("Reasoning")
		supportsImages := model.hasTag("Image")

		model := catwalk.Model{
			ID:                     model.ID,
			Name:                   detailRespData.Model.ScreenName,
			ContextWindow:          detailRespData.Model.Context,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			DefaultMaxTokens:       detailRespData.Model.Context,
			CanReason:              canReason,
			DefaultReasoningEffort: "medium",
			ReasoningLevels:        []string{"low", "medium", "high"},
			SupportsImages:         supportsImages,
		}
		models = append(models, model)
		fmt.Printf("Added model %s (%s)\n", model.ID, model.Name)
	}

	cortecsProvider := catwalk.Provider{
		Name:                "Cortecs",
		ID:                  "cortecs",
		APIKey:              "$CORTECS_API_KEY",
		APIEndpoint:         "https://api.cortecs.ai/v1",
		Type:                catwalk.TypeOpenAI,
		DefaultLargeModelID: "qwen3-coder-30b-a3b-instruct",
		DefaultSmallModelID: "glm-4.7-flash",
		Models:              models,
	}

	data, err := json.MarshalIndent(cortecsProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Cortecs provider:", err)
	}

	if err := os.WriteFile("./internal/providers/configs/cortecs.json", data, 0o600); err != nil {
		log.Fatal("Error writing Cortecs provider config:", err)
	}

	fmt.Println("Cortecs provider configuration generated successfully!")
}
