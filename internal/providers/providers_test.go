package providers

import (
	"regexp"
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

// validSessionAffinityHeader matches a lowercase HTTP header name (e.g.
// "x-opencode-session").
var validSessionAffinityHeader = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func TestValidSessionAffinityHeader(t *testing.T) {
	for _, p := range GetAll() {
		if p.SessionAffinityHeader == "" {
			continue
		}
		t.Run(p.Name, func(t *testing.T) {
			if !validSessionAffinityHeader.MatchString(p.SessionAffinityHeader) {
				t.Errorf("Invalid session affinity header %q in provider %q", p.SessionAffinityHeader, p.Name)
			}
		})
	}
}
