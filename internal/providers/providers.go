// Package providers provides a registry of inference providers
package providers

import (
	_ "embed"
	"encoding/json"
	"log"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
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

//go:embed configs/zai.json
var zAIConfig []byte

//go:embed configs/bedrock.json
var bedrockConfig []byte

//go:embed configs/groq.json
var groqConfig []byte

//go:embed configs/lambda.json
var lambdaConfig []byte

// ProviderFunc is a function that returns a Provider.
type ProviderFunc func() catwalk.Provider

var providerRegistry = []ProviderFunc{
	anthropicProvider,
	openAIProvider,
	geminiProvider,
	azureProvider,
	bedrockProvider,
	vertexAIProvider,
	xAIProvider,
	zAIProvider,
	groqProvider,
	openRouterProvider,
	lambdaProvider,
}

// GetAll returns all registered providers.
func GetAll() []catwalk.Provider {
	providers := make([]catwalk.Provider, 0, len(providerRegistry))
	for _, providerFunc := range providerRegistry {
		providers = append(providers, providerFunc())
	}
	return providers
}

func loadProviderFromConfig(configData []byte) catwalk.Provider {
	var p catwalk.Provider
	if err := json.Unmarshal(configData, &p); err != nil {
		log.Printf("Error loading provider config: %v", err)
		return catwalk.Provider{}
	}
	return p
}

func openAIProvider() catwalk.Provider {
	return loadProviderFromConfig(openAIConfig)
}

func anthropicProvider() catwalk.Provider {
	return loadProviderFromConfig(anthropicConfig)
}

func geminiProvider() catwalk.Provider {
	return loadProviderFromConfig(geminiConfig)
}

func azureProvider() catwalk.Provider {
	return loadProviderFromConfig(azureConfig)
}

func bedrockProvider() catwalk.Provider {
	return loadProviderFromConfig(bedrockConfig)
}

func vertexAIProvider() catwalk.Provider {
	return loadProviderFromConfig(vertexAIConfig)
}

func xAIProvider() catwalk.Provider {
	return loadProviderFromConfig(xAIConfig)
}

func zAIProvider() catwalk.Provider {
	return loadProviderFromConfig(zAIConfig)
}

func openRouterProvider() catwalk.Provider {
	return loadProviderFromConfig(openRouterConfig)
}

func groqProvider() catwalk.Provider {
	return loadProviderFromConfig(groqConfig)
}

func lambdaProvider() catwalk.Provider {
	return loadProviderFromConfig(lambdaConfig)
}
