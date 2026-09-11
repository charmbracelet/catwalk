package providers

import (
	"slices"
	"testing"
)

func TestValidPricingOverrides(t *testing.T) {
	for _, p := range GetAll() {
		t.Run(p.Name, func(t *testing.T) {
			for _, m := range p.Models {
				for i, override := range m.PricingOverrides {
					if err := override.Validate(); err != nil {
						t.Errorf("model %q pricing_overrides[%d]: %v", m.ID, i, err)
					}
				}
			}
		})
	}
}

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
