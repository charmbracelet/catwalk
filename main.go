// Package main is the main entry point for the HTTP server that serves
// inference providers.
package main

import (
	"encoding/json"
	"log"
	"net/http"
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
	providersJSON, err = json.MarshalIndent(providers.GetAll(), "", "  ")
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(foundProvider); err != nil {
		log.Printf("Error encoding provider response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
