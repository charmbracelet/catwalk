// Package main provides a command-line tool to fetch models from QiniuCloud
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
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// APIModel represents a model entry from the QiniuCloud models API.
type APIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse is the response structure for the QiniuCloud models API.
type ModelsResponse struct {
	Object string     `json:"object"`
	Data   []APIModel `json:"data"`
}

// ModelMeta holds capability and context metadata for a known model.
type ModelMeta struct {
	ContextWindow    int64
	DefaultMaxTokens int64
	CanReason        bool
	SupportsImages   bool
}

// modelMetadata provides context/capability data for all known QiniuCloud models.
// API only returns id; this table supplements the missing metadata.
var modelMetadata = map[string]ModelMeta{
	// Qwen models
	"qwen3-235b-a22b-thinking-2507":  {262000, 64000, true, false},
	"qwen3-coder-480b-a35b-instruct": {262000, 4096, false, false},
	"qwen3-32b":                      {40000, 10000, true, false},
	"qwen3-vl-30b-a3b-thinking":      {128000, 32000, false, true},
	"qwen3-30b-a3b-thinking-2507":    {126000, 32000, true, false},
	"qwen3-30b-a3b-instruct-2507":    {128000, 32000, false, false},
	"qwen3-next-80b-a3b-thinking":    {131000, 64000, true, false},
	"qwen3-max-preview":              {262000, 64000, false, false},
	"qwen3-235b-a22b":                {128000, 32000, true, false},
	"qwen3-30b-a3b":                  {128000, 32000, false, false},
	"qwen3-max":                      {262000, 64000, false, false},
	"qwen3-235b-a22b-instruct-2507":  {262000, 64000, false, false},
	"qwen3-next-80b-a3b-instruct":    {131000, 32000, false, false},
	"qwen3.5-397b-a17b":              {262000, 64000, true, false},
	"qwen-vl-max-2025-01-25":         {128000, 32000, false, true},
	"qwen-max-2025-01-25":            {128000, 32000, false, false},
	"qwen2.5-vl-72b-instruct":        {128000, 32000, false, true},
	"qwen2.5-vl-7b-instruct":         {128000, 32000, false, true},
	"qwen-turbo":                     {1000000, 100000, true, false},
	// DeepSeek models
	"deepseek/deepseek-v3.2-exp-thinking":      {128000, 64000, true, false},
	"deepseek/deepseek-v3.2-exp":               {128000, 32000, false, false},
	"deepseek-r1":                              {128000, 64000, true, false},
	"deepseek-r1-0528":                         {128000, 64000, true, false},
	"deepseek-v3-0324":                         {128000, 32000, false, false},
	"deepseek/deepseek-v3.2-251201":            {128000, 32000, false, false},
	"deepseek/deepseek-v3.1-terminus-thinking": {128000, 64000, true, false},
	"deepseek-v3":                              {128000, 32000, false, false},
	"deepseek/deepseek-v3.1-terminus":          {128000, 32000, true, false},
	"deepseek-v3.1":                            {128000, 32000, false, false},
	// MiniMax models
	"minimax/minimax-m2.5":           {204800, 128000, true, false},
	"minimax/minimax-m2.5-highspeed": {204800, 128000, false, false},
	"minimax/minimax-m2.1":           {204800, 4096, true, false},
	"minimax/minimax-m2":             {200000, 4096, true, false},
	"MiniMax-M1":                     {1000000, 100000, true, false},
	// GLM / ZhipuAI models
	"z-ai/glm-5":   {200000, 128000, true, false},
	"z-ai/glm-4.7": {200000, 4096, true, false},
	"z-ai/glm-4.6": {200000, 4096, false, false},
	"glm-4.5":      {131072, 98304, true, false},
	"glm-4.5-air":  {131072, 65536, true, false},
	// Kimi / Moonshot models
	"moonshotai/kimi-k2.5":        {256000, 100000, false, true},
	"moonshotai/kimi-k2-0905":     {256000, 100000, true, false},
	"moonshotai/kimi-k2-thinking": {256000, 100000, true, false},
	"kimi-k2":                     {128000, 32000, false, false},
	// Doubao / ByteDance models
	"doubao-1.5-thinking-pro":  {128000, 64000, true, false},
	"doubao-1.5-vision-pro":    {128000, 32000, false, true},
	"doubao-1.5-pro-32k":       {128000, 32000, false, false},
	"doubao-seed-1.6-thinking": {256000, 64000, true, true},
	"doubao-seed-1.6-flash":    {256000, 64000, true, true},
	"doubao-seed-1.6":          {256000, 64000, true, true},
	"doubao-seed-2.0-lite":     {256000, 64000, false, false},
	"doubao-seed-2.0-mini":     {256000, 64000, false, false},
	"doubao-seed-2.0-pro":      {256000, 64000, false, true},
	"doubao-seed-2.0-code":     {256000, 64000, false, false},
	// OpenAI models (via QiniuCloud proxy)
	"openai/gpt-5.4":       {21000000, 128000, true, true},
	"openai/gpt-5":         {128000, 32000, true, true},
	"openai/gpt-5-chat":    {128000, 32000, true, true},
	"openai/gpt-5.2":       {128000, 32000, true, true},
	"openai/gpt-5.2-chat":  {128000, 32000, true, true},
	"openai/gpt-5-pro":     {128000, 32000, true, true},
	"openai/gpt-5-mini":    {128000, 32000, false, true},
	"openai/gpt-5-nano":    {128000, 32000, false, false},
	"openai/gpt-5.3-codex": {128000, 32000, true, false},
	"openai/gpt-5.2-codex": {128000, 32000, true, false},
	"gpt-oss-120b":         {128000, 32000, true, false},
	"gpt-oss-20b":          {128000, 32000, false, false},
	// Claude models (via QiniuCloud proxy)
	"claude-4.6-sonnet": {200000, 64000, true, true},
	"claude-4.6-opus":   {200000, 32000, true, true},
	"claude-4.5-sonnet": {200000, 64000, false, true},
	"claude-4.5-opus":   {200000, 32000, true, true},
	"claude-4.5-haiku":  {200000, 64000, false, true},
	"claude-4.1-opus":   {200000, 32000, true, true},
	"claude-4.0-sonnet": {200000, 64000, false, true},
	"claude-4.0-opus":   {200000, 32000, true, true},
	// Gemini models (via QiniuCloud proxy)
	"gemini-2.5-pro":                 {1000000, 64000, true, true},
	"gemini-2.5-flash":               {1000000, 64000, true, true},
	"gemini-2.5-flash-lite":          {1000000, 64000, false, true},
	"gemini-2.5-flash-image":         {1000000, 64000, false, true},
	"gemini-3.1-pro-preview":         {1000000, 64000, true, true},
	"gemini-3.1-flash-lite-preview":  {1000000, 64000, false, true},
	"gemini-3.1-flash-image-preview": {1000000, 64000, false, true},
	"gemini-3.0-pro-preview":         {1000000, 64000, true, true},
	"gemini-3.0-pro-image-preview":   {1000000, 64000, false, true},
	"gemini-3.0-flash-preview":       {1000000, 64000, false, true},
	// xAI Grok models (via QiniuCloud proxy)
	"x-ai/grok-4.1-fast":               {256000, 64000, false, true},
	"x-ai/grok-4.1-fast-reasoning":     {256000, 64000, true, true},
	"x-ai/grok-4.1-fast-non-reasoning": {256000, 64000, false, true},
	"x-ai/grok-4-fast":                 {256000, 64000, false, true},
	"x-ai/grok-4-fast-reasoning":       {256000, 64000, true, true},
	"x-ai/grok-4-fast-non-reasoning":   {256000, 64000, false, true},
	"x-ai/grok-code-fast-1":            {256000, 64000, false, false},
	// Other models
	"meituan/longcat-flash-lite": {256000, 32000, false, false},
	"xiaomi/mimo-v2-flash":       {256000, 64000, true, false},
	"arcee-ai/trinity-mini":      {128000, 32000, false, false},
	"stepfun/step-3.5-flash":     {256000, 64000, false, false},
}

