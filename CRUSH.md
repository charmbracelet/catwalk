# Catwalk - AI Provider Database

## Build/Test Commands

- `go run .` - Build and run the main HTTP server on :8080
- `go run ./cmd/{provider-name}` - Build and run a CLI to update the `{provider-name}.json` file
- `go test ./...` - Run all tests
- `task run` - Run the main HTTP server via Taskfile
- `task gen:all` - Regenerate all provider configurations
- `task gen:{provider-name}` - Regenerate a single provider configuration

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

## Local testing with Crush

To test Catwalk changes locally with the Crush client:

1. Start the Catwalk server:
   ```bash
   task run
   # or: go run .
   ```

2. If port 8080 is already in use, find and kill the existing Catwalk process:
   ```bash
   pgrep -fl catwalk
   /bin/kill <PID>
   ```
   Note: On OpenBSD, `pgrep -af` is not supported, so use `pgrep -fl`.

3. In a new terminal, point Crush to the local Catwalk instance:
   ```bash
   export CATWALK_URL=http://localhost:8080
   crush update-providers
   crush
   ```

   Alternatively, use a one-shot command:
   ```bash
   CATWALK_URL=http://localhost:8080 crush
   ```

4. Verify the providers are served correctly:
   ```bash
   curl -s http://localhost:8080/health
   curl -s http://localhost:8080/v2/providers | jq '.[] | select(.id == "ollama")'
   ```

### Ollama-specific testing

- **Ollama (local)** requires a running Ollama instance on `http://localhost:11434`.
  Generate the config with:
  ```bash
  task gen:ollama
  # or: go run ./cmd/ollama/main.go
  ```
  If Ollama is not running, the generator exits with an error message.

- **Ollama Cloud** does not require a local Ollama instance.
  Generate the config with:
  ```bash
  task gen:ollama-cloud
  # or: go run ./cmd/ollama-cloud/main.go
  ```
  It fetches the public model list from `https://ollama.com/v1/models`.

## Local testing with Crush

To test Catwalk changes locally with the Crush client:

1. Start the Catwalk server:
   ```bash
   task run
   # or: go run .
   ```

2. If port 8080 is already in use, find and kill the existing Catwalk process:
   ```bash
   pgrep -fl catwalk
   /bin/kill <PID>
   ```
   Note: On OpenBSD, `pgrep -af` is not supported, so use `pgrep -fl`.

3. In a new terminal, point Crush to the local Catwalk instance:
   ```bash
   export CATWALK_URL=http://localhost:8080
   crush update-providers
   crush
   ```

   Alternatively, use a one-shot command:
   ```bash
   CATWALK_URL=http://localhost:8080 crush
   ```

4. Verify the providers are served correctly:
   ```bash
   curl -s http://localhost:8080/health
   curl -s http://localhost:8080/v2/providers | jq '.[] | select(.id == "ollama")'
   ```

### Ollama-specific testing

- **Ollama (local)** requires a running Ollama instance on `http://localhost:11434`.
  Generate the config with:
  ```bash
  task gen:ollama
  # or: go run ./cmd/ollama/main.go
  ```
  If Ollama is not running, the generator exits with an error message.

- **Ollama Cloud** does not require a local Ollama instance.
  Generate the config with:
  ```bash
  task gen:ollama-cloud
  # or: go run ./cmd/ollama-cloud/main.go
  ```
  It fetches the public model list from `https://ollama.com/v1/models`.
