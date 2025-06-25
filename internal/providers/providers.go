package providers

import (
	_ "embed"
	"encoding/json"
	"log"

	"github.com/charmbracelet/fur/pkg/provider"
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

// ProviderFunc is a function that returns a Provider
type ProviderFunc func() provider.Provider

var providerRegistry = map[provider.InferenceProvider]ProviderFunc{
	provider.InferenceProviderOpenAI:     openAIProvider,
	provider.InferenceProviderAnthropic:  anthropicProvider,
	provider.InferenceProviderGemini:     geminiProvider,
	provider.InferenceProviderAzure:      azureProvider,
	provider.InferenceProviderBedrock:    bedrockProvider,
	provider.InferenceProviderVertexAI:   vertexAIProvider,
	provider.InferenceProviderXAI:        xAIProvider,
	provider.InferenceProviderOpenRouter: openRouterProvider,
}

func GetAll() []provider.Provider {
	providers := make([]provider.Provider, 0, len(providerRegistry))
	for _, providerFunc := range providerRegistry {
		providers = append(providers, providerFunc())
	}
	return providers
}

func GetByID(id provider.InferenceProvider) (provider.Provider, bool) {
	providerFunc, exists := providerRegistry[id]
	if !exists {
		return provider.Provider{}, false
	}
	return providerFunc(), true
}

func GetAvailableIDs() []provider.InferenceProvider {
	ids := make([]provider.InferenceProvider, 0, len(providerRegistry))
	for id := range providerRegistry {
		ids = append(ids, id)
	}
	return ids
}

func loadProviderFromConfig(configData []byte) provider.Provider {
	var p provider.Provider
	if err := json.Unmarshal(configData, &p); err != nil {
		log.Printf("Error loading provider config: %v", err)
		return provider.Provider{}
	}
	return p
}

func openAIProvider() provider.Provider {
	return loadProviderFromConfig(openAIConfig)
}

func anthropicProvider() provider.Provider {
	return loadProviderFromConfig(anthropicConfig)
}

func geminiProvider() provider.Provider {
	return loadProviderFromConfig(geminiConfig)
}

func azureProvider() provider.Provider {
	return loadProviderFromConfig(azureConfig)
}

func bedrockProvider() provider.Provider {
	return loadProviderFromConfig(bedrockConfig)
}

func vertexAIProvider() provider.Provider {
	return loadProviderFromConfig(vertexAIConfig)
}

func xAIProvider() provider.Provider {
	return loadProviderFromConfig(xAIConfig)
}

func openRouterProvider() provider.Provider {
	return loadProviderFromConfig(openRouterConfig)
}
