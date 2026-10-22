package catwalk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelToV2(t *testing.T) {
	m := Model{
		ID:               "gpt-6-astra",
		Type:             TypeResponses,
		Name:             "GPT-6 Astra",
		Pricing:          Pricing{Input: 10, Output: 50, CacheCreate: 12.5, CacheHit: 1},
		ContextWindow:    1050000,
		DefaultMaxTokens: 128000,
		Reasoning: Reasoning{
			Thinking:           ThinkingToggleable,
			EffortLevels:       NewEffortLevels("low", "medium", "high"),
			DefaultEffortLevel: "medium",
		},
		Capabilities:   Capabilities{Vision: true},
		MaxAttachments: 1500,
	}

	v2 := m.ToV2()
	if v2.CostPer1MIn != 10 || v2.CostPer1MOut != 50 {
		t.Errorf("pricing mismatch: got in=%v out=%v", v2.CostPer1MIn, v2.CostPer1MOut)
	}
	if v2.CostPer1MInCached != 12.5 || v2.CostPer1MOutCached != 1 {
		t.Errorf("cached pricing mismatch: got in=%v out=%v", v2.CostPer1MInCached, v2.CostPer1MOutCached)
	}
	if !v2.CanReason {
		t.Error("expected CanReason to be true")
	}
	wantLevels := []string{"low", "medium", "high"}
	if len(v2.ReasoningLevels) != len(wantLevels) {
		t.Fatalf("reasoning levels mismatch: got %v", v2.ReasoningLevels)
	}
	for i := range wantLevels {
		if v2.ReasoningLevels[i] != wantLevels[i] {
			t.Errorf("reasoning level %d mismatch: got %q want %q", i, v2.ReasoningLevels[i], wantLevels[i])
		}
	}
	if v2.DefaultReasoningEffort != "medium" {
		t.Errorf("default effort mismatch: got %q", v2.DefaultReasoningEffort)
	}
	if !v2.SupportsImages {
		t.Error("expected SupportsImages to be true")
	}
}

func TestModelToV2NeverThinks(t *testing.T) {
	m := Model{
		ID:               "gpt-4.1",
		Name:             "GPT-4.1",
		Pricing:          Pricing{Input: 2, Output: 8},
		ContextWindow:    1047576,
		DefaultMaxTokens: 16384,
		Reasoning:        Reasoning{Thinking: ThinkingNever},
	}

	v2 := m.ToV2()
	if v2.CanReason {
		t.Error("expected CanReason to be false")
	}
	if len(v2.ReasoningLevels) != 0 {
		t.Errorf("expected no reasoning levels, got %v", v2.ReasoningLevels)
	}
	if v2.DefaultReasoningEffort != "" {
		t.Errorf("expected empty default effort, got %q", v2.DefaultReasoningEffort)
	}
	if v2.SupportsImages {
		t.Error("expected SupportsImages to be false")
	}
}

func TestProviderToV2DropsV3OnlyFields(t *testing.T) {
	p := Provider{
		Name:                  "OpenAI",
		ID:                    InferenceProviderOpenAI,
		APIKey:                "$OPENAI_API_KEY",
		SessionAffinityHeader: "x-opencode-session",
		DefaultLargeModelID:   "gpt-5.6-sol",
		DefaultSmallModelID:   "gpt-5.6-luna",
		Models: []Model{{
			ID:               "gpt-5.6-sol",
			Type:             TypeResponses,
			Name:             "GPT-5.6 Sol",
			Pricing:          Pricing{Input: 4, Output: 20, CacheCreate: 5, CacheHit: 0.4},
			ContextWindow:    1050000,
			DefaultMaxTokens: 128000,
			Reasoning:        Reasoning{Thinking: ThinkingAlways},
			Capabilities:     Capabilities{Vision: true},
			MaxAttachments:   1500,
		}},
	}

	v2 := p.ToV2()
	if len(v2.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(v2.Models))
	}

	data, err := json.Marshal(v2)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"cost_per_1m_in":4`,
		`"cost_per_1m_in_cached":5`,
		`"cost_per_1m_out_cached":0.4`,
		`"can_reason":true`,
		`"supports_attachments":true`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("v2 JSON missing %q in %s", want, got)
		}
	}
	for _, unwanted := range []string{"session_affinity_header", `"max_attachments"`, `"pricing"`, `"reasoning"`, `"capabilities"`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("v2 JSON should not contain %q in %s", unwanted, got)
		}
	}
}

func TestToV2ProvidersPreservesOrder(t *testing.T) {
	ps := []Provider{
		{Name: "Z", ID: InferenceProviderZAI},
		{Name: "A", ID: InferenceProviderOpenAI},
	}
	v2 := ToV2Providers(ps)
	if len(v2) != 2 || v2[0].ID != InferenceProviderZAI || v2[1].ID != InferenceProviderOpenAI {
		t.Fatalf("order not preserved: %+v", v2)
	}
}
