package catwalk

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

func alibabaLikeModel() Model {
	return Model{
		ID: "qwen-test",
		Pricing: Pricing{
			Input:  1.6,
			Output: 6.4,
		},
		PricingOverrides: []PricingOverride{
			{
				Condition: PricingCondition{
					InputTokens: &TokenRange{GTE: i64(32000), LT: i64(128000)},
				},
				Pricing: PricingValues{Input: f64(2.4), Output: f64(9.6)},
			},
			{
				Condition: PricingCondition{
					InputTokens: &TokenRange{GTE: i64(128000)},
				},
				Pricing: PricingValues{Input: f64(4), Output: f64(16)},
			},
		},
	}
}

func TestResolvePricingTokenRanges(t *testing.T) {
	m := alibabaLikeModel()
	tests := []struct {
		name            string
		inputTokens     int64
		wantIn, wantOut float64
	}{
		{"base tier", 1000, 1.6, 6.4},
		{"exactly at boundary", 32000, 2.4, 9.6},
		{"middle tier", 100000, 2.4, 9.6},
		{"last boundary exclusive", 127999, 2.4, 9.6},
		{"top tier", 200000, 4, 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.ResolvePricing(PricingContext{InputTokens: tt.inputTokens})
			if got.Input != tt.wantIn || got.Output != tt.wantOut {
				t.Errorf("ResolvePricing(%d) = {%v %v}, want {%v %v}",
					tt.inputTokens, got.Input, got.Output, tt.wantIn, tt.wantOut)
			}
		})
	}
}

func TestResolvePricingPeakHours(t *testing.T) {
	m := Model{
		ID:      "peak-model",
		Pricing: Pricing{Input: 1, Output: 4},
		PricingOverrides: []PricingOverride{
			{
				Condition: PricingCondition{
					Time: &TimeWindow{
						Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
						Start:    "08:00",
						End:      "20:00",
						Timezone: "Asia/Shanghai",
					},
				},
				Pricing: PricingValues{Input: f64(2), Output: f64(8)},
			},
		},
	}

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	peak := time.Date(2026, 9, 7, 10, 30, 0, 0, shanghai)     // Monday 10:30
	offPeak := time.Date(2026, 9, 7, 22, 0, 0, 0, shanghai)   // Monday 22:00
	weekend := time.Date(2026, 9, 12, 10, 30, 0, 0, shanghai) // Saturday 10:30

	if got := m.ResolvePricing(PricingContext{Time: peak}); got.Input != 2 || got.Output != 8 {
		t.Errorf("peak hours = %+v, want {2 8}", got)
	}
	if got := m.ResolvePricing(PricingContext{Time: offPeak}); got.Input != 1 || got.Output != 4 {
		t.Errorf("off-peak hours = %+v, want {1 4}", got)
	}
	if got := m.ResolvePricing(PricingContext{Time: weekend}); got.Input != 1 || got.Output != 4 {
		t.Errorf("weekend = %+v, want {1 4}", got)
	}
	if got := m.ResolvePricing(PricingContext{}); got.Input != 1 || got.Output != 4 {
		t.Errorf("zero time = %+v, want base pricing {1 4}", got)
	}
}

func TestTimeWindowWrapsMidnight(t *testing.T) {
	w := TimeWindow{Start: "20:00", End: "08:00"}
	tests := []struct {
		hour int
		want bool
	}{
		{23, true},
		{0, true},
		{7, true},
		{8, false},
		{12, false},
		{19, false},
		{20, true},
	}
	for _, tt := range tests {
		at := time.Date(2026, 9, 7, tt.hour, 30, 0, 0, time.UTC)
		if got := w.Contains(at); got != tt.want {
			t.Errorf("Contains(%02d:30) = %v, want %v", tt.hour, got, tt.want)
		}
	}
}

