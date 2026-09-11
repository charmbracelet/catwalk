package catwalk

import "strings"

// Type represents the type of AI provider.
type Type string

// All the supported AI provider types.
const (
	TypeOpenAI       Type = "openai"
	TypeOpenAICompat Type = "openai-compat"
	TypeOpenRouter   Type = "openrouter"
	TypeVercel       Type = "vercel"
	TypeAnthropic    Type = "anthropic"
	TypeGoogle       Type = "google"
	TypeAzure        Type = "azure"
	TypeBedrock      Type = "bedrock"
	TypeVertexAI     Type = "google-vertex"
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

// Thinking describes when a model reasons.
type Thinking string

// All the supported thinking modes.
const (
	// ThinkingAlways means the model always thinks and reasoning cannot be
	// turned off.
	ThinkingAlways Thinking = "always"
	// ThinkingNever means the model cannot think.
	ThinkingNever Thinking = "never"
	// ThinkingToggleable means thinking can be turned on and off, and
	// optionally configured with an effort level.
	ThinkingToggleable Thinking = "toggleable"
)

// EffortLevel is a selectable reasoning effort level.
type EffortLevel struct {
	Value   string `json:"value"`
	Display string `json:"display"`
}

// Reasoning describes how reasoning is configured for a model.
type Reasoning struct {
	Thinking Thinking `json:"thinking"`
	// EffortLevels are the selectable effort levels. Only set for models with
	// toggleable thinking that support effort levels.
	EffortLevels []EffortLevel `json:"effort_levels,omitempty"`
	// DefaultEffortLevel is the effort level used when none is selected. Only
	// set when EffortLevels is not empty.
	DefaultEffortLevel string `json:"default_effort_level,omitempty"`
}

// NewEffortLevels builds effort levels from their values, deriving the
// display name of each level.
func NewEffortLevels(values ...string) []EffortLevel {
	levels := make([]EffortLevel, 0, len(values))
	for _, value := range values {
		levels = append(levels, EffortLevel{Value: value, Display: effortLevelDisplay(value)})
	}
	return levels
}

func effortLevelDisplay(value string) string {
	switch value {
	case "":
		return ""
	case "xhigh":
		return "X-High"
	default:
		return strings.ToUpper(value[:1]) + value[1:]
	}
}

// Model represents an AI model configuration.
type Model struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Pricing          Pricing      `json:"pricing"`
	ContextWindow    int64        `json:"context_window"`
	DefaultMaxTokens int64        `json:"default_max_tokens"`
	Reasoning        Reasoning    `json:"reasoning"`
	Capabilities     Capabilities `json:"capabilities"`
	Options          ModelOptions `json:"options,omitzero"`
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

// KnownProviderTypes returns all the known inference providers types.
func KnownProviderTypes() []Type {
	return []Type{
		TypeOpenAI,
		TypeOpenAICompat,
		TypeOpenRouter,
		TypeVercel,
		TypeAnthropic,
		TypeGoogle,
		TypeAzure,
		TypeBedrock,
		TypeVertexAI,
	}
}
