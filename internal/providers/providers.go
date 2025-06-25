package providers

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

//go:embed configs/bedrock.json
var bedrockConfig []byte

func loadProviderFromConfig(configData []byte) Provider {
	var provider Provider
	if err := json.Unmarshal(configData, &provider); err != nil {
		log.Printf("Error loading provider config: %v", err)
		return Provider{}
	}
	return provider
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

func openRouterProvider() Provider {
	return loadProviderFromConfig(openRouterConfig)
}