func TestResolvePricingPartialOverrideAndOrder(t *testing.T) {
	m := Model{
		ID:      "partial",
		Pricing: Pricing{Input: 1, Output: 4, CacheHit: 0.1},
		PricingOverrides: []PricingOverride{
			{
				Condition: PricingCondition{InputTokens: &TokenRange{GTE: i64(100)}},
				Pricing:   PricingValues{Output: f64(8)},
			},
			{
				Condition: PricingCondition{InputTokens: &TokenRange{GTE: i64(200)}},
				Pricing:   PricingValues{Output: f64(12)},
			},
		},
	}
	got := m.ResolvePricing(PricingContext{InputTokens: 150})
	if got.Input != 1 || got.Output != 8 || got.CacheHit != 0.1 {
		t.Errorf("got %+v, want input 1 output 8 cache_hit 0.1", got)
	}
	got = m.ResolvePricing(PricingContext{InputTokens: 250})
	if got.Output != 12 {
		t.Errorf("later override should win, got output %v, want 12", got.Output)
	}
}

func TestPricingOverrideJSONRoundTrip(t *testing.T) {
	raw := `{
		"id": "m",
		"name": "M",
		"pricing": {"input": 1, "output": 2},
		"pricing_overrides": [
			{
				"condition": {"input_tokens": {"gte": 32000, "lt": 128000}},
				"pricing": {"input": 1.5}
			},
			{
				"condition": {"time": {"days": ["mon", "fri"], "start": "08:00", "end": "20:00", "timezone": "Asia/Shanghai"}},
				"pricing": {"output": 6}
			}
		],
		"context_window": 1000,
		"default_max_tokens": 100,
		"can_reason": false,
		"capabilities": {"vision": false}
	}`
	var m Model
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.PricingOverrides) != 2 {
		t.Fatalf("got %d overrides, want 2", len(m.PricingOverrides))
	}
	for _, o := range m.PricingOverrides {
		if err := o.Validate(); err != nil {
			t.Errorf("Validate() = %v", err)
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var round Model
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatal(err)
	}
	if len(round.PricingOverrides) != 2 {
		t.Fatalf("round trip lost overrides: %s", out)
	}

	var withoutOverrides Model
	if err := json.Unmarshal([]byte(`{"id":"m","name":"M","pricing":{"input":1,"output":2}}`), &withoutOverrides); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(withoutOverrides)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); strings.Contains(got, "pricing_overrides") {
		t.Errorf("empty overrides should be omitted, got %s", got)
	}
}

func TestPricingOverrideValidate(t *testing.T) {
	tests := []struct {
		name     string
		override PricingOverride
		wantErr  bool
	}{
		{
			name:     "valid token range",
			override: PricingOverride{Condition: PricingCondition{InputTokens: &TokenRange{GTE: i64(0), LT: i64(32000)}}, Pricing: PricingValues{Input: f64(1)}},
		},
		{
			name:     "valid time window",
			override: PricingOverride{Condition: PricingCondition{Time: &TimeWindow{Days: []Weekday{Monday}, Start: "08:00", End: "20:00", Timezone: "UTC"}}, Pricing: PricingValues{Input: f64(1)}},
		},
		{
			name:     "inverted range",
			override: PricingOverride{Condition: PricingCondition{InputTokens: &TokenRange{GTE: i64(100), LT: i64(100)}}, Pricing: PricingValues{Input: f64(1)}},
			wantErr:  true,
		},
		{
			name:     "negative gte",
			override: PricingOverride{Condition: PricingCondition{InputTokens: &TokenRange{GTE: i64(-1)}}, Pricing: PricingValues{Input: f64(1)}},
			wantErr:  true,
		},
		{
			name:     "negative price",
			override: PricingOverride{Pricing: PricingValues{Output: f64(-1)}},
			wantErr:  true,
		},
		{
			name:     "unknown day",
			override: PricingOverride{Condition: PricingCondition{Time: &TimeWindow{Days: []Weekday{"funday"}, Start: "08:00", End: "20:00"}}, Pricing: PricingValues{Input: f64(1)}},
			wantErr:  true,
		},
		{
			name:     "bad clock format",
			override: PricingOverride{Condition: PricingCondition{Time: &TimeWindow{Start: "8am", End: "20:00"}}, Pricing: PricingValues{Input: f64(1)}},
			wantErr:  true,
		},
		{
			name:     "bad timezone",
			override: PricingOverride{Condition: PricingCondition{Time: &TimeWindow{Start: "08:00", End: "20:00", Timezone: "Mars/Olympus"}}, Pricing: PricingValues{Input: f64(1)}},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.override.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
