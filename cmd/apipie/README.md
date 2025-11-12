# APIpie Model Configuration Generator

This tool fetches models from APIpie.ai and generates a configuration file for the provider.

## LLM-Enhanced Display Names

This tool includes an optional feature to generate professional display names for AI models using APIpie.ai's LLM service. This feature is **sponsored** to improve the user experience of this open source project.

### Configuration

Set the following environment variable:

```bash
# Required for LLM-enhanced display names (donated API key)
export APIPIE_DISPLAY_NAME_API_KEY="your-apipie-api-key"
```

### Behavior

- **With API key**: Uses Claude Sonnet 4.5 via APIpie.ai to generate professional display names
  - Example: `gpt-4o-2024-11-20` → `"GPT-4o (2024-11-20)"`
  - Example: `claude-3-5-sonnet` → `"Claude 3.5 Sonnet"`

- **Without API key or on failure**: Falls back to using the raw model ID as display name
  - Example: `gpt-4o-2024-11-20` → `"gpt-4o-2024-11-20"`
  - This ensures the tool **never breaks** due to API issues

### Usage

```bash
# Generate configuration with LLM-enhanced names
go run cmd/apipie/main.go

# The generated config will be saved to:
# internal/providers/configs/apipie.json
```