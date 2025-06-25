# Fur - AI Provider Database

## Build/Test Commands
- `go build` - Build the main HTTP server
- `go build ./cmd/openrouter` - Build OpenRouter config generator
- `go test ./...` - Run all tests
- `go test -run TestName ./pkg/...` - Run specific test
- `go run main.go` - Start HTTP server on :8080
- `go run ./cmd/openrouter/main.go` - Generate OpenRouter config

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