package catwalk

// ProviderV2 represents the v2 (legacy) provider schema, derived
// automatically from Provider so /v2/providers keeps serving the old
// structure.
type ProviderV2 struct {
	Name                string            `json:"name"`
	ID                  InferenceProvider `json:"id"`
	APIKey              string            `json:"api_key,omitempty"`
	APIEndpoint         string            `json:"api_endpoint,omitempty"`
	Type                Type              `json:"type,omitempty"`
	DefaultLargeModelID string            `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string            `json:"default_small_model_id,omitempty"`
	Models              []ModelV2         `json:"models,omitempty"`
	DefaultHeaders      map[string]string `json:"default_headers,omitempty"`
}

// ModelV2 represents the v2 (legacy) model schema.
type ModelV2 struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	CostPer1MIn            float64      `json:"cost_per_1m_in"`
	CostPer1MOut           float64      `json:"cost_per_1m_out"`
	CostPer1MInCached      float64      `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64      `json:"cost_per_1m_out_cached"`
	ContextWindow          int64        `json:"context_window"`
	DefaultMaxTokens       int64        `json:"default_max_tokens"`
	CanReason              bool         `json:"can_reason"`
	ReasoningLevels        []string     `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string       `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool         `json:"supports_attachments"`
	Options                ModelOptions `json:"options,omitzero"`
}

// ToV2 converts the model to the legacy v2 schema. Fields that do not
// exist in v2 (per-model Type, MaxAttachments) are dropped.
func (m Model) ToV2() ModelV2 {
	v2 := ModelV2{
		ID:                     m.ID,
		Name:                   m.Name,
		CostPer1MIn:            m.Pricing.Input,
		CostPer1MOut:           m.Pricing.Output,
		CostPer1MInCached:      m.Pricing.CacheCreate,
		CostPer1MOutCached:     m.Pricing.CacheHit,
		ContextWindow:          m.ContextWindow,
		DefaultMaxTokens:       m.DefaultMaxTokens,
		CanReason:              m.Reasoning.Thinking != ThinkingNever,
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

// ToV2 converts the provider to the legacy v2 schema. The v3-only
// SessionAffinityHeader field is dropped, as it never existed in v2.
func (p Provider) ToV2() ProviderV2 {
	v2 := ProviderV2{
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
		v2.Models = make([]ModelV2, 0, len(p.Models))
		for _, m := range p.Models {
			v2.Models = append(v2.Models, m.ToV2())
		}
	}
	return v2
}

// ToV2Providers converts a slice of providers to the legacy v2 schema.
func ToV2Providers(providers []Provider) []ProviderV2 {
	if providers == nil {
		return nil
	}
	v2 := make([]ProviderV2, 0, len(providers))
	for _, p := range providers {
		v2 = append(v2, p.ToV2())
	}
	return v2
}
