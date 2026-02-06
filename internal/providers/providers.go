// Package providers provides a registry of inference providers
package providers

import (
	_ "embed"
	"encoding/json"
	"log"

	"charm.land/catwalk/pkg/catwalk"
)

//go:embed configs/openai.json
var openAIConfig []byte

//go:embed configs/anthropic.json
var anthropicConfig []byte

//go:embed configs/synthetic.json
var syntheticConfig []byte

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

//go:embed configs/cerebras.json
var cerebrasConfig []byte

//go:embed configs/venice.json
var veniceConfig []byte

//go:embed configs/chutes.json
var chutesConfig []byte

//go:embed configs/deepseek.json
var deepSeekConfig []byte

//go:embed configs/huggingface.json
var huggingFaceConfig []byte

//go:embed configs/aihubmix.json
var aiHubMixConfig []byte

//go:embed configs/kimi.json
var kimiCodingConfig []byte

//go:embed configs/copilot.json
var copilotConfig []byte

//go:embed configs/vercel.json
var vercelConfig []byte

//go:embed configs/minimax.json
var miniMaxConfig []byte

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
	kimiCodingProvider,
	groqProvider,
	openRouterProvider,
	cerebrasProvider,
	veniceProvider,
	chutesProvider,
	deepSeekProvider,
	huggingFaceProvider,
	aiHubMixProvider,
	syntheticProvider,
	copilotProvider,
	vercelProvider,
	miniMaxProvider,
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

func syntheticProvider() catwalk.Provider {
	return loadProviderFromConfig(syntheticConfig)
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

func cerebrasProvider() catwalk.Provider {
	return loadProviderFromConfig(cerebrasConfig)
}

func veniceProvider() catwalk.Provider {
	return loadProviderFromConfig(veniceConfig)
}

func chutesProvider() catwalk.Provider {
	return loadProviderFromConfig(chutesConfig)
}

func deepSeekProvider() catwalk.Provider {
	return loadProviderFromConfig(deepSeekConfig)
}

func huggingFaceProvider() catwalk.Provider {
	return loadProviderFromConfig(huggingFaceConfig)
}

func aiHubMixProvider() catwalk.Provider {
	return loadProviderFromConfig(aiHubMixConfig)
}

func kimiCodingProvider() catwalk.Provider {
	return loadProviderFromConfig(kimiCodingConfig)
}

func copilotProvider() catwalk.Provider {
	return loadProviderFromConfig(copilotConfig)
}

func vercelProvider() catwalk.Provider {
	return loadProviderFromConfig(vercelConfig)
}

func miniMaxProvider() catwalk.Provider {
	return loadProviderFromConfig(miniMaxConfig)
}
