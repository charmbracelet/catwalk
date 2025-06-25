package providers

type ProviderType string

const (
	ProviderTypeOpenAI     ProviderType = "openai"
	ProviderTypeAnthropic  ProviderType = "anthropic"
	ProviderTypeGemini     ProviderType = "gemini"
	ProviderTypeAzure      ProviderType = "azure"
	ProviderTypeBedrock    ProviderType = "bedrock"
	ProviderTypeVertexAI   ProviderType = "vertexai"
	ProviderTypeXAI        ProviderType = "xai"
	ProviderTypeOpenRouter ProviderType = "openrouter"
)

type InferenceProvider string

const (
	InferenceProviderOpenAI     InferenceProvider = "openai"
	InferenceProviderAnthropic  InferenceProvider = "anthropic"
	InferenceProviderGemini     InferenceProvider = "gemini"
	InferenceProviderAzure      InferenceProvider = "azure"
	InferenceProviderBedrock    InferenceProvider = "bedrock"
	InferenceProviderVertexAI   InferenceProvider = "vertexai"
	InferenceProviderXAI        InferenceProvider = "xai"
	InferenceProviderOpenRouter InferenceProvider = "openrouter"
)

type Provider struct {
	Name           string            `json:"name"`
	ID             InferenceProvider `json:"id"`
	APIKey         string            `json:"api_key,omitempty"`
	APIEndpoint    string            `json:"api_endpoint,omitempty"`
	Type           ProviderType      `json:"type,omitempty"`
	DefaultModelID string            `json:"default_model_id,omitempty"`
	Models         []Model           `json:"models,omitempty"`
}

type Model struct {
	ID                 string  `json:"id"`
	Name               string  `json:"model"`
	CostPer1MIn        float64 `json:"cost_per_1m_in"`
	CostPer1MOut       float64 `json:"cost_per_1m_out"`
	CostPer1MInCached  float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached float64 `json:"cost_per_1m_out_cached"`
	ContextWindow      int64   `json:"context_window"`
	DefaultMaxTokens   int64   `json:"default_max_tokens"`
	CanReason          bool    `json:"can_reason"`
	SupportsImages     bool    `json:"supports_attachments"`
}

type ProviderFunc func() Provider

var providerRegistry = map[InferenceProvider]ProviderFunc{
	InferenceProviderOpenAI:     openAIProvider,
	InferenceProviderAnthropic:  anthropicProvider,
	InferenceProviderGemini:     geminiProvider,
	InferenceProviderAzure:      azureProvider,
	InferenceProviderBedrock:    bedrockProvider,
	InferenceProviderVertexAI:   vertexAIProvider,
	InferenceProviderXAI:        xAIProvider,
	InferenceProviderOpenRouter: openRouterProvider,
}

func GetAll() []Provider {
	providers := make([]Provider, 0, len(providerRegistry))
	for _, providerFunc := range providerRegistry {
		providers = append(providers, providerFunc())
	}
	return providers
}

func GetByID(id InferenceProvider) (Provider, bool) {
	providerFunc, exists := providerRegistry[id]
	if !exists {
		return Provider{}, false
	}
	return providerFunc(), true
}

func GetAvailableIDs() []InferenceProvider {
	ids := make([]InferenceProvider, 0, len(providerRegistry))
	for id := range providerRegistry {
		ids = append(ids, id)
	}
	return ids
}

