package providers

import (
	"regexp"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

const nova2LiteModelID = "amazon.nova-2-lite-v1:0"

func novaModels(provider catwalk.Provider) []catwalk.Model {
	novaModelPattern := regexp.MustCompile(`^amazon\.nova-.+-v\d+:\d+$`)
	var models []catwalk.Model
	for _, model := range provider.Models {
		if novaModelPattern.MatchString(model.ID) {
			models = append(models, model)
		}
	}
	return models
}

func TestNova2LiteModelPresent(t *testing.T) {
	provider := bedrockUnitedStatesProvider()
	models := novaModels(provider)

	if len(models) != 1 {
		t.Fatalf("expected exactly 1 Nova model, found %d", len(models))
	}

	model := models[0]
	if model.ID != nova2LiteModelID {
		t.Fatalf("expected %q, got %q", nova2LiteModelID, model.ID)
	}
	if model.Name != "Amazon Nova 2 Lite" {
		t.Fatalf("unexpected model name: %q", model.Name)
	}
}

func TestNova2LiteModelConfiguration(t *testing.T) {
	provider := bedrockUnitedStatesProvider()

	var model catwalk.Model
	for _, m := range provider.Models {
		if m.ID == nova2LiteModelID {
			model = m
			break
		}
	}
	if model.ID == "" {
		t.Fatal("Nova 2 Lite model not found")
	}

	if model.CostPer1MIn != 0.06 {
		t.Errorf("input cost should be 0.06, got %f", model.CostPer1MIn)
	}
	if model.CostPer1MOut != 0.24 {
		t.Errorf("output cost should be 0.24, got %f", model.CostPer1MOut)
	}
	if model.ContextWindow != 1_000_000 {
		t.Errorf("context window should be 1000000, got %d", model.ContextWindow)
	}
	if model.DefaultMaxTokens != 64_000 {
		t.Errorf("default max tokens should be 64000, got %d", model.DefaultMaxTokens)
	}
	if !model.CanReason {
		t.Error("Nova 2 Lite should support extended thinking")
	}
	if !model.SupportsImages {
		t.Error("Nova 2 Lite should support attachments")
	}
}

func TestNovaGen1ModelsNotCataloged(t *testing.T) {
	provider := bedrockUnitedStatesProvider()

	legacyIDs := []string{
		"amazon.nova-pro-v1:0",
		"amazon.nova-lite-v1:0",
		"amazon.nova-micro-v1:0",
		"amazon.nova-premier-v1:0",
	}

	for _, id := range legacyIDs {
		for _, model := range provider.Models {
			if model.ID == id {
				t.Errorf("Gen 1 Nova model %q should not be in the Bedrock catalog", id)
			}
		}
	}
}
