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

func TestSiliconFlowProvidersRegistered(t *testing.T) {
	tests := []struct {
		id          catwalk.InferenceProvider
		name        string
		apiEndpoint string
	}{
		{
			id:          "siliconflow-cn",
			name:        "SiliconFlow CN",
			apiEndpoint: "https://api.siliconflow.cn/v1",
		},
		{
			id:          "siliconflow",
			name:        "SiliconFlow",
			apiEndpoint: "https://api.siliconflow.com/v1",
		},
	}

	providersByID := make(map[catwalk.InferenceProvider]catwalk.Provider)
	for _, p := range GetAll() {
		providersByID[p.ID] = p
	}

	for _, tt := range tests {
		t.Run(string(tt.id), func(t *testing.T) {
			p, ok := providersByID[tt.id]
			if !ok {
				t.Fatalf("Provider %q was not registered", tt.id)
			}
			if p.Name != tt.name {
				t.Fatalf("Provider name = %q, want %q", p.Name, tt.name)
			}
			if p.APIEndpoint != tt.apiEndpoint {
				t.Fatalf("API endpoint = %q, want %q", p.APIEndpoint, tt.apiEndpoint)
			}
			if p.Type != catwalk.TypeOpenAICompat {
				t.Fatalf("Provider type = %q, want %q", p.Type, catwalk.TypeOpenAICompat)
			}
			if len(p.Models) == 0 {
				t.Fatal("Provider has no models")
			}

			modelIDs := make([]string, 0, len(p.Models))
			for _, m := range p.Models {
				modelIDs = append(modelIDs, m.ID)
			}
			if !slices.Contains(modelIDs, p.DefaultLargeModelID) {
				t.Fatalf("Default large model %q not found", p.DefaultLargeModelID)
			}
			if !slices.Contains(modelIDs, p.DefaultSmallModelID) {
				t.Fatalf("Default small model %q not found", p.DefaultSmallModelID)
			}
		})
	}
}
