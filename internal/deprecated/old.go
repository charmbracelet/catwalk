// Package deprecated is used to serve the old verion of the provider config
package deprecated

import "charm.land/catwalk/pkg/catwalk"

// Provider represents an AI provider configuration.
type Provider struct {
	Name                string                    `json:"name"`
	ID                  catwalk.InferenceProvider `json:"id"`
	APIKey              string                    `json:"api_key,omitempty"`
	APIEndpoint         string                    `json:"api_endpoint,omitempty"`
	Type                catwalk.Type              `json:"type,omitempty"`
	DefaultLargeModelID string                    `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string                    `json:"default_small_model_id,omitempty"`
	Models              []Model                   `json:"models,omitempty"`
	DefaultHeaders      map[string]string         `json:"default_headers,omitempty"`
}

// Model represents an AI model configuration.
type Model struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	CostPer1MIn            float64 `json:"cost_per_1m_in"`
	CostPer1MOut           float64 `json:"cost_per_1m_out"`
	CostPer1MInCached      float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64 `json:"cost_per_1m_out_cached"`
	ContextWindow          int64   `json:"context_window"`
	DefaultMaxTokens       int64   `json:"default_max_tokens"`
	CanReason              bool    `json:"can_reason"`
	HasReasoningEffort     bool    `json:"has_reasoning_efforts"`
	DefaultReasoningEffort string  `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool    `json:"supports_attachments"`
}
