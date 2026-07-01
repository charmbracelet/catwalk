// Package main provides a command-line tool to fetch models from NEAR AI Cloud
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

	"charm.land/catwalk/pkg/catwalk"
)

type ModelsResponse struct {
	Models []NearAIModel `json:"models"`
}

type NearAIModel struct {
	ModelID               string       `json:"modelId"`
	InputCostPerToken     PricingValue `json:"inputCostPerToken"`
	OutputCostPerToken    PricingValue `json:"outputCostPerToken"`
	CacheReadCostPerToken PricingValue `json:"cacheReadCostPerToken"`
	Metadata              Metadata     `json:"metadata"`
}

type PricingValue struct {
	Amount   int64  `json:"amount"`
	Scale    int64  `json:"scale"`
	Currency string `json:"currency"`
}

type Metadata struct {
	ContextLength        int64        `json:"contextLength"`
	ModelDisplayName     string       `json:"modelDisplayName"`
	Verifiable           bool         `json:"verifiable"`
	AttestationSupported bool         `json:"attestationSupported"`
	Architecture         Architecture `json:"architecture"`
}

type Architecture struct {
	InputModalities  []string `json:"inputModalities"`
	OutputModalities []string `json:"outputModalities"`
}

func fetchNearAIModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/model/list", nil)
	req.Header.Set("User-Agent", "Crush-Client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &mr, nil
}

func contains(values []string, want string) bool {
	return slices.ContainsFunc(values, func(value string) bool {
		return strings.EqualFold(value, want)
	})
}

func isChatModel(model NearAIModel) bool {
	id := strings.ToLower(model.ModelID)
	if strings.Contains(id, "privacy-filter") || strings.Contains(id, "reranker") {
		return false
	}

	if model.Metadata.ContextLength <= 0 {
		return false
	}

	input := model.Metadata.Architecture.InputModalities
	output := model.Metadata.Architecture.OutputModalities
	if contains(input, "audio") {
		return false
	}
	if contains(output, "embedding") || contains(output, "image") {
		return false
	}
	if len(output) > 0 && !contains(output, "text") {
		return false
	}
	return true
}

func costPer1M(cost PricingValue) float64 {
	if cost.Currency != "" && cost.Currency != "USD" {
		return 0
	}
	v := float64(cost.Amount) * math.Pow10(6-int(cost.Scale))
	return math.Round(v*1e5) / 1e5
}

func displayName(model NearAIModel) string {
	if model.Metadata.ModelDisplayName != "" {
		return model.Metadata.ModelDisplayName
	}
	if _, name, found := strings.Cut(model.ModelID, "/"); found {
		return strings.ReplaceAll(name, "-", " ")
	}
	return strings.ReplaceAll(model.ModelID, "-", " ")
}

func defaultMaxTokens(contextWindow int64) int64 {
	if contextWindow < 10 {
		return contextWindow
	}
	return contextWindow / 10
}

func bestLargeModelID(models []catwalk.Model) string {
	var best *catwalk.Model
	for i := range models {
		m := &models[i]

		if best == nil {
			best = m
			continue
		}
		mCost := m.CostPer1MIn + m.CostPer1MOut
		bestCost := best.CostPer1MIn + best.CostPer1MOut
		if mCost > bestCost {
			best = m
			continue
		}
		if mCost == bestCost && m.ContextWindow > best.ContextWindow {
			best = m
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

func bestSmallModelID(models []catwalk.Model) string {
	var best *catwalk.Model
	for i := range models {
		m := &models[i]

		if best == nil {
			best = m
			continue
		}
		mCost := m.CostPer1MIn + m.CostPer1MOut
		bestCost := best.CostPer1MIn + best.CostPer1MOut
		if mCost < bestCost {
			best = m
			continue
		}
		if mCost == bestCost && m.ContextWindow < best.ContextWindow {
			best = m
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

func main() {
	nearAIProvider := catwalk.Provider{
		Name:        "NEAR AI Cloud",
		ID:          catwalk.InferenceProviderNEARAI,
		APIKey:      "$NEARAI_API_KEY",
		APIEndpoint: "https://cloud-api.near.ai/v1",
		Type:        catwalk.TypeOpenAICompat,
		Models:      []catwalk.Model{},
	}

	modelsResp, err := fetchNearAIModels(nearAIProvider.APIEndpoint)
	if err != nil {
		log.Fatal("Error fetching NEAR AI Cloud models:", err)
	}

	var verifiableModels []catwalk.Model
	for _, model := range modelsResp.Models {
		if !isChatModel(model) {
			continue
		}

		m := catwalk.Model{
			ID:                 model.ModelID,
			Name:               displayName(model),
			CostPer1MIn:        costPer1M(model.InputCostPerToken),
			CostPer1MOut:       costPer1M(model.OutputCostPerToken),
			CostPer1MInCached:  costPer1M(model.CacheReadCostPerToken),
			CostPer1MOutCached: 0,
			ContextWindow:      model.Metadata.ContextLength,
			DefaultMaxTokens:   defaultMaxTokens(model.Metadata.ContextLength),
			CanReason:          false,
			SupportsImages:     contains(model.Metadata.Architecture.InputModalities, "image"),
		}

		nearAIProvider.Models = append(nearAIProvider.Models, m)
		if model.Metadata.Verifiable && model.Metadata.AttestationSupported {
			verifiableModels = append(verifiableModels, m)
		}
		fmt.Printf("Added model %s with context window %d\n", model.ModelID, model.Metadata.ContextLength)
	}

	defaultCandidates := nearAIProvider.Models
	if len(verifiableModels) > 0 {
		defaultCandidates = verifiableModels
	}
	nearAIProvider.DefaultLargeModelID = bestLargeModelID(defaultCandidates)
	nearAIProvider.DefaultSmallModelID = bestSmallModelID(defaultCandidates)

	slices.SortFunc(nearAIProvider.Models, func(a catwalk.Model, b catwalk.Model) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, err := json.MarshalIndent(nearAIProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling NEAR AI Cloud provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/nearai.json", data, 0o600); err != nil {
		log.Fatal("Error writing NEAR AI Cloud provider config:", err)
	}

	fmt.Printf("Generated nearai.json with %d models\n", len(nearAIProvider.Models))
}
