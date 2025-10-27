package deprecated

import (
	_ "embed"
	"encoding/json"
	"log"
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

// ProviderFunc is a function that returns a Provider.
type ProviderFunc func() Provider

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
	cerebrasProvider,
	veniceProvider,
	chutesProvider,
	deepSeekProvider,
	huggingFaceProvider,
	aiHubMixProvider,
}

// GetAll returns all registered providers.
func GetAll() []Provider {
	providers := make([]Provider, 0, len(providerRegistry))
	for _, providerFunc := range providerRegistry {
		providers = append(providers, providerFunc())
	}
	return providers
}

func loadProviderFromConfig(configData []byte) Provider {
	var p Provider
	if err := json.Unmarshal(configData, &p); err != nil {
		log.Printf("Error loading provider config: %v", err)
		return Provider{}
	}
	return p
}

func openAIProvider() Provider {
	return loadProviderFromConfig(openAIConfig)
}

func anthropicProvider() Provider {
	return loadProviderFromConfig(anthropicConfig)
}

func geminiProvider() Provider {
	return loadProviderFromConfig(geminiConfig)
}

func azureProvider() Provider {
	return loadProviderFromConfig(azureConfig)
}

func bedrockProvider() Provider {
	return loadProviderFromConfig(bedrockConfig)
}

func vertexAIProvider() Provider {
	return loadProviderFromConfig(vertexAIConfig)
}

func xAIProvider() Provider {
	return loadProviderFromConfig(xAIConfig)
}

func zAIProvider() Provider {
	return loadProviderFromConfig(zAIConfig)
}

func openRouterProvider() Provider {
	return loadProviderFromConfig(openRouterConfig)
}

func groqProvider() Provider {
	return loadProviderFromConfig(groqConfig)
}

func cerebrasProvider() Provider {
	return loadProviderFromConfig(cerebrasConfig)
}

func veniceProvider() Provider {
	return loadProviderFromConfig(veniceConfig)
}

func chutesProvider() Provider {
	return loadProviderFromConfig(chutesConfig)
}

func deepSeekProvider() Provider {
	return loadProviderFromConfig(deepSeekConfig)
}

func huggingFaceProvider() Provider {
	return loadProviderFromConfig(huggingFaceConfig)
}

func aiHubMixProvider() Provider {
	return loadProviderFromConfig(aiHubMixConfig)
}
