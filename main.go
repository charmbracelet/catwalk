// Package main is the main entry point for the HTTP server that serves
// inference providers.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"charm.land/catwalk/internal/deprecated"
	"charm.land/catwalk/internal/providers"
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
)

func init() {
	var err error
	providersJSON, err = json.Marshal(providers.GetAll())
	if err != nil {
		log.Fatal("Failed to marshal providers:", err)
	}
	providersETag = etag.Of(providersJSON)
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

func providersHandlerDeprecated(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	counter.Inc()
	allProviders := deprecated.GetAll()
	if err := json.NewEncoder(w).Encode(allProviders); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/providers", providersHandler)
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
