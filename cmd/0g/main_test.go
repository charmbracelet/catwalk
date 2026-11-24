package main

import (
	"reflect"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
)

func TestReasoningFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model Model
		want  catwalk.Reasoning
	}{
		{
			name:  "no block, no reasoning_effort",
			model: Model{SupportedParameters: []string{"tools"}},
			want:  catwalk.Reasoning{Thinking: catwalk.ThinkingNever},
		},
		{
			name:  "no block, reasoning_effort forwarded",
			model: Model{SupportedParameters: []string{"tools", "reasoning_effort"}},
			want: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("low", "medium", "high"),
				DefaultEffortLevel: "medium",
			},
		},
		{
			name:  "levels that none turns off",
			model: Model{Reasoning: &Reasoning{SupportedEfforts: []string{"xhigh", "high", "medium", "low", "none"}}},
			want: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("none", "low", "medium", "high", "xhigh"),
				DefaultEffortLevel: "medium",
			},
		},
		{
			name:  "mandatory levels",
			model: Model{Reasoning: &Reasoning{Mandatory: true, SupportedEfforts: []string{"max", "low"}}},
			want: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingAlways,
				EffortLevels:       catwalk.NewEffortLevels("low", "max"),
				DefaultEffortLevel: "max",
			},
		},
		{
			name:  "on and off only",
			model: Model{Reasoning: &Reasoning{}, SupportedParameters: []string{"reasoning_effort"}},
			want: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("none", "low"),
				DefaultEffortLevel: "low",
			},
		},
		{
			name:  "always on, no control",
			model: Model{Reasoning: &Reasoning{Mandatory: true}, SupportedParameters: []string{"reasoning_effort"}},
			want:  catwalk.Reasoning{Thinking: catwalk.ThinkingAlways},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reasoningFor(tt.model); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("reasoningFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
