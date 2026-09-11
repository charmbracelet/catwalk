package catwalk

// Type represents the API endpoint format a provider (or model) speaks.
type Type string

// All the supported API endpoint types.
const (
	// TypeCompletions is the OpenAI Chat Completions endpoint
	// (POST /chat/completions).
	TypeCompletions Type = "completions"
	// TypeResponses is the OpenAI Responses endpoint (POST /responses).
	TypeResponses Type = "responses"
	// TypeMessages is the Anthropic Messages endpoint (POST /messages).
	TypeMessages Type = "messages"
)

// InferenceProvider represents the inference provider identifier.
type InferenceProvider string

// All the inference providers supported by the system.
const (
	InferenceProviderOpenAI           InferenceProvider = "openai"
	InferenceProviderAnthropic        InferenceProvider = "anthropic"
	InferenceProviderSynthetic        InferenceProvider = "synthetic"
	InferenceProviderGemini           InferenceProvider = "gemini"
	InferenceProviderAzure            InferenceProvider = "azure"
	InferenceProviderBedrock          InferenceProvider = "bedrock"
	InferenceProviderBedrockEurope    InferenceProvider = "bedrock-europe"
	InferenceProviderVertexAI         InferenceProvider = "vertexai"
	InferenceProviderXAI              InferenceProvider = "xai"
	InferenceProviderZAI              InferenceProvider = "zai"
	InferenceProviderDeepSeek         InferenceProvider = "deepseek"
	InferenceProviderZhipu            InferenceProvider = "zhipu"
	InferenceProviderZhipuCoding      InferenceProvider = "zhipu-coding"
	InferenceProviderGROQ             InferenceProvider = "groq"
	InferenceProviderOpenRouter       InferenceProvider = "openrouter"
	InferenceProviderCerebras         InferenceProvider = "cerebras"
	InferenceProviderVenice           InferenceProvider = "venice"
	InferenceProviderChutes           InferenceProvider = "chutes"
	InferenceProviderHuggingFace      InferenceProvider = "huggingface"
	InferenceAIHubMix                 InferenceProvider = "aihubmix"
	InferenceKimiCoding               InferenceProvider = "kimi-coding"
	InferenceProviderCopilot          InferenceProvider = "copilot"
	InferenceProviderCortecs          InferenceProvider = "cortecs"
	InferenceProviderVercel           InferenceProvider = "vercel"
	InferenceProviderMiniMax          InferenceProvider = "minimax"
	InferenceProviderMiniMaxChina     InferenceProvider = "minimax-china"
	InferenceProviderIoNet            InferenceProvider = "ionet"
	InferenceProviderQiniuCloud       InferenceProvider = "qiniucloud"
	InferenceProviderAvian            InferenceProvider = "avian"
	InferenceProviderNebius           InferenceProvider = "nebius"
	InferenceProviderNeuralwatt       InferenceProvider = "neuralwatt"
	InferenceProviderOpenCodeZen      InferenceProvider = "opencode-zen"
	InferenceProviderOpenCodeGo       InferenceProvider = "opencode-go"
	InferenceProviderAlibabaSingapore InferenceProvider = "alibaba-singapore"
	InferenceProviderAlibabaUS        InferenceProvider = "alibaba-us"
	InferenceProviderFireworks        InferenceProvider = "fireworks"
	InferenceProviderBaseten          InferenceProvider = "baseten"
	InferenceProviderMoonshot         InferenceProvider = "moonshot"
	InferenceProviderAtlasCloud       InferenceProvider = "atlascloud"
)

// Provider represents an AI provider configuration.
type Provider struct {
	Name                string            `json:"name"`
	ID                  InferenceProvider `json:"id"`
	APIKey              string            `json:"api_key,omitempty"`
	APIEndpoint         string            `json:"api_endpoint,omitempty"`
	Type                Type              `json:"type,omitempty"`
	DefaultLargeModelID string            `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string            `json:"default_small_model_id,omitempty"`
	Models              []Model           `json:"models,omitempty"`
	DefaultHeaders      map[string]string `json:"default_headers,omitempty"`
}

// ModelOptions stores extra options for models.
type ModelOptions struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	TopK             *int64         `json:"top_k,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	ProviderOptions  map[string]any `json:"provider_options,omitempty"`
}

// Pricing stores the pricing of a model in US dollars per 1M tokens.
type Pricing struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheCreate float64 `json:"cache_create,omitempty"`
	CacheHit    float64 `json:"cache_hit,omitempty"`
}

// Capabilities describes the capabilities of a model. Each capability is
// always serialized, even when false, so consumers can distinguish between
// "unknown" (field absent) and "explicitly unsupported".
type Capabilities struct {
	Vision bool `json:"vision"`
}

// Model represents an AI model configuration.
type Model struct {
	ID string `json:"id"`
	// Type optionally overrides the provider's endpoint type for this model.
	// When empty, the provider's Type is used.
	Type                   Type         `json:"type,omitempty"`
	Name                   string       `json:"name"`
	Pricing                Pricing      `json:"pricing"`
	ContextWindow          int64        `json:"context_window"`
	DefaultMaxTokens       int64        `json:"default_max_tokens"`
	CanReason              bool         `json:"can_reason"`
	ReasoningLevels        []string     `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string       `json:"default_reasoning_effort,omitempty"`
	Capabilities           Capabilities `json:"capabilities"`
	Options                ModelOptions `json:"options,omitzero"`
}

// EffectiveType returns the endpoint type for this model, falling back to the
// provider's Type when the model does not override it.
func (m Model) EffectiveType(providerType Type) Type {
	if m.Type != "" {
		return m.Type
	}
	return providerType
}

// KnownProviders returns all the known inference providers.
func KnownProviders() []InferenceProvider {
	return []InferenceProvider{
		InferenceProviderOpenAI,
		InferenceProviderSynthetic,
		InferenceProviderAnthropic,
		InferenceProviderGemini,
		InferenceProviderAzure,
		InferenceProviderBedrock,
		InferenceProviderBedrockEurope,
		InferenceProviderVertexAI,
		InferenceProviderXAI,
		InferenceProviderZAI,
		InferenceProviderZhipu,
		InferenceProviderZhipuCoding,
		InferenceProviderGROQ,
		InferenceProviderOpenRouter,
		InferenceProviderCerebras,
		InferenceProviderVenice,
		InferenceProviderChutes,
		InferenceProviderHuggingFace,
		InferenceAIHubMix,
		InferenceKimiCoding,
		InferenceProviderCopilot,
		InferenceProviderCortecs,
		InferenceProviderVercel,
		InferenceProviderMiniMax,
		InferenceProviderMiniMaxChina,
		InferenceProviderQiniuCloud,
		InferenceProviderAvian,
		InferenceProviderNebius,
		InferenceProviderNeuralwatt,
		InferenceProviderOpenCodeZen,
		InferenceProviderOpenCodeGo,
		InferenceProviderFireworks,
		InferenceProviderBaseten,
		InferenceProviderMoonshot,
		InferenceProviderAtlasCloud,
	}
}

// KnownProviderTypes returns all the known API endpoint types.
func KnownProviderTypes() []Type {
	return []Type{
		TypeCompletions,
		TypeResponses,
		TypeMessages,
	}
}
