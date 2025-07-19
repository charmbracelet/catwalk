// Package providers provides a registry of inference providers
package providers

import (
	_ "embed"
	"encoding/json"
	"log"

	"github.com/charmbracelet/fur/pkg/fur"
)

//go:embed configs/openai.json
var openAIConfig []byte

//go:embed configs/anthropic.json
var anthropicConfig []byte

//go:embed configs/gemini.json
var geminiConfig []byte

//go:embed configs/openrouter.json
var openRouterConfig []byte

//go:embed configs/azure.json
var azureConfig []byte

//go:embed configs/vertexai.json
var vertexAIConfig []byte

//go:embed configs/xai.json
var xAIConfig []byte

//go:embed configs/bedrock.json
var bedrockConfig []byte

// ProviderFunc is a function that returns a Provider.
type ProviderFunc func() fur.Provider

var providerRegistry = []ProviderFunc{
	anthropicProvider,
	openAIProvider,
	geminiProvider,
	azureProvider,
	bedrockProvider,
	vertexAIProvider,
	xAIProvider,
	openRouterProvider,
}

// GetAll returns all registered providers.
func GetAll() []fur.Provider {
	providers := make([]fur.Provider, 0, len(providerRegistry))
	for _, providerFunc := range providerRegistry {
		providers = append(providers, providerFunc())
	}
	return providers
}

func loadProviderFromConfig(configData []byte) fur.Provider {
	var p fur.Provider
	if err := json.Unmarshal(configData, &p); err != nil {
		log.Printf("Error loading provider config: %v", err)
		return fur.Provider{}
	}
	return p
}

func openAIProvider() fur.Provider {
	return loadProviderFromConfig(openAIConfig)
}

func anthropicProvider() fur.Provider {
	return loadProviderFromConfig(anthropicConfig)
}

func geminiProvider() fur.Provider {
	return loadProviderFromConfig(geminiConfig)
}

func azureProvider() fur.Provider {
	return loadProviderFromConfig(azureConfig)
}

func bedrockProvider() fur.Provider {
	return loadProviderFromConfig(bedrockConfig)
}

func vertexAIProvider() fur.Provider {
	return loadProviderFromConfig(vertexAIConfig)
}

func xAIProvider() fur.Provider {
	return loadProviderFromConfig(xAIConfig)
}

func openRouterProvider() fur.Provider {
	return loadProviderFromConfig(openRouterConfig)
}