// defaultMeta is used for models not in modelMetadata.
var defaultMeta = ModelMeta{
	ContextWindow:    128000,
	DefaultMaxTokens: 32000,
	CanReason:        false,
	SupportsImages:   false,
}

func getModelMeta(id string) ModelMeta {
	if meta, ok := modelMetadata[id]; ok {
		return meta
	}
	return defaultMeta
}

// modelIDToName converts a model ID to a human-readable name.
func modelIDToName(id string) string {
	// Take the part after the last '/'
	name := id
	if idx := strings.LastIndex(id, "/"); idx != -1 {
		name = id[idx+1:]
	}
	// Replace hyphens and underscores with spaces
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	// Title-case each word
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func fetchQiniuCloudModels(apiEndpoint string) (*ModelsResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", apiEndpoint+"/models", nil)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	req.Header.Set("User-Agent", "Crush-Client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
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

func main() {
	qiniuProvider := catwalk.Provider{
		Name:                "QiniuCloud",
		ID:                  "qiniucloud",
		APIKey:              "$QINIUCLOUD_API_KEY",
		APIEndpoint:         "https://api.qnaigc.com/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "minimax/minimax-m2.5",
		DefaultSmallModelID: "glm-4.5-air",
		Models:              []catwalk.Model{},
	}

	// Use the overseas endpoint for fetching the model list (more accessible).
	// The provider api_endpoint remains the domestic endpoint for runtime use.
	fetchEndpoint := "https://openai.sufy.com/v1"
	modelsResp, err := fetchQiniuCloudModels(fetchEndpoint)
	if err != nil {
		log.Printf("Warning: failed to fetch models from %s (%v), trying domestic endpoint", fetchEndpoint, err)
		modelsResp, err = fetchQiniuCloudModels(qiniuProvider.APIEndpoint)
		if err != nil {
			log.Printf("Warning: both endpoints failed (%v), using metadata table only", err)
			modelsResp = buildFallbackResponse()
		}
	}

	// Track which model IDs we've added to avoid duplicates
	seen := map[string]bool{}

	for _, apiModel := range modelsResp.Data {
		id := apiModel.ID

		if seen[id] {
			continue
		}

		meta := getModelMeta(id)

		var reasoningLevels []string
		var defaultReasoning string
		if meta.CanReason {
			reasoningLevels = []string{"low", "medium", "high"}
			defaultReasoning = "medium"
		}

		m := catwalk.Model{
			ID:                     id,
			Name:                   modelIDToName(id),
			ContextWindow:          meta.ContextWindow,
			DefaultMaxTokens:       meta.DefaultMaxTokens,
			CanReason:              meta.CanReason,
			ReasoningLevels:        reasoningLevels,
			DefaultReasoningEffort: defaultReasoning,
			SupportsImages:         meta.SupportsImages,
			// Pricing set to 0 — API does not return pricing information
		}

		qiniuProvider.Models = append(qiniuProvider.Models, m)
		seen[id] = true
		fmt.Printf("Added model %s (context: %d, reason: %v, images: %v)\n",
			id, meta.ContextWindow, meta.CanReason, meta.SupportsImages)
	}

	// Save the JSON in internal/providers/configs/qiniucloud.json
	data, err := json.MarshalIndent(qiniuProvider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling QiniuCloud provider:", err)
	}

	if err := os.WriteFile("internal/providers/configs/qiniucloud.json", data, 0o600); err != nil {
		log.Fatal("Error writing QiniuCloud provider config:", err)
	}

	fmt.Printf("Generated qiniucloud.json with %d models\n", len(qiniuProvider.Models))
}

// buildFallbackResponse constructs a ModelsResponse from the known metadata table.
// Used when the live API is unreachable.
func buildFallbackResponse() *ModelsResponse {
	resp := &ModelsResponse{Object: "list"}
	for id := range modelMetadata {
		resp.Data = append(resp.Data, APIModel{ID: id})
	}
	return resp
}
