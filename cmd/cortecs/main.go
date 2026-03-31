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
	"time"
	"strings"
	"charm.land/catwalk/pkg/catwalk"
)

type CortecsModel struct {
	ID              string  `json:"id"`
	Object string `json:"object"`
	Created int64 `json:"created"`
	OwnedBy            string  `json:"owned_by"`
	Description string `json:"description"`
	Pricing         Pricing `json:"pricing"`
	ContextSize   int64   `json:"context_size"`
	Tags            []string `json:"tags,omitempty"`
}

type Pricing struct {
	InputToken              float64 `json:"input_token"`
	OutputToken          float64 `json:"output_token"`
}

type ModelsResponse struct {
	Data []CortecsModel `json:"data"`
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
	//	var costPer1MIn, costPer1MOut float64

	var costPer1MIn float64 = model.Pricing.InputToken
		var costPer1MOut float64 = model.Pricing.OutputToken

		// Determine if reasoning is supported based on tags 
		canReason := false
		if model.Tags != nil {
			for _, tag := range model.Tags {
				if strings.ToLower(tag) == "reasoning" {
					canReason = true
					break
				}
			}
		}

		// TODO: determine if model supports images
		supportsImages := false

		model := catwalk.Model{
			ID:                     model.ID,
			Name:                   model.ID,
			ContextWindow:          model.ContextSize,
			CostPer1MIn:            costPer1MIn,
			CostPer1MOut:           costPer1MOut,
			CostPer1MInCached:      0,
			CostPer1MOutCached:     0,
			DefaultMaxTokens:       model.ContextSize,
			CanReason:              canReason,
			DefaultReasoningEffort: "unknown",
			ReasoningLevels:        []string{"unknown"},
			SupportsImages:         supportsImages,
		}
		models = append(models, model)
		fmt.Printf("Added model %s\n", model.ID)
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
		DefaultHeaders: map[string]string{
			"User-Agent": "Crush-Client/1.0",
		},
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
