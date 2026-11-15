package catwalk

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestProviderRequestEncodingJSON(t *testing.T) {
	t.Run("marshal with encoding", func(t *testing.T) {
		data, err := json.Marshal(Provider{ID: InferenceProviderOpenAI, RequestEncoding: RequestEncodingGzip})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"request_encoding":"gzip"`) {
			t.Errorf("marshaled provider = %s, want it to contain request_encoding gzip", data)
		}
	})

	t.Run("marshal without encoding omits field", func(t *testing.T) {
		data, err := json.Marshal(Provider{ID: InferenceProviderOpenAI})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "request_encoding") {
			t.Errorf("marshaled provider = %s, want no request_encoding field", data)
		}
	})

	t.Run("unmarshal with encoding", func(t *testing.T) {
		var p Provider
		if err := json.Unmarshal([]byte(`{"id":"openai","request_encoding":"brotli"}`), &p); err != nil {
			t.Fatal(err)
		}
		if p.RequestEncoding != RequestEncodingBrotli {
			t.Errorf("RequestEncoding = %q, want %q", p.RequestEncoding, RequestEncodingBrotli)
		}
	})

	t.Run("unmarshal without encoding leaves zero value", func(t *testing.T) {
		var p Provider
		if err := json.Unmarshal([]byte(`{"id":"openai"}`), &p); err != nil {
			t.Fatal(err)
		}
		if p.RequestEncoding != "" {
			t.Errorf("RequestEncoding = %q, want empty", p.RequestEncoding)
		}
	})
}

func TestKnownRequestEncodings(t *testing.T) {
	want := []RequestEncoding{RequestEncodingNone, RequestEncodingBrotli, RequestEncodingGzip}
	if got := KnownRequestEncodings(); !reflect.DeepEqual(got, want) {
		t.Errorf("KnownRequestEncodings() = %v, want %v", got, want)
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
