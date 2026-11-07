package main

import (
	"slices"
	"testing"
)

// Minimal API catalog containing both eligible default models.
func catalogFixture() ModelsResponse {
	contextWindow, outputTokens := int64(128_000), int64(8_000)
	var response ModelsResponse
	for _, id := range []string{"deepseek/deepseek-v4.1-flash", "qwen/qwen3.7-flash"} {
		response.Data = append(response.Data, PrimeInferenceModel{
			ID: id,
			Specs: Specs{
				ContextWindow:   &contextWindow,
				MaxOutputTokens: &outputTokens,
				Modalities:      Modalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			SupportedParameters: []string{"tools"},
		})
	}
	return response
}

func TestGenerateProviderRejectsIncompleteCatalog(t *testing.T) {
	for _, scenario := range []string{"empty", "large missing", "small lacks tools", "small lacks text output"} {
		t.Run(scenario, func(t *testing.T) {
			response := catalogFixture()
			switch scenario {
			case "empty":
				response.Data = nil
			case "large missing":
				response.Data = slices.DeleteFunc(response.Data, func(m PrimeInferenceModel) bool {
					return m.ID == "deepseek/deepseek-v4.1-flash"
				})
			case "small lacks tools", "small lacks text output":
				for i := range response.Data {
					if response.Data[i].ID == "qwen/qwen3.7-flash" {
						if scenario == "small lacks tools" {
							response.Data[i].SupportedParameters = nil
							break
						}
						response.Data[i].Specs.Modalities.Output = nil
					}
				}
			}
			if _, err := generateProvider(response); err == nil {
				t.Fatal("incomplete catalog must fail before the config is overwritten")
			}
		})
	}
}

func TestGenerateProviderWithUnscopedModelID(t *testing.T) {
	response := catalogFixture()
	model := response.Data[0]
	model.ID = "unscoped-model"
	model.DisplayName = ""
	response.Data = append(response.Data, model)
	provider, err := generateProvider(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range provider.Models {
		if model.ID == "unscoped-model" {
			if model.Name != model.ID {
				t.Fatalf("name = %q, want %q", model.Name, model.ID)
			}
			return
		}
	}
	t.Fatal("unscoped model was dropped")
}
