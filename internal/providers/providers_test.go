package providers

import (
	"slices"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

func TestValidDefaultModels(t *testing.T) {
	for _, p := range GetAll() {
		t.Run(p.Name, func(t *testing.T) {
			var modelIds []string
			for _, m := range p.Models {
				modelIds = append(modelIds, m.ID)
			}
			if !slices.Contains(modelIds, p.DefaultLargeModelID) {
				t.Errorf("Default large model %q not found in provider %q", p.DefaultLargeModelID, p.Name)
			}
			if !slices.Contains(modelIds, p.DefaultSmallModelID) {
				t.Errorf("Default small model %q not found in provider %q", p.DefaultSmallModelID, p.Name)
			}
		})
	}
}

func TestGreenPTProvider(t *testing.T) {
	providers := GetAll()
	idx := slices.IndexFunc(providers, func(p catwalk.Provider) bool {
		return p.ID == catwalk.InferenceProviderGreenPT
	})
	if idx == -1 {
		t.Fatal("GreenPT provider is not registered")
	}

	provider := providers[idx]
	if provider.APIEndpoint != "https://api.greenpt.ai/v1" {
		t.Errorf("unexpected GreenPT endpoint %q", provider.APIEndpoint)
	}
	for _, id := range []string{"glm-5.2", "kimi-k2.7-code", "kimi-k3"} {
		if !slices.ContainsFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == id }) {
			t.Errorf("GreenPT model %q is not registered", id)
		}
	}
}
