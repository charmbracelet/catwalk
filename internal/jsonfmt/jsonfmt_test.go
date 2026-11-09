package jsonfmt

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestMarshalCollapsesSmallObjects(t *testing.T) {
	type inner struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	type outer struct {
		Name    string  `json:"name"`
		Small   inner   `json:"small"`
		Models  []inner `json:"models"`
		Nothing []int   `json:"nothing,omitempty"`
	}
	got, err := Marshal(outer{
		Name:  "test",
		Small: inner{A: 1, B: "x"},
		Models: []inner{
			{A: 1, B: "one"},
			{A: 2, B: "two"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "name": "test",
  "small": { "a": 1, "b": "x" },
  "models": [{ "a": 1, "b": "one" }, { "a": 2, "b": "two" }]
}
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalExpandsWhenTooWide(t *testing.T) {
	got, err := MarshalOptions(map[string]any{
		"key": strings.Repeat("x", 79),
	}, Options{Width: 40})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"key\": \"" + strings.Repeat("x", 79) + "\"\n}\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalCollapsesScalarArrays(t *testing.T) {
	got, err := Marshal(map[string]any{"ids": []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{ \"ids\": [\"a\", \"b\", \"c\"] }\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalEmptyComposites(t *testing.T) {
	got, err := Marshal(map[string]any{"obj": map[string]any{}, "arr": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{ \"arr\": [], \"obj\": {} }\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatPreservesKeyOrderAndScalars(t *testing.T) {
	input := []byte(`{"zeta": 1.250, "alpha": "has \"quotes\", and \u00e9", "nested": {"k": [1,2,3], "j": null}}`)
	got := Format(input, Options{Width: 1000})
	want := "{ \"zeta\": 1.250, \"alpha\": \"has \\\"quotes\\\", and \\u00e9\", \"nested\": { \"k\": [1, 2, 3], \"j\": null } }\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatInvalidJSONReturnsInput(t *testing.T) {
	input := []byte("{\"broken\": ")
	got := Format(input, Options{})
	if string(got) != string(input) {
		t.Errorf("got:\n%s\nwant:\n%s", got, input)
	}
}

func TestFormatRoundTripsRealConfig(t *testing.T) {
	for _, name := range []string{
		"../../internal/providers/configs/openrouter.json",
		"../../internal/providers/configs/anthropic.json",
	} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var before, after any
		if err := json.Unmarshal(raw, &before); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out := Format(raw, Options{})
		if err := json.Unmarshal(out, &after); err != nil {
			t.Fatalf("%s: formatted output is invalid JSON: %v", name, err)
		}
		if len(before.(map[string]any)) != len(after.(map[string]any)) {
			t.Errorf("%s: top-level key count changed", name)
		}
		if string(Format(out, Options{})) != string(out) {
			t.Errorf("%s: formatting is not idempotent", name)
		}
	}
}
