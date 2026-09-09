// Package main provides a command-line tool to fetch models from Umans
// and generate a configuration file for the provider.
package main

import (
	"context"
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

// UmansModel represents a model from the Umans models API.
type UmansModel struct {
	ID            string       `json:"id"`
	OwnedBy       string       `json:"owned_by"`
	ContextLength int64        `json:"context_length"`
	Pricing       UmansPricing `json:"pricing,omitempty"`
}

// UmansPricing contains per-million-token pricing for a model.
type UmansPricing struct {
	Input  float64 `json:"input,omitempty"`
	Output float64 `json:"output,omitempty"`
}

// ModelsResponse is the response structure for the Umans models API.
type ModelsResponse struct {
	Data []UmansModel `json:"data"`
}

func fetchUmansModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading models response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	_ = os.MkdirAll("tmp", 0o700)
	_ = os.WriteFile("tmp/umans-response.json", body, 0o600)

	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	return &mr, nil
}

func modelDisplayName(id string) string {
	name := strings.TrimPrefix(id, "umans-")
	name = strings.ReplaceAll(name, "-", " ")
	words := strings.Fields(name)
	for i, w := range words {
		upper := strings.ToUpper(w)
		if upper == "GLM" || upper == "A3B" || upper == "35B" {
			words[i] = upper
		} else {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return "Umans " + strings.Join(words, " ")
}

// This is used to generate the umans.json config file.
func main() {
	umansProvider := catwalk.Provider{
		Name:                "Umans",
		ID:                  "umans",
		APIKey:              "$UMANS_API_KEY",
		APIEndpoint:         "https://api.code.umans.ai/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "umans-coder",
		DefaultSmallModelID: "umans-flash",
	}

	modelsResp, err := fetchUmansModels(umansProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching Umans models:", err)
	}

	for _, model := range modelsResp.Data {
		// Skip quantized variants
		if strings.Contains(model.ID, "nvfp4") {
			continue
		}

		var defaultMaxTokens int64
		if model.ID == "umans-flash" {
			defaultMaxTokens = 8192
		} else {
			defaultMaxTokens = 32768
		}

		canReason := !strings.Contains(model.ID, "flash")

		m := catwalk.Model{
			ID:                 model.ID,
			Name:               modelDisplayName(model.ID),
			CostPer1MIn:        model.Pricing.Input,
			CostPer1MOut:       model.Pricing.Output,
			CostPer1MInCached:  0,
			CostPer1MOutCached: 0,
			ContextWindow:      model.ContextLength,
			DefaultMaxTokens:   defaultMaxTokens,
			CanReason:          canReason,
			SupportsImages:     true,
		}

		umansProvider.Models = append(umansProvider.Models, m)
	}

	slices.SortFunc(umansProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(umansProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Umans provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/umans.json", data, 0o600); err != nil {
		log.Fatal("Error writing Umans provider config:", err)
	}

	fmt.Printf("Generated umans.json with %d models\n", len(umansProvider.Models))
}
