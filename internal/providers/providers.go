// Package providers provides a registry of inference providers
package providers

import (
	_ "embed"
	"encoding/json"
	"log"

	"charm.land/catwalk/pkg/catwalk"
)

//go:embed configs/aihubmix.json
var aiHubMixConfig []byte

//go:embed configs/anthropic.json
var anthropicConfig []byte

//go:embed configs/avian.json
var avianConfig []byte

//go:embed configs/azure.json
var azureConfig []byte

//go:embed configs/bedrock.json
var bedrockConfig []byte

//go:embed configs/cerebras.json
var cerebrasConfig []byte

//go:embed configs/chutes.json
var chutesConfig []byte

//go:embed configs/copilot.json
var copilotConfig []byte

//go:embed configs/cortecs.json
var cortecsConfig []byte

//go:embed configs/deepseek.json
var deepSeekConfig []byte

//go:embed configs/gemini.json
var geminiConfig []byte

//go:embed configs/groq.json
var groqConfig []byte

//go:embed configs/huggingface.json
var huggingFaceConfig []byte

//go:embed configs/ionet.json
var ioNetConfig []byte

//go:embed configs/kimi.json
var kimiCodingConfig []byte

//go:embed configs/minimax.json
var miniMaxConfig []byte

//go:embed configs/minimax-china.json
var miniMaxChinaConfig []byte

//go:embed configs/moonshot.json
var moonshotConfig []byte

//go:embed configs/moonshot-cn.json
var moonshotCNConfig []byte

//go:embed configs/nebius.json
var nebiusConfig []byte

//go:embed configs/neuralwatt.json
var neuralwattConfig []byte

//go:embed configs/openai.json
var openAIConfig []byte

//go:embed configs/opencode-go.json
var openCodeGoConfig []byte

//go:embed configs/opencode-zen.json
var openCodeZenConfig []byte

//go:embed configs/openrouter.json
var openRouterConfig []byte

//go:embed configs/qiniucloud.json
var qiniuCloudConfig []byte

//go:embed configs/synthetic.json
var syntheticConfig []byte

//go:embed configs/vercel.json
var vercelConfig []byte

//go:embed configs/venice.json
var veniceConfig []byte

//go:embed configs/vertexai.json
var vertexAIConfig []byte

//go:embed configs/xai.json
var xAIConfig []byte

//go:embed configs/zai.json
var zAIConfig []byte

//go:embed configs/zhipu.json
var zhipuConfig []byte

//go:embed configs/zhipu-coding.json
var zhipuCodingConfig []byte

// ProviderFunc is a function that returns a Provider.
type ProviderFunc func() catwalk.Provider

var providerRegistry = []ProviderFunc{
	// Let's keep the main providers at the top.
	anthropicProvider,
	openAIProvider,
	geminiProvider,
	xAIProvider,
	zAIProvider,
	kimiCodingProvider,
	miniMaxProvider,
	miniMaxChinaProvider,
	moonshotProvider,
	moonshotCNProvider,
	syntheticProvider,

	// The remaining will be in alphabetical order.
	aiHubMixProvider,
	avianProvider,
	azureProvider,
	bedrockProvider,
	cerebrasProvider,
	chutesProvider,
	copilotProvider,
	cortecsProvider,
	deepSeekProvider,
	groqProvider,
	huggingFaceProvider,
	ioNetProvider,
	nebiusProvider,
	neuralwattProvider,
	openCodeGoProvider,
	openCodeZenProvider,
	openRouterProvider,
	qiniuCloudProvider,
	vercelProvider,
	veniceProvider,
	vertexAIProvider,
	zhipuProvider,
	zhipuCodingProvider,
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

func aiHubMixProvider() catwalk.Provider {
	return loadProviderFromConfig(aiHubMixConfig)
}

func anthropicProvider() catwalk.Provider {
	return loadProviderFromConfig(anthropicConfig)
}

func avianProvider() catwalk.Provider {
	return loadProviderFromConfig(avianConfig)
}

func azureProvider() catwalk.Provider {
	return loadProviderFromConfig(azureConfig)
}

func bedrockProvider() catwalk.Provider {
	return loadProviderFromConfig(bedrockConfig)
}

func cerebrasProvider() catwalk.Provider {
	return loadProviderFromConfig(cerebrasConfig)
}

func chutesProvider() catwalk.Provider {
	return loadProviderFromConfig(chutesConfig)
}

func copilotProvider() catwalk.Provider {
	return loadProviderFromConfig(copilotConfig)
}

func cortecsProvider() catwalk.Provider {
	return loadProviderFromConfig(cortecsConfig)
}

func deepSeekProvider() catwalk.Provider {
	return loadProviderFromConfig(deepSeekConfig)
}

func geminiProvider() catwalk.Provider {
	return loadProviderFromConfig(geminiConfig)
}

func groqProvider() catwalk.Provider {
	return loadProviderFromConfig(groqConfig)
}

func huggingFaceProvider() catwalk.Provider {
	return loadProviderFromConfig(huggingFaceConfig)
}

func ioNetProvider() catwalk.Provider {
	return loadProviderFromConfig(ioNetConfig)
}

func kimiCodingProvider() catwalk.Provider {
	return loadProviderFromConfig(kimiCodingConfig)
}

func miniMaxProvider() catwalk.Provider {
	return loadProviderFromConfig(miniMaxConfig)
}

func miniMaxChinaProvider() catwalk.Provider {
	return loadProviderFromConfig(miniMaxChinaConfig)
}

func moonshotProvider() catwalk.Provider {
	return loadProviderFromConfig(moonshotConfig)
}

func moonshotCNProvider() catwalk.Provider {
	return loadProviderFromConfig(moonshotCNConfig)
}

func nebiusProvider() catwalk.Provider {
	return loadProviderFromConfig(nebiusConfig)
}

func neuralwattProvider() catwalk.Provider {
	return loadProviderFromConfig(neuralwattConfig)
}

func openAIProvider() catwalk.Provider {
	return loadProviderFromConfig(openAIConfig)
}

func openCodeGoProvider() catwalk.Provider {
	return loadProviderFromConfig(openCodeGoConfig)
}

func openCodeZenProvider() catwalk.Provider {
	return loadProviderFromConfig(openCodeZenConfig)
}

func openRouterProvider() catwalk.Provider {
	return loadProviderFromConfig(openRouterConfig)
}

func qiniuCloudProvider() catwalk.Provider {
	return loadProviderFromConfig(qiniuCloudConfig)
}

func syntheticProvider() catwalk.Provider {
	return loadProviderFromConfig(syntheticConfig)
}

func vercelProvider() catwalk.Provider {
	return loadProviderFromConfig(vercelConfig)
}

func veniceProvider() catwalk.Provider {
	return loadProviderFromConfig(veniceConfig)
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

func zhipuProvider() catwalk.Provider {
	return loadProviderFromConfig(zhipuConfig)
}

func zhipuCodingProvider() catwalk.Provider {
	return loadProviderFromConfig(zhipuCodingConfig)
}
