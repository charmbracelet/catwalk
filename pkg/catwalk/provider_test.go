package catwalk

import (
	"reflect"
	"testing"
)

func TestModelCanReason(t *testing.T) {
	tests := []struct {
		name     string
		thinking Thinking
		want     bool
	}{
		{"unset", "", false},
		{"never", ThinkingNever, false},
		{"always", ThinkingAlways, true},
		{"toggleable", ThinkingToggleable, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{Reasoning: Reasoning{Thinking: tt.thinking}}
			if got := m.CanReason(); got != tt.want {
				t.Errorf("CanReason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelReasoningEffortLevels(t *testing.T) {
	t.Run("with levels", func(t *testing.T) {
		m := Model{Reasoning: Reasoning{EffortLevels: NewEffortLevels("low", "high")}}
		want := []string{"low", "high"}
		if got := m.ReasoningEffortLevels(); !reflect.DeepEqual(got, want) {
			t.Errorf("ReasoningEffortLevels() = %v, want %v", got, want)
		}
	})

	t.Run("without levels", func(t *testing.T) {
		if got := (Model{}).ReasoningEffortLevels(); len(got) != 0 {
			t.Errorf("ReasoningEffortLevels() = %v, want empty", got)
		}
	})
}
