// Package v2compat provides the legacy v2 provider schema, derived
// automatically from the v3 catwalk types so /v2/providers keeps serving the
// old structure.
package v2compat

import "charm.land/catwalk/v3/pkg/catwalk"

// Provider represents the v2 (legacy) provider schema.
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

// Model represents the v2 (legacy) model schema.
type Model struct {
	ID                     string               `json:"id"`
	Name                   string               `json:"name"`
	CostPer1MIn            float64              `json:"cost_per_1m_in"`
	CostPer1MOut           float64              `json:"cost_per_1m_out"`
	CostPer1MInCached      float64              `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64              `json:"cost_per_1m_out_cached"`
	ContextWindow          int64                `json:"context_window"`
	DefaultMaxTokens       int64                `json:"default_max_tokens"`
	CanReason              bool                 `json:"can_reason"`
	ReasoningLevels        []string             `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string               `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool                 `json:"supports_attachments"`
	Options                catwalk.ModelOptions `json:"options,omitzero"`
}

// FromModel converts a model to the legacy v2 schema. Fields that do not
// exist in v2 (per-model Type, MaxAttachments) are dropped.
func FromModel(m catwalk.Model) Model {
	v2 := Model{
		ID:                     m.ID,
		Name:                   m.Name,
		CostPer1MIn:            m.Pricing.Input,
		CostPer1MOut:           m.Pricing.Output,
		CostPer1MInCached:      m.Pricing.CacheCreate,
		CostPer1MOutCached:     m.Pricing.CacheHit,
		ContextWindow:          m.ContextWindow,
		DefaultMaxTokens:       m.DefaultMaxTokens,
		CanReason:              m.Reasoning.Thinking != catwalk.ThinkingNever,
		DefaultReasoningEffort: m.Reasoning.DefaultEffortLevel,
		SupportsImages:         m.Capabilities.Vision,
		Options:                m.Options,
	}
	if v2.CanReason && len(m.Reasoning.EffortLevels) > 0 {
		v2.ReasoningLevels = make([]string, 0, len(m.Reasoning.EffortLevels))
		for _, level := range m.Reasoning.EffortLevels {
			v2.ReasoningLevels = append(v2.ReasoningLevels, level.Value)
		}
	}
	return v2
}

// FromProvider converts a provider to the legacy v2 schema. The v3-only
// SessionAffinityHeader field is dropped, as it never existed in v2.
func FromProvider(p catwalk.Provider) Provider {
	v2 := Provider{
		Name:                p.Name,
		ID:                  p.ID,
		APIKey:              p.APIKey,
		APIEndpoint:         p.APIEndpoint,
		Type:                p.Type,
		DefaultLargeModelID: p.DefaultLargeModelID,
		DefaultSmallModelID: p.DefaultSmallModelID,
		DefaultHeaders:      p.DefaultHeaders,
	}
	if len(p.Models) > 0 {
		v2.Models = make([]Model, 0, len(p.Models))
		for _, m := range p.Models {
			v2.Models = append(v2.Models, FromModel(m))
		}
	}
	return v2
}

// FromProviders converts a slice of providers to the legacy v2 schema.
func FromProviders(providers []catwalk.Provider) []Provider {
	if providers == nil {
		return nil
	}
	v2 := make([]Provider, 0, len(providers))
	for _, p := range providers {
		v2 = append(v2, FromProvider(p))
	}
	return v2
}
