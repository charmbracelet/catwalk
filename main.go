// Package main is the main entry point for the HTTP server that serves
// inference providers.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"charm.land/catwalk/internal/providers"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/x/etag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var counter = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: "catwalk",
	Subsystem: "providers",
	Name:      "requests_total",
	Help:      "Total number of requests to the providers endpoint",
})

var (
	providersJSON []byte
	providersETag string

	deprecatedJSON []byte
)

func init() {
	var err error
	providersJSON, err = json.Marshal(providers.GetAll())
	if err != nil {
		log.Fatal("Failed to marshal providers:", err)
	}
	providersETag = etag.Of(providersJSON)

	deprecatedJSON, err = json.Marshal(map[string]any{"error": "This endpoint was removed. Please use /v2/providers instead."})
	if err != nil {
		log.Fatal("Failed to marshal deprecated response:", err)
	}
}

func providersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	etag.Response(w, providersETag)

	if r.Method == http.MethodHead {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	counter.Inc()

	if etag.Matches(r, providersETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if _, err := w.Write(providersJSON); err != nil {
		log.Printf("Error writing response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func providersSpecificHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract provider ID from the URL path
	path := strings.TrimPrefix(r.URL.Path, "/v2/providers/")
	providerID := strings.TrimSuffix(path, "/")

	if providerID == "" {
		http.Error(w, "Provider ID is required", http.StatusBadRequest)
		return
	}

	// Check if pretty printing is requested
	pretty := r.URL.Query().Get("pretty") == "true"

	// Get all providers
	allProviders := providers.GetAll()

	// Find the specific provider by ID
	var foundProvider *catwalk.Provider
	for _, provider := range allProviders {
		if string(provider.ID) == providerID {
			foundProvider = &provider
			break
		}
	}

	if foundProvider == nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	if pretty {
		// Return as markdown table
		w.Header().Set("Content-Type", "text/markdown")
		renderProviderMarkdown(w, *foundProvider, r)
	} else {
		// Return as JSON
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(foundProvider); err != nil {
			log.Printf("Error encoding provider response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func renderProviderMarkdown(w http.ResponseWriter, provider catwalk.Provider, r *http.Request) {
	// Header
	fmt.Fprintf(w, "# Provider: %s\n\n", provider.Name)

	// Basic info table
	fmt.Fprintf(w, "| Field | Value |\n")
	fmt.Fprintf(w, "|-------|-------|\n")
	fmt.Fprintf(w, "| ID | `%s` |\n", provider.ID)
	fmt.Fprintf(w, "| Type | `%s` |\n", provider.Type)
	fmt.Fprintf(w, "| API Endpoint | `%s` |\n", provider.APIEndpoint)
	fmt.Fprintf(w, "| Default Large Model ID | `%s` |\n", provider.DefaultLargeModelID)
	fmt.Fprintf(w, "| Default Small Model ID | `%s` |\n", provider.DefaultSmallModelID)

	if len(provider.Models) > 0 {
		fmt.Fprintf(w, "\n## Available Models\n\n")

		// Handle sorting
		models := provider.Models
		sortParam := r.URL.Query().Get("sort")

		switch sortParam {
		case "output":
			// Sort by output cost (ascending)
			sort.Slice(models, func(i, j int) bool {
				return models[i].CostPer1MOut < models[j].CostPer1MOut
			})
		case "context":
			// Sort by context window (descending)
			sort.Slice(models, func(i, j int) bool {
				return models[i].ContextWindow > models[j].ContextWindow
			})
		default:
			// Default sort by input cost (ascending)
			sort.Slice(models, func(i, j int) bool {
				return models[i].CostPer1MIn < models[j].CostPer1MIn
			})
		}

		fmt.Fprintf(w, "| Name | ID | Context Window | Input Cost ($/M) | Output Cost ($/M) | Reasoning |\n")
		fmt.Fprintf(w, "|----------|------|----------------|------------------|------------------|----------|\n")
		for _, model := range models {
			fmt.Fprintf(w, "| %s | `%s` | %d | %.6f | %.6f | %s |\n", model.Name, model.ID, model.ContextWindow, model.CostPer1MIn, model.CostPer1MOut, yesNo(model.CanReason))
		}
	}
}

func providersHandlerDeprecated(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(deprecatedJSON); err != nil {
		log.Printf("Error writing response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/providers", providersHandler)
	mux.HandleFunc("/v2/providers/", providersSpecificHandler)
	mux.HandleFunc("/providers", providersHandlerDeprecated)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("Server starting on :8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
