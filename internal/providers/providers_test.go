package providers

import (
	"regexp"
	"slices"
	"testing"
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
