# Catwalk - AI Provider Database

## Build/Test Commands

- `go run .` - Build and run the main HTTP server on :8080
- `go run ./cmd/{provider-name}` - Build and run a CLI to update the `{provider-name}.json` file
- `go test ./...` - Run all tests

## Code Style Guidelines

- Package comments: Start with "Package name provides/represents..."
- Imports: Standard library first, then third-party, then local packages
- Error handling: Use `fmt.Errorf("message: %w", err)` for wrapping
- Struct tags: Use json tags with omitempty for optional fields
- Constants: Group related constants with descriptive comments
- Types: Use custom types for IDs (e.g., `InferenceProvider`, `Type`)
- Naming: Use camelCase for unexported, PascalCase for exported
- Comments: Use `//nolint:directive` for linter exceptions
- HTTP: Always set timeouts, use context, defer close response bodies
- JSON: Use `json.MarshalIndent` for pretty output, validate unmarshaling
- File permissions: Use 0o600 for sensitive config files
- Always format code with `gofumpt`

## Model pricing

Model pricing lives in a nested `pricing` object and is in **dollars per
token**:

```json
"pricing": {
  "input": 1e-05,
  "output": 5e-05,
  "cache_create": 1.25e-05,
  "cache_hit": 2.5e-07
}
```

Provider pages usually quote per-million prices (e.g. "$3/M") — divide by
1,000,000 before putting them in the config.

- `cache_create` = cache **creation** (write) price
- `cache_hit` = cache **read** price

Providers usually advertise a single discounted "cached" price (e.g.
"$0.044/M cached") — that is the cache **read** price, so it goes in
`cache_hit`. Omit `cache_create` (or leave it at 0) unless the provider
explicitly prices cache writes (Anthropic-style). Zero-valued cache fields
are omitted from the configs.

## Adding more provider commands

- Create the `./cmd/{provider-name}/main.go` file
- Try to use the provider API to figure out the available models. If there's no
  endpoint for listing the models, look for some sort of structured text format
  (usually in the docs). If none of that exist, refuse to create the command,
  and add it to the `MANUAL_UPDATES.md` file.
- Add it to `.github/workflows/update.yml`

## Updating providers manually

### Zai

For `zai`, we'll need to grab the model list and capabilities from `https://docs.z.ai/guides/overview/overview`.

That page does not contain the exact `context_window` and `default_max_tokens` though. We can grab the exact value from `./internal/providers/configs/openrouter.json`.
