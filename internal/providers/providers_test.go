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

func TestKnownProvidersMatchesRegistry(t *testing.T) {
	known := catwalk.KnownProviders()

	registered := make([]catwalk.InferenceProvider, 0, len(providerRegistry))
	for _, p := range GetAll() {
		registered = append(registered, p.ID)
		if !slices.Contains(known, p.ID) {
			t.Errorf("Provider %q is registered but missing from catwalk.KnownProviders()", p.ID)
		}
	}

	for _, id := range known {
		if !slices.Contains(registered, id) {
			t.Errorf("Provider %q is in catwalk.KnownProviders() but is not registered", id)
		}
	}
}

func TestTsubasaConfiguration(t *testing.T) {
	for _, provider := range GetAll() {
		if provider.ID != catwalk.InferenceProviderTsubasa {
			continue
		}
		if provider.Type != catwalk.TypeOpenAICompat || provider.APIKey != "$TSUBASA_API_KEY" || provider.APIEndpoint != "https://api.tsubasa.sh/v1" {
			t.Fatal("Tsubasa must use its named credential and Chat Completions endpoint")
		}
		if len(provider.Models) != 2 || provider.DefaultLargeModelID != "tsubasa-pro" || provider.DefaultSmallModelID != "tsubasa-fast" {
			t.Fatal("Tsubasa must expose both public models with valid defaults")
		}
		for _, model := range provider.Models {
			if model.DefaultMaxTokens <= 0 || model.DefaultMaxTokens > model.ContextWindow || model.SupportsImages {
				t.Fatal("Tsubasa model limits and text-only capabilities must be valid")
			}
		}
		return
	}
	t.Fatal("Tsubasa is missing from the provider registry")
}
