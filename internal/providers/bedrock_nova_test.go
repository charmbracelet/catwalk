package providers

import (
	"regexp"
	"testing"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

// Feature: amazon-nova-bedrock-support, Property 5: Catwalk Model Configuration Validity
// For all Nova model entries in the Bedrock catwalk configuration, each entry should have:
// (1) a valid model ID matching the pattern "amazon.nova-*-v*:*",
// (2) non-negative pricing values for input and output tokens,
// (3) a positive context window size,
// (4) a positive default max tokens value,
// (5) a boolean supports_attachments field.
// Validates: Requirements 2.2, 2.3, 2.4, 2.5, 2.6
func TestProperty_CatwalkModelConfigurationValidity(t *testing.T) {
	// Get the Bedrock provider configuration
	provider := bedrockProvider()

	// Define the expected Nova model IDs
	expectedNovaModels := []string{
		"amazon.nova-pro-v1:0",
		"amazon.nova-lite-v1:0",
		"amazon.nova-micro-v1:0",
		"amazon.nova-premier-v1:0",
	}

	// Pattern for valid Nova model IDs
	novaModelPattern := regexp.MustCompile(`^amazon\.nova-[a-z]+-v\d+:\d+$`)

	// Find all Nova models in the configuration
	var novaModels []catwalk.Model
	for _, model := range provider.Models {
		if novaModelPattern.MatchString(model.ID) {
			novaModels = append(novaModels, model)
		}
	}

	// Property 1: All expected Nova models should be present
	if len(novaModels) != len(expectedNovaModels) {
		t.Errorf("Expected %d Nova models, found %d", len(expectedNovaModels), len(novaModels))
	}

	foundModels := make(map[string]bool)
	for _, model := range novaModels {
		foundModels[model.ID] = true
	}

	for _, expectedID := range expectedNovaModels {
		if !foundModels[expectedID] {
			t.Errorf("Expected Nova model %q not found in configuration", expectedID)
		}
	}

	// Property 2-5: Validate each Nova model configuration
	for _, model := range novaModels {
		t.Run(model.ID, func(t *testing.T) {
			// Property 2.1: Model ID should match the pattern
			if !novaModelPattern.MatchString(model.ID) {
				t.Errorf("Model ID %q does not match expected pattern amazon.nova-*-v*:*", model.ID)
			}

			// Property 2.2: Pricing values should be non-negative
			if model.CostPer1MIn < 0 {
				t.Errorf("Model %q has negative input cost: %f", model.ID, model.CostPer1MIn)
			}
			if model.CostPer1MOut < 0 {
				t.Errorf("Model %q has negative output cost: %f", model.ID, model.CostPer1MOut)
			}
			if model.CostPer1MInCached < 0 {
				t.Errorf("Model %q has negative cached input cost: %f", model.ID, model.CostPer1MInCached)
			}
			if model.CostPer1MOutCached < 0 {
				t.Errorf("Model %q has negative cached output cost: %f", model.ID, model.CostPer1MOutCached)
			}

			// Property 2.3: Context window should be positive
			if model.ContextWindow <= 0 {
				t.Errorf("Model %q has non-positive context window: %d", model.ID, model.ContextWindow)
			}

			// Property 2.4: Default max tokens should be positive
			if model.DefaultMaxTokens <= 0 {
				t.Errorf("Model %q has non-positive default max tokens: %d", model.ID, model.DefaultMaxTokens)
			}

			// Property 2.5: Model should have a name
			if model.Name == "" {
				t.Errorf("Model %q has empty name", model.ID)
			}

			// Additional validation: Verify specific model characteristics
			switch model.ID {
			case "amazon.nova-pro-v1:0":
				if model.CostPer1MIn != 0.8 {
					t.Errorf("Nova Pro input cost should be 0.8, got %f", model.CostPer1MIn)
				}
				if model.CostPer1MOut != 3.2 {
					t.Errorf("Nova Pro output cost should be 3.2, got %f", model.CostPer1MOut)
				}
				if model.ContextWindow != 300000 {
					t.Errorf("Nova Pro context window should be 300000, got %d", model.ContextWindow)
				}
				if !model.SupportsImages {
					t.Errorf("Nova Pro should support attachments")
				}

			case "amazon.nova-lite-v1:0":
				if model.CostPer1MIn != 0.06 {
					t.Errorf("Nova Lite input cost should be 0.06, got %f", model.CostPer1MIn)
				}
				if model.CostPer1MOut != 0.24 {
					t.Errorf("Nova Lite output cost should be 0.24, got %f", model.CostPer1MOut)
				}
				if model.ContextWindow != 300000 {
					t.Errorf("Nova Lite context window should be 300000, got %d", model.ContextWindow)
				}
				if !model.SupportsImages {
					t.Errorf("Nova Lite should support attachments")
				}

			case "amazon.nova-micro-v1:0":
				if model.CostPer1MIn != 0.035 {
					t.Errorf("Nova Micro input cost should be 0.035, got %f", model.CostPer1MIn)
				}
				if model.CostPer1MOut != 0.14 {
					t.Errorf("Nova Micro output cost should be 0.14, got %f", model.CostPer1MOut)
				}
				if model.ContextWindow != 128000 {
					t.Errorf("Nova Micro context window should be 128000, got %d", model.ContextWindow)
				}
				if model.SupportsImages {
					t.Errorf("Nova Micro should not support attachments")
				}

			case "amazon.nova-premier-v1:0":
				if model.CostPer1MIn != 2.5 {
					t.Errorf("Nova Premier input cost should be 2.5, got %f", model.CostPer1MIn)
				}
				if model.CostPer1MOut != 12.5 {
					t.Errorf("Nova Premier output cost should be 12.5, got %f", model.CostPer1MOut)
				}
				if model.ContextWindow != 300000 {
					t.Errorf("Nova Premier context window should be 300000, got %d", model.ContextWindow)
				}
				if !model.CanReason {
					t.Errorf("Nova Premier should have reasoning capability")
				}
				if !model.SupportsImages {
					t.Errorf("Nova Premier should support attachments")
				}
			}
		})
	}
}

// TestNovaModelsPresent verifies that all Nova models are present in the Bedrock configuration.
// Validates: Requirements 2.1, 2.6
func TestNovaModelsPresent(t *testing.T) {
	provider := bedrockProvider()

	expectedModels := map[string]string{
		"amazon.nova-pro-v1:0":     "Amazon Nova Pro",
		"amazon.nova-lite-v1:0":    "Amazon Nova Lite",
		"amazon.nova-micro-v1:0":   "Amazon Nova Micro",
		"amazon.nova-premier-v1:0": "Amazon Nova Premier",
	}

	// Build a map of actual models
	actualModels := make(map[string]catwalk.Model)
	for _, model := range provider.Models {
		actualModels[model.ID] = model
	}

	// Verify each expected model is present
	for expectedID, expectedName := range expectedModels {
		model, found := actualModels[expectedID]
		if !found {
			t.Errorf("Expected Nova model %q not found in Bedrock provider", expectedID)
			continue
		}

		if model.Name != expectedName {
			t.Errorf("Model %q has incorrect name: expected %q, got %q", expectedID, expectedName, model.Name)
		}
	}
}

// TestNovaModelIDFormat verifies that all Nova model IDs match the expected format.
// Validates: Requirements 2.1, 2.6
func TestNovaModelIDFormat(t *testing.T) {
	provider := bedrockProvider()

	// Pattern for valid Nova model IDs: amazon.nova-{variant}-v{version}:{revision}
	novaModelPattern := regexp.MustCompile(`^amazon\.nova-[a-z]+-v\d+:\d+$`)

	expectedNovaModels := []string{
		"amazon.nova-pro-v1:0",
		"amazon.nova-lite-v1:0",
		"amazon.nova-micro-v1:0",
		"amazon.nova-premier-v1:0",
	}

	for _, expectedID := range expectedNovaModels {
		// Find the model
		var found bool
		for _, model := range provider.Models {
			if model.ID == expectedID {
				found = true

				// Verify the ID matches the pattern
				if !novaModelPattern.MatchString(model.ID) {
					t.Errorf("Nova model ID %q does not match expected pattern amazon.nova-*-v*:*", model.ID)
				}

				// Verify the ID starts with "amazon."
				if model.ID[:7] != "amazon." {
					t.Errorf("Nova model ID %q should start with 'amazon.'", model.ID)
				}

				break
			}
		}

		if !found {
			t.Errorf("Expected Nova model %q not found in configuration", expectedID)
		}
	}
}

// TestNovaPricingNonNegative verifies that all Nova models have non-negative pricing values.
// Validates: Requirements 2.1, 2.6
func TestNovaPricingNonNegative(t *testing.T) {
	provider := bedrockProvider()

	novaModelPattern := regexp.MustCompile(`^amazon\.nova-`)

	for _, model := range provider.Models {
		if !novaModelPattern.MatchString(model.ID) {
			continue
		}

		t.Run(model.ID, func(t *testing.T) {
			if model.CostPer1MIn < 0 {
				t.Errorf("Model %q has negative input cost: %f", model.ID, model.CostPer1MIn)
			}

			if model.CostPer1MOut < 0 {
				t.Errorf("Model %q has negative output cost: %f", model.ID, model.CostPer1MOut)
			}

			if model.CostPer1MInCached < 0 {
				t.Errorf("Model %q has negative cached input cost: %f", model.ID, model.CostPer1MInCached)
			}

			if model.CostPer1MOutCached < 0 {
				t.Errorf("Model %q has negative cached output cost: %f", model.ID, model.CostPer1MOutCached)
			}

			// Verify pricing is reasonable (not zero for non-cached costs)
			if model.CostPer1MIn == 0 {
				t.Errorf("Model %q has zero input cost, which is likely incorrect", model.ID)
			}

			if model.CostPer1MOut == 0 {
				t.Errorf("Model %q has zero output cost, which is likely incorrect", model.ID)
			}
		})
	}
}

// TestNovaModelContextWindows verifies that Nova models have appropriate context window sizes.
// Validates: Requirements 2.1, 2.6
func TestNovaModelContextWindows(t *testing.T) {
	provider := bedrockProvider()

	expectedContextWindows := map[string]int64{
		"amazon.nova-pro-v1:0":     300000,
		"amazon.nova-lite-v1:0":    300000,
		"amazon.nova-micro-v1:0":   128000,
		"amazon.nova-premier-v1:0": 300000,
	}

	for modelID, expectedWindow := range expectedContextWindows {
		var found bool
		for _, model := range provider.Models {
			if model.ID == modelID {
				found = true

				if model.ContextWindow != expectedWindow {
					t.Errorf("Model %q has incorrect context window: expected %d, got %d",
						modelID, expectedWindow, model.ContextWindow)
				}

				if model.ContextWindow <= 0 {
					t.Errorf("Model %q has non-positive context window: %d", modelID, model.ContextWindow)
				}

				break
			}
		}

		if !found {
			t.Errorf("Expected Nova model %q not found in configuration", modelID)
		}
	}
}

// TestNovaModelDefaultMaxTokens verifies that Nova models have appropriate default max tokens.
// Validates: Requirements 2.1, 2.6
func TestNovaModelDefaultMaxTokens(t *testing.T) {
	provider := bedrockProvider()

	novaModelPattern := regexp.MustCompile(`^amazon\.nova-`)

	for _, model := range provider.Models {
		if !novaModelPattern.MatchString(model.ID) {
			continue
		}

		t.Run(model.ID, func(t *testing.T) {
			if model.DefaultMaxTokens <= 0 {
				t.Errorf("Model %q has non-positive default max tokens: %d", model.ID, model.DefaultMaxTokens)
			}

			// Verify default max tokens is reasonable (should be 5000 for Nova models)
			if model.DefaultMaxTokens != 5000 {
				t.Errorf("Model %q has unexpected default max tokens: expected 5000, got %d",
					model.ID, model.DefaultMaxTokens)
			}
		})
	}
}

// TestNovaModelAttachmentSupport verifies that Nova models have correct attachment support flags.
// Validates: Requirements 2.1, 2.6
func TestNovaModelAttachmentSupport(t *testing.T) {
	provider := bedrockProvider()

	expectedAttachmentSupport := map[string]bool{
		"amazon.nova-pro-v1:0":     true,
		"amazon.nova-lite-v1:0":    true,
		"amazon.nova-micro-v1:0":   false, // Micro does not support attachments
		"amazon.nova-premier-v1:0": true,
	}

	for modelID, expectedSupport := range expectedAttachmentSupport {
		var found bool
		for _, model := range provider.Models {
			if model.ID == modelID {
				found = true

				if model.SupportsImages != expectedSupport {
					t.Errorf("Model %q has incorrect attachment support: expected %v, got %v",
						modelID, expectedSupport, model.SupportsImages)
				}

				break
			}
		}

		if !found {
			t.Errorf("Expected Nova model %q not found in configuration", modelID)
		}
	}
}

// TestNovaModelReasoningCapability verifies that Nova Premier has reasoning capability.
// Validates: Requirements 2.1, 2.6
func TestNovaModelReasoningCapability(t *testing.T) {
	provider := bedrockProvider()

	expectedReasoning := map[string]bool{
		"amazon.nova-pro-v1:0":     false,
		"amazon.nova-lite-v1:0":    false,
		"amazon.nova-micro-v1:0":   false,
		"amazon.nova-premier-v1:0": true, // Only Premier has reasoning
	}

	for modelID, expectedCanReason := range expectedReasoning {
		var found bool
		for _, model := range provider.Models {
			if model.ID == modelID {
				found = true

				if model.CanReason != expectedCanReason {
					t.Errorf("Model %q has incorrect reasoning capability: expected %v, got %v",
						modelID, expectedCanReason, model.CanReason)
				}

				break
			}
		}

		if !found {
			t.Errorf("Expected Nova model %q not found in configuration", modelID)
		}
	}
}
