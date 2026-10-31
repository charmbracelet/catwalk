package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

func row(id, name, extra string) string {
	return `{"id":"` + id + `","name":"` + name + `","context_length":128000,` +
		`"architecture":{"input_modalities":["text"],"output_modalities":["text"]},` +
		`"pricing":{"prompt":"0.000000385001","completion":"0.00000154","input_cache_read":"0.000000007701","input_cache_write":"0.0000015"},` +
		`"supported_parameters":["tools"],"protocols":[{"protocol":"openai-chat-completions"}]` + extra + `}`
}

func build(t *testing.T, rows ...string) (catwalk.Provider, error) {
	t.Helper()
	var resp ModelsResponse
	if err := json.Unmarshal([]byte(`{"data":[`+strings.Join(rows, ",")+`]}`), &resp); err != nil {
		t.Fatal(err)
	}
	return buildProvider(&resp)
}

func TestBuildProviderMapsListingFields(t *testing.T) {
	p, err := build(t,
		row("deepseek-flash", "DeepSeek Flash", `,"supported_parameters":["tools","reasoning"],"max_output_tokens":16000`),
		row("kimi-k2.6", "kimi-k2.6", `,"supported_parameters":["tools","reasoning"]`),
		row("plain", "", ``),
	)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != catwalk.InferenceProviderDotHoosh || p.Type != catwalk.TypeOpenAICompat || p.APIKey != "$DOTHOOSH_API_KEY" {
		t.Fatalf("unexpected provider header: %+v", p)
	}
	byID := map[string]catwalk.Model{}
	for _, m := range p.Models {
		byID[m.ID] = m
	}
	ds := byID["deepseek-flash"]
	// Micro-dollar precision survives; cache write is "in_cached", cache read "out_cached" (CRUSH.md).
	if ds.Name != "DeepSeek Flash" || ds.CostPer1MIn != 0.385001 || ds.CostPer1MOut != 1.54 ||
		ds.CostPer1MInCached != 1.5 || ds.CostPer1MOutCached != 0.007701 {
		t.Errorf("name or prices wrong: %+v", ds)
	}
	if ds.ContextWindow != 128000 || ds.DefaultMaxTokens != 8000 || ds.DefaultReasoningEffort != "high" || len(ds.ReasoningLevels) != 2 {
		t.Errorf("limits or reasoning wrong: %+v", ds)
	}
	if m := byID["kimi-k2.6"]; !m.CanReason || len(m.ReasoningLevels) != 0 || m.DefaultReasoningEffort != "" {
		t.Errorf("a toggle-only model must not get effort levels: %+v", m)
	}
	if byID["plain"].Name != "plain" {
		t.Errorf("an empty listing name falls back to the id, got %q", byID["plain"].Name)
	}
	// Defaults: highest and lowest output price, ties by id.
	if p.DefaultLargeModelID != "deepseek-flash" || p.DefaultSmallModelID != "deepseek-flash" {
		t.Errorf("defaults = %s / %s", p.DefaultLargeModelID, p.DefaultSmallModelID)
	}
}

func TestSkippedAndRefusedLists(t *testing.T) {
	responses := row("both", "both", `,"protocols":[{"protocol":"openai-chat-completions"},{"protocol":"openai-responses"}]`)
	noTools := row("no-tools", "no-tools", `,"supported_parameters":["max_tokens"]`)
	p, err := build(t, row("a", "a", ``), row("b", "b", ``), row("c", "c", ``), responses, noTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Models) != 3 {
		t.Errorf("Responses-first and tool-less models must be skipped, got %d models", len(p.Models))
	}
	for name, rows := range map[string][]string{
		"short list":     {row("a", "a", ``)},
		"few usable":     {row("a", "a", ``), row("b", "b", ``), noTools},
		"duplicate id":   {row("a", "a", ``), row("b", "b", ``), row("c", "c", ``), row("a", "a", ``)},
		"missing prices": {row("a", "a", ``), row("b", "b", ``), `{"id":"c","context_length":1,"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`},
		"bad price":      {row("a", "a", ``), row("b", "b", ``), strings.Replace(row("c", "c", ``), "0.00000154", "free", 1)},
	} {
		if _, err := build(t, rows...); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestFetchModelsRejectsHTTPErrors(t *testing.T) {
	t.Chdir(t.TempDir()) // fetchModels writes tmp/dothoosh-response.json
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the listing must be fetched without a key")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := fetchModels(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error on 502")
	}
}

func TestMassRetirementIsRefused(t *testing.T) {
	prev := []string{"a", "b", "c", "d", "e", "f"}
	if err := checkRetirement(prev, []string{"a", "b", "c", "d"}); err != nil {
		t.Errorf("retiring a third is allowed: %v", err)
	}
	if err := checkRetirement(prev, []string{"a", "b", "c"}); err == nil {
		t.Error("retiring half must be refused")
	}
	ids, err := previousModelIDs(t.TempDir() + "/missing.json")
	if err != nil || !slices.Equal(ids, nil) {
		t.Errorf("a missing catalog means nothing to protect: %v, %v", ids, err)
	}
}
