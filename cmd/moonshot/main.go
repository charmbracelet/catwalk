// Package main provides a command-line tool to fetch models from the Moonshot
// international (api.moonshot.ai) OpenAI-compatible API and generate moonshot.json.
package main

import (
	"cmp"
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

const baseURL = "https://api.moonshot.ai/v1"

// openAIListModelsResponse is the response shape for GET /v1/models.
type openAIListModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func fetchModelIDs(ctx context.Context, apiKey string) ([]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Charm-Catwalk/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	_ = os.MkdirAll("tmp", 0o700)                                      //nolint:errcheck,gosec
	_ = os.WriteFile("tmp/moonshot-models-response.json", body, 0o600) //nolint:errcheck,gosec

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("status %d: %s", resp.StatusCode, body)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf(
				"%w: use a key from platform.moonshot.ai for this command; for platform.moonshot.cn use ./cmd/moonshot-cn",
				err,
			)
		}
		return nil, err
	}

	var parsed openAIListModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal models: %w", err)
	}

	ids := make([]string, 0, len(parsed.Data))
	seen := make(map[string]struct{})
	for _, m := range parsed.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(id), "kimi-") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func staticMetadata(id string) (catwalk.Model, bool) {
	known := map[string]catwalk.Model{
		"kimi-k2.6": {
			ID:                     "kimi-k2.6",
			Name:                   "Kimi K2.6",
			CostPer1MIn:            0.8,
			CostPer1MOut:           4,
			CostPer1MInCached:      0.2,
			CostPer1MOutCached:     0,
			ContextWindow:          262144,
			DefaultMaxTokens:       26214,
			CanReason:              true,
			ReasoningLevels:        []string{"low", "medium", "high"},
			DefaultReasoningEffort: "medium",
			SupportsImages:         true,
		},
		"kimi-k2.5": {
			ID:                     "kimi-k2.5",
			Name:                   "Kimi K2.5",
			CostPer1MIn:            0.445,
			CostPer1MOut:           2,
			CostPer1MInCached:      0.225,
			CostPer1MOutCached:     1.1,
			ContextWindow:          262144,
			DefaultMaxTokens:       26214,
			CanReason:              true,
			ReasoningLevels:        []string{"low", "medium", "high"},
			DefaultReasoningEffort: "medium",
			SupportsImages:         true,
		},
	}
	m, ok := known[id]
	return m, ok
}

func buildModel(id string) catwalk.Model {
	if m, ok := staticMetadata(id); ok {
		return m
	}
	return catwalk.Model{
		ID:                     id,
		Name:                   id,
		CostPer1MIn:            0,
		CostPer1MOut:           0,
		CostPer1MInCached:      0,
		CostPer1MOutCached:     0,
		ContextWindow:          262144,
		DefaultMaxTokens:       26214,
		CanReason:              true,
		ReasoningLevels:        []string{"low", "medium", "high"},
		DefaultReasoningEffort: "medium",
		SupportsImages:         true,
	}
}

func main() {
	apiKey := strings.TrimSpace(cmp.Or(os.Getenv("MOONSHOT_API_KEY"), os.Getenv("KIMI_API_KEY")))
	if apiKey == "" {
		log.Fatal("Set MOONSHOT_API_KEY (or KIMI_API_KEY) for platform.moonshot.ai")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ids, err := fetchModelIDs(ctx, apiKey)
	if err != nil {
		log.Fatalf("Error fetching Moonshot models: %v", err)
	}
	if len(ids) == 0 {
		log.Fatal("No kimi-* models returned from the Moonshot /v1/models API")
	}

	provider := catwalk.Provider{
		Name:        "Kimi (Moonshot)",
		ID:          catwalk.InferenceProviderMoonshot,
		APIKey:      "$MOONSHOT_API_KEY",
		APIEndpoint: baseURL,
		Type:        catwalk.TypeOpenAICompat,
	}

	for _, id := range ids {
		m := buildModel(id)
		provider.Models = append(provider.Models, m)
		fmt.Printf("Added model %s\n", id)
	}

	provider.DefaultLargeModelID = "kimi-k2.6"
	provider.DefaultSmallModelID = "kimi-k2.5"
	if !slices.ContainsFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == provider.DefaultLargeModelID }) {
		provider.DefaultLargeModelID = provider.Models[len(provider.Models)-1].ID
	}
	if !slices.ContainsFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == provider.DefaultSmallModelID }) {
		provider.DefaultSmallModelID = provider.Models[0].ID
	}

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatalf("Error marshaling Moonshot provider: %v", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/moonshot.json", data, 0o600); err != nil { //nolint:gosec
		log.Fatalf("Error writing moonshot config: %v", err)
	}
	fmt.Printf("Generated moonshot.json with %d models\n", len(provider.Models))
}
