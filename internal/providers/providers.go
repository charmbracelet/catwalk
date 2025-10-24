// Package providers provides a registry of inference providers
package providers

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

//go:embed configs/*.json
var configsFS embed.FS

// GetAll returns all registered providers.
func GetAll() []catwalk.Provider {
	var providers []catwalk.Provider

	entries, err := fs.ReadDir(configsFS, "configs")
	if err != nil {
		log.Printf("Error reading configs directory: %v", err)
		return providers
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		configPath := filepath.Join("configs", entry.Name())
		configData, err := fs.ReadFile(configsFS, configPath)
		if err != nil {
			log.Printf("Error reading config %s: %v", entry.Name(), err)
			continue
		}

		var provider catwalk.Provider
		if err := json.Unmarshal(configData, &provider); err != nil {
			log.Printf("Error unmarshaling config %s: %v", entry.Name(), err)
			continue
		}

		providers = append(providers, provider)
	}

	return providers
}
